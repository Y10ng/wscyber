package config

import (
	"strings"
	"testing"
)

func setup(t *testing.T) {
	t.Helper()
	for _, k := range []string{"WSCYBER_LISTEN", "WSCYBER_WEB_DIR", "WSCYBER_SANDBOX_IMAGE", "WSCYBER_SANDBOX_RUNTIME", "WSCYBER_SANDBOX_NETWORK", "WSCYBER_MAX_PARALLEL", "WSCYBER_MAX_STEPS", "WSCYBER_MAX_OUTPUT_BYTES", "WSCYBER_MODEL_TIMEOUT", "WSCYBER_ACTION_TIMEOUT", "WSCYBER_MODEL_BASE_URL", "WSCYBER_MODEL_API_KEY", "WSCYBER_MODEL", "WSCYBER_ALLOW_LOOPBACK"} {
		t.Setenv(k, "")
	}
	t.Setenv("WSCYBER_AUTH_TOKEN", strings.Repeat("a", 48))
	t.Setenv("WSCYBER_DATABASE_URL", "postgres://wscyber:fixture@127.0.0.1:15432/wscyber")
}
func TestSafeDefaults(t *testing.T) {
	setup(t)
	c, e := Load()
	if e != nil {
		t.Fatal(e)
	}
	if c.WebDir != "web/dist-react" || c.Listen != "127.0.0.1:8790" || c.AllowLoopback || c.ModelConfigured() || c.SandboxRuntime != "runc" || c.SandboxNetwork != "bridge" || c.SandboxImage != "wscyber/runtime:local" {
		t.Fatal("unsafe defaults")
	}
}
func TestExplicitLegacyWebDirectory(t *testing.T) {
	setup(t)
	t.Setenv("WSCYBER_WEB_DIR", "web/dist")
	c, err := Load()
	if err != nil || c.WebDir != "web/dist" {
		t.Fatal(c.WebDir, err)
	}
}
func TestInvalidConfig(t *testing.T) {
	for _, tc := range []struct{ k, v string }{{"WSCYBER_AUTH_TOKEN", "short"}, {"WSCYBER_DATABASE_URL", "sqlite:///tmp/x"}, {"WSCYBER_MAX_PARALLEL", "1000"}, {"WSCYBER_MODEL", "without-credentials"}, {"WSCYBER_ACTION_TIMEOUT", "0s"}, {"WSCYBER_ALLOW_LOOPBACK", "perhaps"}, {"WSCYBER_SANDBOX_NETWORK", "host"}} {
		t.Run(tc.k, func(t *testing.T) {
			setup(t)
			t.Setenv(tc.k, tc.v)
			if _, e := Load(); e == nil {
				t.Fatal("invalid config accepted")
			}
		})
	}
}
