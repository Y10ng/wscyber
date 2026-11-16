// Package config 只读取本进程的显式配置，不导入其他应用或会话凭据。
package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Listen, DatabaseURL, AuthToken, WebDir       string
	SandboxImage, SandboxRuntime, SandboxNetwork string
	AllowLoopback                                bool
	MaxParallel, MaxSteps                        int
	MaxOutputBytes                               int64
	ModelBaseURL, ModelAPIKey, ModelName         string
	ModelEncryptionKey                           string
	ModelTimeout, ActionTimeout                  time.Duration
}

func Load() (Config, error) {
	c := Config{
		Listen: env("WSCYBER_LISTEN", "127.0.0.1:8790"), DatabaseURL: os.Getenv("WSCYBER_DATABASE_URL"), AuthToken: os.Getenv("WSCYBER_AUTH_TOKEN"),
		WebDir: env("WSCYBER_WEB_DIR", "web/dist-react"), SandboxImage: env("WSCYBER_SANDBOX_IMAGE", "wscyber/runtime:local"), SandboxRuntime: env("WSCYBER_SANDBOX_RUNTIME", "runc"),
		SandboxNetwork:     env("WSCYBER_SANDBOX_NETWORK", "bridge"),
		ModelEncryptionKey: os.Getenv("WSCYBER_MODEL_ENCRYPTION_KEY"),
		ModelBaseURL:       os.Getenv("WSCYBER_MODEL_BASE_URL"), ModelAPIKey: os.Getenv("WSCYBER_MODEL_API_KEY"), ModelName: os.Getenv("WSCYBER_MODEL"),
	}
	if len(c.AuthToken) < 32 || strings.ContainsAny(c.AuthToken, " \t\r\n") {
		return c, fmt.Errorf("WSCYBER_AUTH_TOKEN 需至少32字符且无空白，请生成随机令牌")
	}
	u, e := url.Parse(c.DatabaseURL)
	if e != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") || u.Host == "" {
		return c, fmt.Errorf("必须提供 WSCYBER_DATABASE_URL 的 PostgreSQL 连接地址")
	}
	if _, _, e = net.SplitHostPort(c.Listen); e != nil {
		return c, fmt.Errorf("WSCYBER_LISTEN 需为 host:port")
	}
	if c.SandboxNetwork != "bridge" && c.SandboxNetwork != "none" {
		return c, fmt.Errorf("WSCYBER_SANDBOX_NETWORK 需为 bridge 或 none")
	}
	var err error
	if c.MaxParallel, err = integer("WSCYBER_MAX_PARALLEL", 2, 1, 16); err != nil {
		return c, err
	}
	if c.MaxSteps, err = integer("WSCYBER_MAX_STEPS", 12, 1, 1000); err != nil {
		return c, err
	}
	max, e := integer("WSCYBER_MAX_OUTPUT_BYTES", 65536, 1024, 1<<20)
	if e != nil {
		return c, e
	}
	c.MaxOutputBytes = int64(max)
	if c.ModelTimeout, err = duration("WSCYBER_MODEL_TIMEOUT", 120*time.Second); err != nil {
		return c, err
	}
	if c.ActionTimeout, err = duration("WSCYBER_ACTION_TIMEOUT", 30*time.Second); err != nil {
		return c, err
	}
	if raw := os.Getenv("WSCYBER_ALLOW_LOOPBACK"); raw != "" {
		c.AllowLoopback, err = strconv.ParseBool(raw)
		if err != nil {
			return c, fmt.Errorf("WSCYBER_ALLOW_LOOPBACK 需为 true/false")
		}
	}
	count := 0
	for _, v := range []string{c.ModelBaseURL, c.ModelAPIKey, c.ModelName} {
		if v != "" {
			count++
		}
	}
	if count != 0 && count != 3 {
		return c, fmt.Errorf("模型 BASE_URL/API_KEY/MODEL 必须一起配置，或全部留空")
	}
	return c, nil
}
func (c Config) ModelConfigured() bool { return c.ModelName != "" }
func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
func integer(k string, d, min, max int) (int, error) {
	v := os.Getenv(k)
	if v == "" {
		return d, nil
	}
	n, e := strconv.Atoi(v)
	if e != nil || n < min || n > max {
		return 0, fmt.Errorf("%s 必须在 %d–%d 之间", k, min, max)
	}
	return n, nil
}
func duration(k string, d time.Duration) (time.Duration, error) {
	v := os.Getenv(k)
	if v == "" {
		return d, nil
	}
	n, e := time.ParseDuration(v)
	if e != nil || n < time.Second || n > 10*time.Minute {
		return 0, fmt.Errorf("%s 必须在1s–10m之间", k)
	}
	return n, nil
}
