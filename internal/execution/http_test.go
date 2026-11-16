package execution

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/x509"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"wscyber/internal/domain"
)

func fixtureExecutor(t *testing.T, server *httptest.Server, config Config) (*Executor, string, *atomic.Int32) {
	t.Helper()
	config.AllowLoopback = true
	e := New(config)
	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	calls := &atomic.Int32{}
	e.lookup = func(context.Context, string) ([]netip.Addr, error) {
		calls.Add(1)
		return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
	}
	return e, parsed.Scheme + "://" + net.JoinHostPort("fixture.test", parsed.Port()), calls
}

func TestHTTPPinnedOriginIgnoresProxyAndPreservesBytes(t *testing.T) {
	payload := []byte{0, 1, 255, 'a', '\n'}
	var targetCalls, proxyCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetCalls.Add(1)
		if r.Host[:len("fixture.test")] != "fixture.test" {
			t.Errorf("HTTP Host did not retain approved host: %q", r.Host)
		}
		if r.Method != "POST" || r.Header.Get("X-Test") != "approved" {
			t.Errorf("request did not preserve approved method/headers")
		}
		body, _ := io.ReadAll(r.Body)
		if string(body) != "input" {
			t.Errorf("request body = %q", body)
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.WriteHeader(http.StatusCreated)
		w.Write(payload)
	}))
	defer server.Close()
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyCalls.Add(1)
		http.Error(w, "must not use proxy", 502)
	}))
	defer proxy.Close()
	t.Setenv("HTTP_PROXY", proxy.URL)
	t.Setenv("HTTPS_PROXY", proxy.URL)
	t.Setenv("ALL_PROXY", proxy.URL)
	t.Setenv("NO_PROXY", "")
	e, origin, dnsCalls := fixtureExecutor(t, server, Config{})
	result, err := e.Execute(context.Background(), domain.ActionInput{
		Kind: "http", Method: "POST", URL: origin + "/binary", Body: "input", Headers: map[string]string{"X-Test": "approved"},
	}, []string{origin})
	if err != nil || result.StatusCode != http.StatusCreated || !bytes.Equal(result.Body, payload) || result.Truncated || result.EffectUnknown || result.ResolvedIP != "127.0.0.1" {
		t.Fatalf("result = %#v, error = %v", result, err)
	}
	if targetCalls.Load() != 1 || proxyCalls.Load() != 0 || dnsCalls.Load() != 1 {
		t.Fatalf("target=%d proxy=%d DNS=%d", targetCalls.Load(), proxyCalls.Load(), dnsCalls.Load())
	}
}

func TestHTTPDoesNotFollowRedirects(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Location", "http://metadata.google.internal/secret")
		w.WriteHeader(http.StatusFound)
		w.Write([]byte("redirect body"))
	}))
	defer server.Close()
	e, origin, dnsCalls := fixtureExecutor(t, server, Config{})
	result, err := e.Execute(context.Background(), domain.ActionInput{Kind: "http", URL: origin}, []string{origin})
	if err != nil || result.StatusCode != http.StatusFound || string(result.Body) != "redirect body" || calls.Load() != 1 || dnsCalls.Load() != 1 {
		t.Fatalf("redirect was followed or lost: %#v, %v", result, err)
	}
}

func TestHTTPTruncationAndNoImplicitDecompression(t *testing.T) {
	var compressed bytes.Buffer
	zip := gzip.NewWriter(&compressed)
	zip.Write([]byte("uncompressed content"))
	zip.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Encoding", "gzip")
		w.Write(compressed.Bytes())
	}))
	defer server.Close()
	e, origin, _ := fixtureExecutor(t, server, Config{})
	result, err := e.Execute(context.Background(), domain.ActionInput{Kind: "http", URL: origin}, []string{origin})
	if err != nil || !bytes.Equal(result.Body, compressed.Bytes()) || result.Headers["Content-Encoding"][0] != "gzip" {
		t.Fatalf("compressed representation was altered: %#v, %v", result, err)
	}
	e.config.MaxOutputBytes = 4
	result, err = e.Execute(context.Background(), domain.ActionInput{Kind: "http", URL: origin}, []string{origin})
	if err != nil || !result.Truncated || len(result.Body) != 4 || result.EffectUnknown {
		t.Fatalf("truncation not explicit: %#v, %v", result, err)
	}
}

func TestHTTPTLSCertificateVerification(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Write([]byte("verified"))
	}))
	server.Config.ErrorLog = log.New(io.Discard, "", 0)
	server.StartTLS()
	defer server.Close()
	e := New(Config{AllowLoopback: true})
	input := domain.ActionInput{Kind: "http", URL: server.URL}
	result, err := e.Execute(context.Background(), input, []string{server.URL})
	if err == nil || calls.Load() != 0 || result.EffectUnknown {
		t.Fatalf("untrusted TLS accepted or HTTP written: %#v, %v, calls=%d", result, err, calls.Load())
	}
	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	e.tlsConfig.RootCAs = roots
	result, err = e.Execute(context.Background(), input, []string{server.URL})
	if err != nil || string(result.Body) != "verified" || calls.Load() != 1 {
		t.Fatalf("trusted TLS fixture rejected: %#v, %v", result, err)
	}
	named, origin, _ := fixtureExecutor(t, server, Config{})
	named.tlsConfig.RootCAs = roots
	result, err = named.Execute(context.Background(), domain.ActionInput{Kind: "http", URL: origin}, []string{origin})
	if err == nil || result.EffectUnknown || calls.Load() != 1 {
		t.Fatalf("TLS hostname mismatch accepted: %#v, %v", result, err)
	}
}

func TestHTTPWriteThenDisconnectIsUnknownWithoutReplay(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		conn, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		conn.Close()
	}))
	defer server.Close()
	e, origin, dnsCalls := fixtureExecutor(t, server, Config{})
	result, err := e.Execute(context.Background(), domain.ActionInput{Kind: "http", Method: "POST", URL: origin, Body: "side effect"}, []string{origin})
	if err == nil || !result.EffectUnknown || calls.Load() != 1 || dnsCalls.Load() != 1 {
		t.Fatalf("lost receipt not treated conservatively: %#v, %v, calls=%d", result, err, calls.Load())
	}
}

func TestHTTPCancellationAfterWriteIsUnknown(t *testing.T) {
	received := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(received)
		<-r.Context().Done()
	}))
	defer server.Close()
	e, origin, _ := fixtureExecutor(t, server, Config{Timeout: 5 * time.Second})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	type outcome struct {
		result domain.ActionResult
		err    error
	}
	completed := make(chan outcome, 1)
	go func() {
		result, err := e.Execute(ctx, domain.ActionInput{Kind: "http", URL: origin}, []string{origin})
		completed <- outcome{result, err}
	}()
	select {
	case <-received:
		cancel()
	case <-time.After(5 * time.Second):
		t.Fatal("local fixture never received request")
	}
	select {
	case got := <-completed:
		if got.err == nil || !got.result.EffectUnknown {
			t.Fatalf("cancellation after send must be unknown: %#v, %v", got.result, got.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("HTTP cancellation did not terminate")
	}
}

func TestHTTPBlockedResolutionSendsNothing(t *testing.T) {
	e := New(Config{})
	e.lookup = func(context.Context, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("169.254.169.254")}, nil
	}
	result, err := e.Execute(context.Background(), domain.ActionInput{Kind: "http", URL: "http://fixture.test"}, []string{"http://fixture.test"})
	if err == nil || result.EffectUnknown || result.StatusCode != 0 {
		t.Fatalf("blocked DNS should fail before send: %#v, %v", result, err)
	}
}
