package modelprovider

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestResponseTransportBoundaries(t *testing.T) {
	for _, protocol := range []string{"openai_chat", "anthropic_messages"} {
		good := success(protocol)
		padded := strings.TrimSuffix(good, "}") + `,"padding":""}`
		padded = strings.Replace(padded, `"padding":""`, `"padding":"`+strings.Repeat("x", maxResponseBytes-len(padded))+`"`, 1)
		for _, tc := range []struct {
			name, outcome string
			status        int
			serve         func(http.ResponseWriter, *http.Request)
		}{
			{"exact_body_limit", ValidResponse, 200, func(w http.ResponseWriter, _ *http.Request) { io.WriteString(w, padded) }},
			{"over_body_limit", TransportUnknown, 200, func(w http.ResponseWriter, _ *http.Request) { io.WriteString(w, padded+" ") }},
			{"short_body", TransportUnknown, 200, func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Length", fmt.Sprint(len(good)+10))
				io.WriteString(w, good)
			}},
			{"large_headers", TransportUnknown, 0, func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("X-Large", strings.Repeat("x", 33<<10))
				io.WriteString(w, good)
			}},
			{"closed_socket", TransportUnknown, 0, func(w http.ResponseWriter, _ *http.Request) {
				conn, _, err := w.(http.Hijacker).Hijack()
				if err != nil {
					t.Error(err)
					return
				}
				conn.Close()
			}},
			{"compression", InvalidResponse, 200, func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Encoding", "gzip")
				io.WriteString(w, good)
			}},
			{"wrong_media", InvalidResponse, 200, func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "text/plain")
				io.WriteString(w, good)
			}},
			{"jsonp_media", InvalidResponse, 200, func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/jsonp")
				io.WriteString(w, good)
			}},
			{"problem_json", ValidResponse, 200, func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/example+json; charset=utf-8")
				io.WriteString(w, good)
			}},
		} {
			t.Run(protocol+"/"+tc.name, func(t *testing.T) {
				var hits atomic.Int32
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					hits.Add(1)
					w.Header().Set("Content-Type", "application/json")
					tc.serve(w, r)
				}))
				defer srv.Close()
				got := Complete(context.Background(), fixtureDoc(srv.URL, protocol, "none"), probe())
				if got.Outcome != tc.outcome || got.HTTPStatus != tc.status || !got.Attempted || hits.Load() != 1 || (tc.outcome != ValidResponse && got.Text != "") {
					t.Fatalf("outcome=%s status=%d attempted=%v hits=%d", got.Outcome, got.HTTPStatus, got.Attempted, hits.Load())
				}
			})
		}
	}
}

func TestTimeoutBeforeAndAfterHeaders(t *testing.T) {
	for _, protocol := range []string{"openai_chat", "anthropic_messages"} {
		for _, afterHeaders := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/headers=%v", protocol, afterHeaders), func(t *testing.T) {
				var hits atomic.Int32
				arrived, release := make(chan struct{}), make(chan struct{})
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					hits.Add(1)
					if afterHeaders {
						w.Header().Set("Content-Type", "application/json")
						io.WriteString(w, `{"id":`)
						w.(http.Flusher).Flush()
					}
					close(arrived)
					<-release
				}))
				defer srv.Close()
				defer close(release)
				doc := fixtureDoc(srv.URL, protocol, "none")
				doc.TimeoutSeconds = 1
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				done := make(chan Result, 1)
				go func() { done <- Complete(ctx, doc, probe()) }()
				select {
				case <-arrived:
				case <-time.After(3 * time.Second):
					t.Fatal("request did not arrive")
				}
				// 让配置deadline在等头/读体阶段生效。服务器Flush不证明客户端已收到头，
				// 不能在Flush后立即取消，却断言客户端必有200；显式取消另有独立用例。
				select {
				case got := <-done:
					status := 0
					if afterHeaders {
						status = 200
					}
					if got.Outcome != TransportUnknown || got.HTTPStatus != status || got.Text != "" || hits.Load() != 1 {
						t.Fatalf("unexpected result %+v hits=%d", got, hits.Load())
					}
				case <-time.After(3 * time.Second):
					t.Fatal("timeout/cancellation not propagated")
				}
			})
		}
	}
}

