package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/chainreactors/cyber/core/extension"
	webpkg "github.com/chainreactors/cyber/pkg/web"
	"github.com/chainreactors/cyber/tools/headless"

	"wscyber/native/internal/buildinfo"
	"wscyber/native/internal/uiembed"
	"wscyber/native/pgschema"
)

const (
	defaultServeListen = "127.0.0.1:8080"
	accessKeyName      = "access.key"
	envListen          = "WSCYBER_LISTEN"
	envStateDir        = "WSCYBER_STATE_DIR"
	envAccessKeyFile   = "WSCYBER_ACCESS_KEY_FILE"
	envDatabaseURLFile = pgschema.EnvDatabaseURLFile
	envDatabaseURL     = pgschema.EnvDatabaseURL
)

// embeddedUI 返回编译进二进制的前端；测试可替换为合成资源。
var embeddedUI = uiembed.FS

// notices 接收给运维看的 stderr 提示（不含 key/连接串内容）；测试可替换。
var notices io.Writer = os.Stderr

func notice(format string, args ...any) {
	fmt.Fprintf(notices, "wscyber-hub: "+format+"\n", args...)
}

// serveConfig 是 serve 解析后的配置；来源优先级：旗标 > 环境变量 > 默认值。
type serveConfig struct {
	listen          string
	stateDir        string
	accessKeyFile   string // 空 = 使用 <state>/access.key（不存在则生成）
	databaseURLFile string // 只取旗标；环境变量由 loadDatabaseURL（pgschema.ResolveDatabaseURL）处理
	uiDir           string
}

func resolveServeConfig(flags serveConfig, lookup func(string) (string, bool)) (serveConfig, error) {
	pick := func(flagValue, env, fallback string) string {
		if flagValue != "" {
			return flagValue
		}
		if value, ok := lookup(env); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
		return fallback
	}
	config := serveConfig{
		listen:          pick(flags.listen, envListen, defaultServeListen),
		stateDir:        pick(flags.stateDir, envStateDir, ""),
		accessKeyFile:   pick(flags.accessKeyFile, envAccessKeyFile, ""),
		databaseURLFile: flags.databaseURLFile,
		uiDir:           flags.uiDir,
	}
	if config.stateDir == "" {
		base, _ := lookup("XDG_DATA_HOME")
		if !filepath.IsAbs(base) {
			home, _ := lookup("HOME")
			if !filepath.IsAbs(home) {
				return serveConfig{}, fmt.Errorf("无法确定默认 state 目录（HOME 未设置）；请指定 --state-dir 或 %s", envStateDir)
			}
			base = filepath.Join(home, ".local", "share")
		}
		config.stateDir = filepath.Join(base, "wscyber")
	}
	return config, nil
}

// loadDatabaseURL 取 PostgreSQL 连接串（旧旗标形式与 serve 共用）：file（--database-url-file）>
// $WSCYBER_DATABASE_URL_FILE > $WSCYBER_DATABASE_URL；读取与权限校验全部由 pgschema.ResolveDatabaseURL
// 完成（错误不回显连接串）。两个环境变量无论是否采用，读后都立即从进程环境删除（终端子进程继承环境）。
// 都没有时返回 pgschema.ErrNoDatabaseURL。
func loadDatabaseURL(file string) (pgschema.DatabaseURL, error) {
	env := map[string]string{}
	for _, name := range []string{envDatabaseURLFile, envDatabaseURL} {
		value, set := os.LookupEnv(name)
		if !set {
			continue
		}
		env[name] = value
		if err := os.Unsetenv(name); err != nil {
			return pgschema.DatabaseURL{}, fmt.Errorf("清除环境变量 %s: %w", name, err)
		}
	}
	return pgschema.ResolveDatabaseURL(file, func(name string) string { return env[name] })
}

// ensureAccessKey 返回 <state>/access.key；不存在时生成 32 字节随机密钥（hex，0600），已存在则复用。
// 先写临时文件再 link，避免并发启动或中途崩溃留下半个密钥。
func ensureAccessKey(stateDir string) (path string, created bool, err error) {
	path = filepath.Join(stateDir, accessKeyName)
	if _, err := os.Lstat(path); err == nil {
		return path, false, nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return "", false, fmt.Errorf("检查访问密钥文件: %w", err)
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return "", false, err
	}
	tmp, err := os.CreateTemp(stateDir, ".access.key-*")
	if err != nil {
		return "", false, fmt.Errorf("生成访问密钥: %w", err)
	}
	defer os.Remove(tmp.Name())
	_, err = tmp.WriteString(hex.EncodeToString(secret) + "\n")
	err = errors.Join(err, tmp.Chmod(0600), tmp.Sync(), tmp.Close())
	if err != nil {
		return "", false, fmt.Errorf("生成访问密钥: %w", err)
	}
	if err := os.Link(tmp.Name(), path); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return path, false, nil
		}
		return "", false, fmt.Errorf("生成访问密钥: %w", err)
	}
	return path, true, nil
}

