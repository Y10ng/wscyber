// Package pgschema 是 wscyber 的 PostgreSQL 基础设施：连接配置、启动检查、
// 单实例锁与 schema 迁移（设计见 native/POSTGRES.md §1、§3、§4）。
//
// 本包不持有业务逻辑；上游原生存储（P2 overlay）与 webapi（P3）各自开池，
// 都经 OpenDB 取得相同的连接参数。
package pgschema

import (
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"
	"syscall"

	"github.com/jackc/pgx/v5"
)

// 未给 --database-url-file 时依次读取的环境变量：先连接串文件路径，再连接串本身。
// Hub 读完后应从进程环境删除两者（终端子进程继承环境）。
const (
	EnvDatabaseURLFile = "WSCYBER_DATABASE_URL_FILE"
	EnvDatabaseURL     = "WSCYBER_DATABASE_URL"
)

// ErrNoDatabaseURL 表示三种来源都没有给出连接串。
var ErrNoDatabaseURL = errors.New("未提供 PostgreSQL 连接串")

// maxDatabaseURLFileBytes 限制连接串文件大小，防止误指向大文件。
const maxDatabaseURLFileBytes = 64 << 10

// DatabaseURL 是已解析的连接串。连接串原文（含口令）只能经 DSN() 取得；
// String/GoString 只给出 host/port/database/user，可安全进日志。
type DatabaseURL struct {
	dsn      string
	source   string
	host     string
	port     uint16
	database string
	user     string
}

// ResolveDatabaseURL 按 §1 的优先级取连接串：path（--database-url-file）非空则读该文件
// （须为属主可读、group/other 无任何权限的普通文件，不跟随符号链接）；否则环境变量
// WSCYBER_DATABASE_URL_FILE 指向的文件；否则环境变量 WSCYBER_DATABASE_URL；都没有则
// 返回 ErrNoDatabaseURL。前一来源给了但不合格时直接失败，不回退。getenv 为 nil 时用 os.Getenv。
func ResolveDatabaseURL(path string, getenv func(string) string) (DatabaseURL, error) {
	if getenv == nil {
		getenv = os.Getenv
	}
	source := "--database-url-file"
	if strings.TrimSpace(path) == "" {
		path, source = strings.TrimSpace(getenv(EnvDatabaseURLFile)), EnvDatabaseURLFile
	}
	if strings.TrimSpace(path) != "" {
		dsn, err := ReadDatabaseURLFile(path)
		if err != nil {
			return DatabaseURL{}, err
		}
		return parseDatabaseURL(dsn, source)
	}
	if dsn := strings.TrimSpace(getenv(EnvDatabaseURL)); dsn != "" {
		return parseDatabaseURL(dsn, EnvDatabaseURL)
	}
	return DatabaseURL{}, fmt.Errorf("%w：用 --database-url-file（或环境变量 %s）指定属主私有（0600/0400）的连接串文件，或设置环境变量 %s；本机准备见 native/POSTGRES.md §7.2",
		ErrNoDatabaseURL, EnvDatabaseURLFile, EnvDatabaseURL)
}

// ParseDatabaseURL 解析连接串（URL 或 key=value 形式）。错误信息不回显原文。
func ParseDatabaseURL(dsn string) (DatabaseURL, error) {
	return parseDatabaseURL(strings.TrimSpace(dsn), "参数")
}

func parseDatabaseURL(dsn, source string) (DatabaseURL, error) {
	if dsn == "" {
		return DatabaseURL{}, fmt.Errorf("%s 中的 PostgreSQL 连接串为空", source)
	}
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		// pgx/url 的解析错误可能包含连接串原文（含口令），这里不透传。
		return DatabaseURL{}, fmt.Errorf("%s 中的 PostgreSQL 连接串无法解析（内容不回显）", source)
	}
	return DatabaseURL{
		dsn:      dsn,
		source:   source,
		host:     cfg.Host,
		port:     cfg.Port,
		database: cfg.Database,
		user:     cfg.User,
	}, nil
}

