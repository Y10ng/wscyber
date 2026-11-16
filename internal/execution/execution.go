// Package execution 实现独立的受控 HTTP 出口与默认联网的本地沙箱。
// 审批与原子认领属于持久层；这里在每次执行前重新核验不可变请求和授权范围。
package execution

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/netip"
	"strings"
	"time"

	"wscyber/internal/domain"
)

const (
	defaultImage       = "wscyber/runtime:local"
	defaultRuntime     = "runc"
	defaultTimeout     = 60 * time.Second
	defaultOutputBytes = 1 << 20
	maxOutputBytes     = 16 << 20
	maxRequestBytes    = 1 << 20
	maxHeaderBytes     = 64 << 10
	maxCommandBytes    = 64 << 10
	cleanupTimeout     = 10 * time.Second
)

type Config struct {
	Image          string
	Runtime        string
	Network        string // 默认 bridge；需要离线分析时显式设为 none。
	Timeout        time.Duration
	MaxOutputBytes int64
	AllowLoopback  bool
}

type Executor struct {
	config    Config
	configErr error
	lookup    func(context.Context, string) ([]netip.Addr, error)
	tlsConfig *tls.Config
	docker    func(context.Context, []string, io.Writer, io.Writer) error
}

var _ domain.Executor = (*Executor)(nil)

func New(config Config) *Executor {
	if config.Image == "" {
		config.Image = defaultImage
	}
	if config.Runtime == "" {
		config.Runtime = defaultRuntime
	}
	if config.Network == "" {
		config.Network = "bridge"
	}
	if config.Timeout == 0 {
		config.Timeout = defaultTimeout
	}
	if config.MaxOutputBytes == 0 {
		config.MaxOutputBytes = defaultOutputBytes
	}
	e := &Executor{
		config: config,
		lookup: func(ctx context.Context, host string) ([]netip.Addr, error) {
			return net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		},
		tlsConfig: &tls.Config{MinVersion: tls.VersionTLS12},
	}
	e.docker = runDocker
	if config.Timeout < 0 || config.MaxOutputBytes < 1 || config.MaxOutputBytes > maxOutputBytes {
		e.configErr = invalid("执行时限必须为正，输出上限必须介于 1 字节与 16 MiB 之间")
	}
	if !safeImage(config.Image) || !safeRuntime(config.Runtime) {
		e.configErr = invalid("沙箱镜像或运行时名称无效")
	}
	if config.Network != "bridge" && config.Network != "none" {
		e.configErr = invalid("沙箱网络必须为 bridge 或 none")
	}
	return e
}

func (e *Executor) Validate(input domain.ActionInput, origins []string) error {
	if e.configErr != nil {
		return e.configErr
	}
	switch input.Kind {
	case "http":
		_, err := e.validateHTTP(input, origins)
		return err
	case "local_command":
		if strings.TrimSpace(input.Command) == "" || len(input.Command) > maxCommandBytes || strings.ContainsRune(input.Command, 0) {
			return invalid("本地命令不能为空、包含 NUL 或超过 64 KiB")
		}
		if input.URL != "" || input.Method != "" || len(input.Headers) != 0 || input.Body != "" {
			return invalid("本地命令不能携带 HTTP 参数")
		}
		return nil
	default:
		return invalid("不支持的动作类型")
	}
}

func (e *Executor) Execute(ctx context.Context, input domain.ActionInput, origins []string) (result domain.ActionResult, err error) {
	started := time.Now()
	result.ExitCode = -1
	defer func() {
		result.DurationMS = time.Since(started).Milliseconds()
		err = diagnostic(err, input)
	}()
	if err = e.Validate(input, origins); err != nil {
		return result, err
	}
	ctx, cancel := context.WithTimeout(ctx, e.config.Timeout)
	defer cancel()
	if err = ctx.Err(); err != nil {
		return result, err
	}
	if input.Kind == "http" {
		return e.executeHTTP(ctx, input, origins)
	}
	return e.executeCommand(ctx, input.Command)
}

func invalid(message string) error {
	return fmt.Errorf("%w：%s", domain.ErrInvalid, message)
}

func safeRuntime(value string) bool {
	if value == "" || len(value) > 128 || value[0] == '-' {
		return false
	}
	for _, c := range value {
		if !asciiAlphaNumeric(c) && c != '_' && c != '-' && c != '.' {
			return false
		}
	}
	return true
}

func safeImage(value string) bool {
	if value == "" || len(value) > 512 || value[0] == '-' {
		return false
	}
	for _, c := range value {
		if !asciiAlphaNumeric(c) && !strings.ContainsRune("/._-:@", c) {
			return false
		}
	}
	return true
}

func asciiAlphaNumeric(c rune) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}