func TestDialResolvesOnceAndNeverFallsBack(t *testing.T) {
	for _, tc := range []struct {
		name      string
		ips       []netip.Addr
		lookupErr error
		wantDial  int
	}{
		{"first_ip_only", []netip.Addr{netip.MustParseAddr("127.0.0.1"), netip.MustParseAddr("127.0.0.2")}, nil, 1},
		{"mapped_loopback", []netip.Addr{netip.MustParseAddr("::ffff:127.0.0.1")}, nil, 1},
		{"mixed_forbidden", []netip.Addr{netip.MustParseAddr("127.0.0.1"), netip.MustParseAddr("169.254.169.254")}, nil, 0},
		{"no_addresses", nil, nil, 0},
		{"resolver_error", nil, errors.New("fixture resolver error"), 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lookups, dials := 0, 0
			_, err := dialResolved(context.Background(), "tcp", "fixture.test:443",
				func(_ context.Context, network, host string) ([]netip.Addr, error) {
					lookups++
					if network != "ip" || host != "fixture.test" {
						t.Error("wrong lookup")
					}
					return tc.ips, tc.lookupErr
				}, func(_ context.Context, network, address string) (net.Conn, error) {
					dials++
					if network != "tcp" || address != "127.0.0.1:443" {
						t.Error("did not dial pinned IP")
					}
					return nil, errors.New("fixture dial failure")
				})
			if err == nil || lookups != 1 || dials != tc.wantDial {
				t.Fatalf("lookups=%d dials=%d error=%v", lookups, dials, err)
			}
		})
	}
}

func TestTransportRequestGuardAndWireLimit(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if n, _ := io.Copy(io.Discard, r.Body); n != maxRequestBytes {
			t.Errorf("wire bytes=%d", n)
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, success("openai_chat"))
	}))
	defer srv.Close()
	endpoint, _ := url.Parse(srv.URL + "/chat/completions")
	transport := newTransport(endpoint)
	defer transport.transport.CloseIdleConnections()
	for _, tc := range []struct {
		name, method, endpoint, host string
		size                         int
	}{
		{"large", "POST", endpoint.String(), "", maxRequestBytes + 1},
		{"wrong_method", "GET", endpoint.String(), "", 1},
		{"wrong_endpoint", "POST", srv.URL + "/other", "", 1},
		{"wrong_host", "POST", endpoint.String(), "other.test", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req, _ := http.NewRequest(tc.method, tc.endpoint, strings.NewReader(strings.Repeat("x", tc.size)))
			if tc.host != "" {
				req.Host = tc.host
			}
			// 长度未知时同样必须在发送前读到上限，不能只信ContentLength。
			if tc.name == "large" {
				req.ContentLength = -1
			}
			if _, err := transport.RoundTrip(req); err == nil || transport.attempted || hits.Load() != 0 {
				t.Fatal("invalid request sent")
			}
		})
	}
	for i := 0; i < 2; i++ {
		req, _ := http.NewRequest("POST", endpoint.String(), strings.NewReader(strings.Repeat("x", maxRequestBytes)))
		resp, err := transport.RoundTrip(req)
		if resp != nil {
			resp.Body.Close()
		}
		if (i == 0 && err != nil) || (i == 1 && err == nil) {
			t.Fatalf("attempt %d error=%v", i, err)
		}
	}
	if hits.Load() != 1 {
		t.Fatalf("unexpected posts=%d", hits.Load())
	}
}

func TestHTTPSIsolationAndFreshHTTP1Connections(t *testing.T) {
	var hits, connections, proxyHits atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { proxyHits.Add(1) }))
	defer proxy.Close()
	for _, name := range []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "all_proxy"} {
		t.Setenv(name, proxy.URL)
	}
	t.Setenv("NO_PROXY", "")
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.ProtoMajor != 1 || !r.Close || r.TLS.ServerName != "example.com" || r.Header.Get("Accept-Encoding") != "" {
			t.Error("unexpected HTTP version, reuse, SNI or compression")
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, success("openai_chat"))
	}))
	srv.EnableHTTP2 = true
	srv.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			connections.Add(1)
		}
	}
	srv.StartTLS()
	defer srv.Close()
	endpoint, _ := url.Parse("https://example.com/chat/completions")
	transport := newTransport(endpoint)
	defer transport.transport.CloseIdleConnections()
	if transport.transport.Proxy != nil || transport.transport.TLSClientConfig.InsecureSkipVerify || transport.transport.TLSClientConfig.MinVersion != tls.VersionTLS12 {
		t.Fatal("unsafe production defaults")
	}
	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())
	transport.transport.TLSClientConfig.RootCAs = pool // 仅测试信任fixture证书，不改变生产根证书。
	transport.transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		if address != "example.com:443" {
			t.Error("unexpected destination")
		}
		return (&net.Dialer{}).DialContext(ctx, network, srv.Listener.Addr().String())
	}
	client := &http.Client{Transport: transport, Timeout: 2 * time.Second}
	for i := 0; i < 2; i++ {
		// 测底层标准Transport在相邻请求间也不复用；固定端点包装每次仅允许一次调用。
		if i == 1 {
			transport.attempted = false
		}
		req, _ := http.NewRequest("POST", endpoint.String(), strings.NewReader(`{}`))
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
	}
	if hits.Load() != 2 || connections.Load() != 2 || proxyHits.Load() != 0 {
		t.Fatalf("hits=%d connections=%d proxy=%d", hits.Load(), connections.Load(), proxyHits.Load())
	}
}