// nodeDialAddress 把监听地址换成进程内节点可回连的地址：通配地址（0.0.0.0 / ::）一律改为
// 127.0.0.1:<port>（Go 对通配地址建双栈监听，IPv4 回环可达），不依赖“连 0.0.0.0 即本机”的平台行为。
func nodeDialAddress(address string) string {
	addr, err := netip.ParseAddrPort(address)
	if err != nil || !addr.Addr().IsUnspecified() {
		return address
	}
	return netip.AddrPortFrom(netip.AddrFrom4([4]byte{127, 0, 0, 1}), addr.Port()).String()
}

func browserPath() string {
	binary, err := headless.Discover()
	if err != nil {
		return ""
	}
	return binary.Path
}

func embeddedUIPresent(fsys fs.FS) bool {
	entries, err := fs.ReadDir(fsys, ".")
	return err == nil && !(len(entries) == 0 || (len(entries) == 1 && entries[0].Name() == uiembed.Placeholder))
}

func currentBuildInfo(manifestSHA string) buildinfo.Info {
	return buildinfo.Get(uiCommit, manifestSHA)
}

// embeddedManifestSHA 只计算嵌入清单原文的 SHA256（version 子命令用，完整校验在 serve 启动时）。
func embeddedManifestSHA(fsys fs.FS) string {
	raw, err := fs.ReadFile(fsys, uiManifest)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// versionExtension 注册 GET /api/wscyber/version；位于 /api/ 下，由原生 Auth 中间件鉴权。
func versionExtension(info buildinfo.Info) extension.Extension {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(info)
	})
	return extension.Func{LoadFunc: func(scope *extension.Scope) error {
		return extension.Add(scope, webpkg.Route{Pattern: "GET /api/wscyber/version", Handler: handler})
	}}
}

func parseSubcommand(flags *flag.FlagSet, args []string, output io.Writer) (help bool, err error) {
	flags.SetOutput(io.Discard)
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			flags.SetOutput(output)
			flags.PrintDefaults()
			return true, nil
		}
		return false, err
	}
	if flags.NArg() != 0 {
		return false, fmt.Errorf("%s 不接受位置参数", flags.Name())
	}
	return false, nil
}

