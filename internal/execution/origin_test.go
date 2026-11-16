package execution

import (
	"context"
	"errors"
	"net/netip"
	"strings"
	"testing"

	"wscyber/internal/domain"
)

func TestNormalizeOrigin(t *testing.T) {
	cases := map[string]string{
		"HTTP://Example.COM:080":           "http://example.com",
		"https://EXAMPLE.com.:443":         "https://example.com",
		"http://example.com:008080":        "http://example.com:8080",
		"https://[2001:0db8::1]:443":       "https://[2001:db8::1]",
		"http://[::ffff:192.168.1.1]:8080": "http://192.168.1.1:8080",
		"https://xn--fsqu00a.xn--0zwm56d":  "https://xn--fsqu00a.xn--0zwm56d",
	}
	for raw, want := range cases {
		t.Run(raw, func(t *testing.T) {
			got, err := NormalizeOrigin(raw)
			if err != nil || got != want {
				t.Fatalf("NormalizeOrigin(%q) = %q, %v; want %q", raw, got, err, want)
			}
		})
	}
	for _, raw := range []string{
		"", " http://example.com", "http://example.com/", "http://example.com/path",
		"http://example.com?", "http://example.com?a=1", "http://example.com#",
		"http://user:pass@example.com", "http://*.example.com", "file:///etc/passwd",
		"//example.com", "http://example.com:", "http://example.com:0", "http://example.com:65536",
		"http://[fe80::1%25eth0]", "http://example.com..", "http://例子.测试",
		"http://good.example\\@bad.example", "http://good.example\n",
	} {
		t.Run(raw, func(t *testing.T) {
			if _, err := NormalizeOrigin(raw); !errors.Is(err, domain.ErrInvalid) {
				t.Fatalf("accepted invalid origin %q: %v", raw, err)
			}
		})
	}
}

func TestValidationIsExactAndDoesNotResolve(t *testing.T) {
	e := New(Config{})
	e.lookup = func(context.Context, string) ([]netip.Addr, error) {
		t.Fatal("proposal validation must not resolve DNS")
		return nil, nil
	}
	valid := domain.ActionInput{Kind: "http", Method: "POST", URL: "https://EXAMPLE.com.:443/a", Body: "test"}
	if err := e.Validate(valid, []string{"https://example.com"}); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{"http://example.com/a", "https://example.com:444/a", "https://sub.example.com/a", "https://example.com.evil/a"} {
		input := valid
		input.URL = target
		if err := e.Validate(input, []string{"https://example.com"}); err == nil {
			t.Fatalf("accepted out-of-scope URL %q", target)
		}
	}
	for _, input := range []domain.ActionInput{
		{Kind: "http", URL: "https://example.com", Method: "CONNECT"},
		{Kind: "http", URL: "https://example.com", Method: "GET\r\nHost: evil"},
		{Kind: "http", URL: "https://example.com", Headers: map[string]string{"Host": "elsewhere"}},
		{Kind: "http", URL: "https://example.com", Headers: map[string]string{"Upgrade": "websocket"}},
		{Kind: "http", URL: "https://example.com", Headers: map[string]string{"Content-Length": "0"}},
		{Kind: "http", URL: "https://example.com", Headers: map[string]string{"X-Key": "a", "x-key": "b"}},
		{Kind: "http", URL: "https://example.com", Headers: map[string]string{"X-Test": "a\r\nInjected: yes"}},
		{Kind: "http", URL: "https://example.com", Body: strings.Repeat("x", maxRequestBytes+1)},
		{Kind: "http", URL: "https://example.com", Command: "id"},
		{Kind: "local_command", Command: "id", URL: "https://example.com"},
		{Kind: "local_command", Command: "\x00"},
		{Kind: "local_command", Command: " "},
		{Kind: "unknown"},
	} {
		if err := e.Validate(input, []string{"https://example.com"}); !errors.Is(err, domain.ErrInvalid) {
			t.Errorf("accepted invalid action %#v: %v", input, err)
		}
	}
}

func TestAddressPolicy(t *testing.T) {
	blocked := []string{
		"0.0.0.0", "0.1.2.3", "169.254.169.254", "169.254.1.1", "224.0.0.1", "255.255.255.255",
		"127.0.0.1", "::", "::1", "::ffff:127.0.0.1", "fe80::1", "ff02::1",
		"100.100.100.200", "168.63.129.16", "192.0.0.192", "fd00:ec2::254",
		"64:ff9b::7f00:1", "2002:7f00:1::", "2001::1",
	}
	e := New(Config{})
	for _, raw := range blocked {
		if err := e.allowedIP(netip.MustParseAddr(raw)); err == nil {
			t.Errorf("accepted blocked IP %s", raw)
		}
	}
	for _, raw := range []string{"10.0.0.1", "172.16.0.1", "192.168.1.1", "fd12::1", "203.0.113.1"} {
		if err := e.allowedIP(netip.MustParseAddr(raw)); err != nil {
			t.Errorf("rejected explicitly authorizable unicast IP %s: %v", raw, err)
		}
	}
	if err := e.Validate(domain.ActionInput{Kind: "http", URL: "http://10.0.0.1/path"}, nil); err == nil {
		t.Fatal("private addresses still require an origin authorization")
	}
	if err := e.Validate(domain.ActionInput{Kind: "http", URL: "http://10.0.0.1/path"}, []string{"http://10.0.0.1"}); err != nil {
		t.Fatal(err)
	}
	dev := New(Config{AllowLoopback: true})
	if err := dev.allowedIP(netip.MustParseAddr("::ffff:127.0.0.1")); err != nil {
		t.Fatal(err)
	}
	if err := dev.allowedIP(netip.MustParseAddr("169.254.169.254")); err == nil {
		t.Fatal("development loopback flag must not permit metadata")
	}
	if err := dev.Validate(domain.ActionInput{Kind: "http", URL: "http://metadata.google.internal"}, []string{"http://metadata.google.internal"}); err == nil {
		t.Fatal("metadata host must remain forbidden even if explicitly listed")
	}
}

func TestDNSRejectsMixedPermittedAndBlockedAnswers(t *testing.T) {
	e := New(Config{})
	e.lookup = func(context.Context, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("203.0.113.1"), netip.MustParseAddr("169.254.169.254")}, nil
	}
	if _, err := e.resolve(context.Background(), "fixture.test"); err == nil {
		t.Fatal("mixed DNS answers must be denied before dialing")
	}
}

func TestInvalidConfigFailsBeforeExecution(t *testing.T) {
	for _, config := range []Config{
		{Runtime: "--privileged"}, {Runtime: "runc --debug"}, {Image: "--mount=bad"},
		{Image: "image\nother"}, {Timeout: -1}, {MaxOutputBytes: -1}, {MaxOutputBytes: maxOutputBytes + 1},
	} {
		e := New(config)
		if err := e.Validate(domain.ActionInput{Kind: "local_command", Command: "id"}, nil); err == nil {
			t.Errorf("accepted invalid config %#v", config)
		}
	}
}