// ReadDatabaseURLFile 读取连接串文件，规则与 D1 的私密文件一致：
// 拒绝符号链接与非普通文件；属主须可读，group/other 不得有任何权限
// （0600 或 0400）；内容去掉首尾空白后须为单行非空。
func ReadDatabaseURLFile(path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", fmt.Errorf("读取连接串文件: %w", err)
	}
	if err := checkPrivateFileMode(path, info.Mode()); err != nil {
		return "", err
	}
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return "", fmt.Errorf("打开连接串文件: %w", err)
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil {
		return "", fmt.Errorf("读取连接串文件: %w", err)
	}
	// 打开后再查一次，防止 Lstat 与 Open 之间被替换。
	if err := checkPrivateFileMode(path, opened.Mode()); err != nil {
		return "", err
	}
	if !os.SameFile(info, opened) {
		return "", fmt.Errorf("连接串文件 %s 在检查期间被替换", path)
	}
	raw, err := io.ReadAll(io.LimitReader(file, maxDatabaseURLFileBytes+1))
	if err != nil {
		return "", fmt.Errorf("读取连接串文件: %w", err)
	}
	if len(raw) > maxDatabaseURLFileBytes {
		return "", fmt.Errorf("连接串文件 %s 过大", path)
	}
	dsn := strings.TrimSpace(string(raw))
	if dsn == "" {
		return "", fmt.Errorf("连接串文件 %s 为空", path)
	}
	if strings.ContainsAny(dsn, "\r\n") {
		return "", fmt.Errorf("连接串文件 %s 须只有一行", path)
	}
	return dsn, nil
}

func checkPrivateFileMode(path string, mode os.FileMode) error {
	if mode&os.ModeSymlink != 0 {
		return fmt.Errorf("连接串文件 %s 不能是符号链接", path)
	}
	if !mode.IsRegular() {
		return fmt.Errorf("连接串文件 %s 必须是普通文件", path)
	}
	perm := mode.Perm()
	if perm&0o077 != 0 || perm&0o400 == 0 {
		return fmt.Errorf("连接串文件 %s 权限为 %04o，须为属主可读且 group/other 无权限（0600 或 0400）", path, perm)
	}
	return nil
}

// DSN 返回连接串原文（含口令），只用于建立连接，不得写日志。
func (u DatabaseURL) DSN() string { return u.dsn }

// IsZero 报告是否未解析任何连接串。
func (u DatabaseURL) IsZero() bool { return u.dsn == "" }

// Source 说明连接串来源（--database-url-file 或环境变量名）。
func (u DatabaseURL) Source() string { return u.source }

// Host、Port、Database、User 是解析出的非敏感字段。
func (u DatabaseURL) Host() string     { return u.host }
func (u DatabaseURL) Port() uint16     { return u.port }
func (u DatabaseURL) Database() string { return u.database }
func (u DatabaseURL) User() string     { return u.user }

// WithDatabase 返回指向同一服务器、同一角色但换了库名的连接串
// （演示/测试按库名隔离时使用）。
func (u DatabaseURL) WithDatabase(name string) (DatabaseURL, error) {
	if u.IsZero() {
		return DatabaseURL{}, errors.New("连接串为空")
	}
	if strings.TrimSpace(name) == "" {
		return DatabaseURL{}, errors.New("库名为空")
	}
	cfg, err := pgx.ParseConfig(u.dsn)
	if err != nil {
		return DatabaseURL{}, errors.New("连接串无法解析（内容不回显）")
	}
	// 以 key=value 形式追加 dbname，后者覆盖前者；URL 形式则改 path。
	dsn, err := replaceDatabase(u.dsn, name)
	if err != nil {
		return DatabaseURL{}, err
	}
	out, err := parseDatabaseURL(dsn, u.source)
	if err != nil {
		return DatabaseURL{}, err
	}
	if out.database != name || out.host != cfg.Host || out.user != cfg.User {
		return DatabaseURL{}, errors.New("替换库名失败")
	}
	return out, nil
}

func replaceDatabase(dsn, name string) (string, error) {
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		parsed, err := url.Parse(dsn)
		if err != nil {
			return "", errors.New("连接串无法解析（内容不回显）")
		}
		parsed.Path = "/" + name
		parsed.RawPath = ""
		return parsed.String(), nil
	}
	escaped := strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(name)
	return dsn + " dbname='" + escaped + "'", nil
}

// String 只给出可进日志的字段。
func (u DatabaseURL) String() string {
	if u.IsZero() {
		return "postgres(未配置)"
	}
	return fmt.Sprintf("postgres host=%s port=%d database=%s user=%s", u.host, u.port, u.database, u.user)
}

// GoString 防止 %#v 打印出口令。
func (u DatabaseURL) GoString() string { return "pgschema.DatabaseURL{" + u.String() + "}" }