// runServe 是正式运行入口（= 旧旗标 --local-hub --model-node 正式分支）+ 默认值、环境变量、
// 自动访问密钥、非 loopback 监听与嵌入前端。
func runServe(ctx context.Context, args []string, output io.Writer) error {
	var given serveConfig
	flags := flag.NewFlagSet("serve", flag.ContinueOnError)
	flags.StringVar(&given.listen, "listen", "", "监听 IP:port；默认 $"+envListen+" 或 "+defaultServeListen+"；对内网开放用 0.0.0.0:8080 并经反向代理终止 TLS")
	flags.StringVar(&given.stateDir, "state-dir", "", "状态目录（可重开）；默认 $"+envStateDir+" 或 ${XDG_DATA_HOME:-$HOME/.local/share}/wscyber")
	flags.StringVar(&given.accessKeyFile, "access-key-file", "", "访问密钥文件（0600/0400 普通文件）；默认 $"+envAccessKeyFile+" 或 <state>/access.key（不存在则生成）")
	flags.StringVar(&given.databaseURLFile, "database-url-file", "", "PostgreSQL 连接串文件（属主私有 0600/0400）；默认 $"+envDatabaseURLFile+"，或读 $"+envDatabaseURL+"（读后从环境删除）；三者都没有则启动失败")
	flags.StringVar(&given.uiDir, "ui-dir", "", "可选前端构建产物目录，优先于嵌入前端（开发预览用）")
	if help, err := parseSubcommand(flags, args, output); help || err != nil {
		return err
	}
	config, err := resolveServeConfig(given, os.LookupEnv)
	if err != nil {
		return err
	}
	// 先做不产生副作用的检查，再建目录/生成密钥。
	if addr, err := netip.ParseAddrPort(config.listen); err != nil || addr.Addr().Zone() != "" {
		return fmt.Errorf("--listen 必须为 IP:port（如 127.0.0.1:8080、0.0.0.0:8080）")
	}
	database, err := loadDatabaseURL(config.databaseURLFile)
	if err != nil {
		return err
	}
	stateDir, err := filepath.Abs(config.stateDir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(stateDir, 0700); err != nil {
		return fmt.Errorf("创建或重开 state-dir: %w", err)
	}
	if err := os.Chmod(stateDir, 0700); err != nil {
		return fmt.Errorf("保护 state-dir: %w", err)
	}
	keyFile := config.accessKeyFile
	if keyFile == "" {
		var created bool
		if keyFile, created, err = ensureAccessKey(stateDir); err != nil {
			return err
		}
		if created {
			notice("已生成访问密钥文件 %s（0600）；浏览器登录时粘贴其内容", keyFile)
		}
	}
	embedded := embeddedUI()
	if config.uiDir == "" && !embeddedUIPresent(embedded) {
		notice("本二进制未嵌入前端，只提供 API；发布构建会嵌入前端，开发时可用 --ui-dir")
	}
	if browserPath() == "" {
		notice("未找到 Chromium，playwright/katana -headless 不可用；安装系统 chromium 或设 CYBER_BROWSER_PATH")
	}
	opts := options{
		localHub: true, modelNode: true, serve: true,
		listen: config.listen, keyFile: keyFile, stateDir: stateDir, uiDir: config.uiDir, embedUI: embedded,
		database: database,
	}
	return runHub(ctx, opts, output, map[string]any{"access_key_file": keyFile, "state_dir": stateDir})
}

func runVersion(args []string, output io.Writer) error {
	flags := flag.NewFlagSet("version", flag.ContinueOnError)
	asJSON := flags.Bool("json", false, "输出 JSON")
	if help, err := parseSubcommand(flags, args, output); help || err != nil {
		return err
	}
	info := currentBuildInfo(embeddedManifestSHA(embeddedUI()))
	if *asJSON {
		return json.NewEncoder(output).Encode(info)
	}
	_, err := fmt.Fprintln(output, info.String())
	return err
}

// runHealthcheck GET <base>/readyz（404 时退回 /health），2 s 超时，200 退出 0，否则返回错误（退出 1）。
func runHealthcheck(ctx context.Context, args []string, output io.Writer) error {
	flags := flag.NewFlagSet("healthcheck", flag.ContinueOnError)
	base := flags.String("url", "", "Hub 基地址（如 http://127.0.0.1:8080）；默认按 $"+envListen+" 或 "+defaultServeListen+"，通配地址改连 127.0.0.1")
	if help, err := parseSubcommand(flags, args, output); help || err != nil {
		return err
	}
	target := *base
	if target == "" {
		listen := defaultServeListen
		if value := strings.TrimSpace(os.Getenv(envListen)); value != "" {
			listen = value
		}
		target = "http://" + nodeDialAddress(listen)
	}
	u, err := url.Parse(target)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil ||
		(u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("--url 须为 http(s)://主机:端口 形式的基地址")
	}
	u.Path = ""
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	client := &http.Client{
		Transport:     &http.Transport{Proxy: nil},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	defer client.CloseIdleConnections()
	// 返回 HTTP 状态与 JSON 的 status 字段；非 JSON（如前端 index）时 status 为空。
	get := func(path string) (int, string, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String()+path, nil)
		if err != nil {
			return 0, "", err
		}
		res, err := client.Do(req)
		if err != nil {
			return 0, "", err
		}
		defer res.Body.Close()
		var body struct {
			Status string `json:"status"`
		}
		_ = json.NewDecoder(io.LimitReader(res.Body, 1<<16)).Decode(&body)
		return res.StatusCode, body.Status, nil
	}
	endpoint, want := "/readyz", "ready"
	code, status, err := get(endpoint)
	if err == nil && code == http.StatusNotFound {
		endpoint, want = "/health", "ok"
		code, status, err = get(endpoint)
	}
	if err != nil {
		return fmt.Errorf("健康检查失败：%w", err)
	}
	if code != http.StatusOK || status != want {
		return fmt.Errorf("健康检查失败：%s 返回 %d（status=%q）", endpoint, code, status)
	}
	_, err = fmt.Fprintf(output, "ok %s\n", endpoint)
	return err
}
