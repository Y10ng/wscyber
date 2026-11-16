package modeltransport

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"net/netip"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chainreactors/cyber/agent/provider"
	aop "github.com/chainreactors/cyber/aop"
	"github.com/chainreactors/proxyclient"
)

const testKey = "h4b-not-a-real-credential"

func testConfig(endpoint string) provider.ProviderConfig {
	images := true
	return provider.ProviderConfig{Provider: "openai", BaseURL: endpoint, APIKey: testKey, Model: "h4b-wire", Timeout: 3, Images: &images}
}

func testRequest() *provider.ChatCompletionRequest {
	return &provider.ChatCompletionRequest{Messages: []*aop.Message{provider.TextMessage("user", "本机受控协议测试")}}
}

// 纯输入/factory/dial检查：此用例不创建监听器、不调用真实拨号器。
func TestConfigurationAndDialBoundaries(t *testing.T) {
	const endpoint = "http://127.0.0.1:43123/v1"
	invalidEndpoints := []string{
		"", "http://localhost:43123/v1", "http://127.0.0.2:43123/v1", "http://192.0.2.1:43123/v1",
		"http://[::1]:43123/v1", "http://[::ffff:127.0.0.1]:43123/v1", "https://127.0.0.1:43123/v1",
		"http://127.0.0.1:0/v1", "http://127.0.0.1:043123/v1", "http://127.0.0.1/v1",
		"http://user:secret@127.0.0.1:43123/v1", endpoint + "?", endpoint + "?x=1", endpoint + "#",
		endpoint + "#fragment", endpoint + "/", "http://127.0.0.1:43123/%76%31", "HTTP://127.0.0.1:43123/v1",
		"http://127.0.0.1:43123/other", " " + endpoint,
	}
	for i, value := range invalidEndpoints {
		if _, err := NewLoopbackOpenAI(value, testConfig(value)); !errors.Is(err, ErrConfiguration) {
			t.Fatalf("endpoint case %d was not rejected", i)
		}
	}
	changes := []struct {
		name string
		edit func(*provider.ProviderConfig)
	}{
		{"inferred-provider", func(c *provider.ProviderConfig) { c.Provider = "" }},
		{"provider-alias", func(c *provider.ProviderConfig) { c.Provider = "deepseek" }},
		{"other-protocol", func(c *provider.ProviderConfig) { c.Provider = "anthropic" }},
		{"provider-case", func(c *provider.ProviderConfig) { c.Provider = "OpenAI" }},
		{"mismatched-endpoint", func(c *provider.ProviderConfig) { c.BaseURL = "http://127.0.0.1:43124/v1" }},
		{"proxy", func(c *provider.ProviderConfig) { c.Proxy = "http://127.0.0.1:43124" }},
		{"internal-proxy-injection", func(c *provider.ProviderConfig) { c.Proxy = pinScheme + "://127.0.0.1:43124" }},
		{"direct-proxy", func(c *provider.ProviderConfig) { c.Proxy = "direct" }},
		{"missing-key", func(c *provider.ProviderConfig) { c.APIKey = " " }},
		{"missing-model", func(c *provider.ProviderConfig) { c.Model = " " }},
		{"negative-tokens", func(c *provider.ProviderConfig) { c.MaxTokens = -1 }},
		{"negative-window", func(c *provider.ProviderConfig) { c.ContextWindow = -1 }},
	}
	for _, tc := range changes {
		t.Run(tc.name, func(t *testing.T) {
			config := testConfig(endpoint)
			tc.edit(&config)
			if _, err := NewLoopbackOpenAI(endpoint, config); !errors.Is(err, ErrConfiguration) {
				t.Fatal("invalid configuration was not rejected")
			}
		})
	}
	input := testConfig(endpoint)
	client, err := NewLoopbackOpenAI(endpoint, input)
	if err != nil {
		t.Fatal(err)
	}
	defer client.CloseIdleConnections()
	*input.Images = false
	view := client.Config()
	if view.Images == nil || !*view.Images || view.Proxy != pinScheme+"://127.0.0.1:43123" || input.Proxy != "" {
		t.Fatal("input snapshot is shared or internal pin not installed")
	}
	*view.Images, view.APIKey, view.BaseURL, view.Proxy = false, "changed", "http://127.0.0.1:1/v1", "direct"
	if fresh := client.Config(); fresh.Images == nil || !*fresh.Images || fresh.APIKey != testKey || fresh.BaseURL != endpoint {
		t.Fatal("returned snapshot aliases internal configuration")
	}
	if _, ok := any(client).(provider.WebSearchProvider); ok {
		t.Fatal("web search must not be exposed")
	}
	if _, ok := any(client).(interface {
		ListModels(context.Context) ([]string, error)
	}); ok {
		t.Fatal("model listing must not be exposed")
	}

	for i, raw := range []string{
		"http://127.0.0.1:43123", pinScheme + "://localhost:43123", pinScheme + "://127.0.0.1:0",
		pinScheme + "://127.0.0.1:43123/path", pinScheme + "://127.0.0.1:43123?x=1",
		pinScheme + "://127.0.0.1:43123?", pinScheme + "://user@127.0.0.1:43123", pinScheme + "://127.0.0.1:43123#x",
	} {
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatal("invalid test URL")
		}
		if _, err := pinFactory(u, nil); !errors.Is(err, ErrConfiguration) {
			t.Fatalf("factory case %d was not rejected", i)
		}
	}
	if _, err := pinFactory(nil, nil); !errors.Is(err, ErrConfiguration) {
		t.Fatal("nil factory URL was not rejected")
	}
	// 原生工厂可构造，不发请求；上游dial即使存在也不作为连接委托。
	u, _ := url.Parse(pinScheme + "://127.0.0.1:43123")
	registered, err := proxyclient.NewClient(u)
	if err != nil || registered == nil {
		t.Fatal("native scheme registration failed")
	}
	u.Host = "127.0.0.1:43124"
	if _, err := registered(context.Background(), "tcp", u.Host); !errors.Is(err, ErrDestination) {
		t.Fatal("factory pin changed with input URL")
	}

	calls := 0
	stubError := errors.New("stub reached")
	dial := pinnedDial(netip.MustParseAddrPort("127.0.0.1:43123"), func(_ context.Context, network, address string) (net.Conn, error) {
		calls++
		if network != "tcp4" || address != "127.0.0.1:43123" {
			t.Fatal("unexpected underlying dial arguments")
		}
		return nil, stubError
	})
	for _, address := range []string{"localhost:43123", "blocked.invalid:43123", "127.0.0.1:43124", "127.0.0.2:43123", "192.0.2.1:43123", "[::1]:43123", "127.0.0.1:043123", "127.0.0.1:0"} {
		if _, err := dial(context.Background(), "tcp", address); !errors.Is(err, ErrDestination) {
			t.Fatal("non-pinned destination was not rejected before dial")
		}
	}
	for _, network := range []string{"udp", "tcp6", "unix", ""} {
		if _, err := dial(context.Background(), network, "127.0.0.1:43123"); !errors.Is(err, ErrDestination) {
			t.Fatal("unsupported network was not rejected")
		}
	}
	if calls != 0 {
		t.Fatal("rejected request reached underlying dial")
	}
	for _, network := range []string{"tcp", "tcp4"} {
		if _, err := dial(context.Background(), network, "127.0.0.1:43123"); !errors.Is(err, stubError) {
			t.Fatal("pinned request did not use underlying dial")
		}
	}
	if calls != 2 {
		t.Fatal("each new connection must independently pass pin")
	}
}

func writeReply(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, `{"id":"h4b-local","choices":[{"message":{"role":"assistant","content":"H4B_LOCAL_MODEL_OK"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)
}

func writeStream(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"role\":\"assistant\",\"content\":\"H4B_LOCAL_MODEL_OK\"},\"finish_reason\":\"\"}]}\n\ndata: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	w.(http.Flusher).Flush()
}

func expectReply(t *testing.T, client *Client, ctx context.Context) {
	t.Helper()
	response, err := client.ChatCompletion(ctx, testRequest())
	if err != nil {
		t.Fatalf("native completion failed: %v", err)
	}
	if len(response.Choices) != 1 || provider.MessageText(response.Choices[0].Message) != "H4B_LOCAL_MODEL_OK" || response.Choices[0].FinishReason != "stop" {
		t.Fatal("unexpected native completion")
	}
}

func expectStream(t *testing.T, client *Client, ctx context.Context) {
	t.Helper()
	events, err := client.ChatCompletionStream(ctx, testRequest())
	if err != nil {
		t.Fatalf("native stream failed: %v", err)
	}
	var text strings.Builder
	done := false
	for event := range events {
		if event.Err != nil {
			t.Fatalf("native stream event failed: %v", event.Err)
		}
		text.WriteString(event.MessageDelta.GetText())
		done = done || event.Done
	}
	if !done || text.String() != "H4B_LOCAL_MODEL_OK" {
		t.Fatal("native stream did not complete")
	}
}

func waitSignal(t *testing.T, signal <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(2 * time.Second):
		t.Fatal(what)
	}
}

func TestNativeHTTPAndLifecycle(t *testing.T) {
	var requests, badRequests, connections atomic.Int64
	closed := make(chan struct{}, 16)
	started := make(chan struct{})
	canceled := make(chan struct{})
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		var body struct {
			Model  string `json:"model"`
			Stream bool   `json:"stream"`
		}
		if r.Method != "POST" || r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer "+testKey || json.NewDecoder(io.LimitReader(r.Body, 8192)).Decode(&body) != nil {
			badRequests.Add(1)
			http.Error(w, "invalid fixture request", http.StatusBadRequest)
			return
		}
		if body.Model == "cancel-wire" {
			close(started)
			<-r.Context().Done()
			close(canceled)
			return
		}
		if body.Model != "h4b-wire" {
			badRequests.Add(1)
		}
		if body.Stream {
			writeStream(w)
		} else {
			writeReply(w)
		}
	}))
	server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			connections.Add(1)
		}
		if state == http.StateClosed {
			closed <- struct{}{}
		}
	}
	server.Start()
	defer server.Close()
	endpoint := server.URL + "/v1"
	input := testConfig(endpoint)
	client, err := NewLoopbackOpenAI(endpoint, input)
	if err != nil {
		t.Fatal(err)
	}
	defer client.CloseIdleConnections()
	var state provider.State
	state.Set(client, client.Config())
	if requests.Load() != 0 || connections.Load() != 0 || state.Health().State != provider.HealthConfigured {
		t.Fatal("construction/publication must not probe or mark ready")
	}
	// 改输入及返回快照后，真正线上请求仍必须使用原key/地址/模型。
	*input.Images = false
	snapshot := client.Config()
	*snapshot.Images, snapshot.APIKey, snapshot.Model, snapshot.BaseURL, snapshot.Proxy = false, "changed", "changed", "http://127.0.0.1:1/v1", "direct"
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	expectReply(t, client, ctx)
	expectReply(t, client, ctx)
	if connections.Load() != 1 {
		t.Fatal("same-authority HTTP/1 keepalive was not reused")
	}
	client.CloseIdleConnections()
	waitSignal(t, closed, "idle connection did not close")
	expectReply(t, client, ctx)
	if connections.Load() != 2 {
		t.Fatal("request after idle close must reconnect through pin")
	}
	expectStream(t, client, ctx)

	activeCtx, activeCancel := context.WithCancel(ctx)
	defer activeCancel()
	callDone := make(chan error, 1)
	go func() {
		request := testRequest()
		request.Model = "cancel-wire"
		_, err := client.ChatCompletion(activeCtx, request)
		callDone <- err
	}()
	waitSignal(t, started, "active request did not start")
	client.CloseIdleConnections()
	select {
	case <-callDone:
		t.Fatal("idle close unexpectedly ended active call")
	case <-time.After(30 * time.Millisecond):
	}
	activeCancel()
	select {
	case err := <-callDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatal("native cancellation did not propagate")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("active call did not cancel")
	}
	waitSignal(t, canceled, "server did not observe canceled call")
	if requests.Load() != 5 || badRequests.Load() != 0 {
		t.Fatalf("wire requests=%d invalid=%d", requests.Load(), badRequests.Load())
	}
}

func TestNativeRedirectAndInstancePins(t *testing.T) {
	var sentinelRequests, sentinelConnections, sentinelBadAuth, sameHits atomic.Int64
	sentinel := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sentinelRequests.Add(1)
		if r.Header.Get("Authorization") != "Bearer instance-two" {
			sentinelBadAuth.Add(1)
		}
		_, _ = io.Copy(io.Discard, r.Body)
		writeReply(w)
	}))
	sentinel.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			sentinelConnections.Add(1)
		}
	}
	sentinel.Start()
	defer sentinel.Close()
	for _, name := range []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "all_proxy"} {
		t.Setenv(name, sentinel.URL)
	}
	t.Setenv("NO_PROXY", "")
	t.Setenv("no_proxy", "")

	type redirect struct {
		status   int
		location string
	}
	var rule atomic.Value
	rule.Store(redirect{})
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		if r.URL.Path == "/same-destination" {
			sameHits.Add(1)
			writeReply(w)
			return
		}
		value := rule.Load().(redirect)
		if value.status != 0 {
			http.Redirect(w, r, value.location, value.status)
			return
		}
		writeReply(w)
	}))
	defer origin.Close()
	endpoint := origin.URL + "/v1"
	client, err := NewLoopbackOpenAI(endpoint, testConfig(endpoint))
	if err != nil {
		t.Fatal(err)
	}
	defer client.CloseIdleConnections()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var dnsStarts atomic.Int64
	ctx = httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{DNSStart: func(httptrace.DNSStartInfo) { dnsStarts.Add(1) }})
	// 先建立可复用连接；后续跨authority重定向不能通过该连接绕过目标限制。
	expectReply(t, client, ctx)
	other, _ := url.Parse(sentinel.URL)
	cases := []struct {
		name     string
		status   int
		location string
		stream   bool
	}{
		{"cross-port-302", 302, sentinel.URL + "/leak", false},
		{"cross-port-307", 307, sentinel.URL + "/leak", false},
		{"cross-port-stream-308", 308, sentinel.URL + "/leak", true},
		{"hostname-303", 303, "http://localhost:" + other.Port() + "/leak", false},
		{"other-loopback-ip", 307, "http://127.0.0.2:" + other.Port() + "/leak", false},
		{"https-cross-port", 307, "https://" + other.Host + "/leak", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rule.Store(redirect{tc.status, tc.location})
			var err error
			if tc.stream {
				var events <-chan provider.ChatCompletionStreamEvent
				events, err = client.ChatCompletionStream(ctx, testRequest())
				if events != nil {
					for range events {
					}
				}
			} else {
				_, err = client.ChatCompletion(ctx, testRequest())
			}
			if !errors.Is(err, ErrDestination) {
				t.Fatalf("cross-destination redirect was not denied by pin: %v", err)
			}
			if sentinelRequests.Load() != 0 || sentinelConnections.Load() != 0 || dnsStarts.Load() != 0 {
				t.Fatal("blocked destination/proxy/DNS was touched")
			}
		})
	}
	rule.Store(redirect{307, "/same-destination"})
	expectReply(t, client, ctx)
	if sameHits.Load() != 1 {
		t.Fatal("same-destination redirect boundary was not observed")
	}

	// 第二实例可以访问它自己独立获准的目标，不会改变第一个实例的pin。
	otherEndpoint := sentinel.URL + "/v1"
	otherConfig := testConfig(otherEndpoint)
	otherConfig.APIKey = "instance-two"
	otherClient, err := NewLoopbackOpenAI(otherEndpoint, otherConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer otherClient.CloseIdleConnections()
	expectReply(t, otherClient, ctx)
	if sentinelRequests.Load() != 1 || sentinelBadAuth.Load() != 0 {
		t.Fatal("second instance did not use its independent configuration")
	}
	rule.Store(redirect{307, otherEndpoint + "/chat/completions"})
	client.CloseIdleConnections()
	if _, err := client.ChatCompletion(ctx, testRequest()); !errors.Is(err, ErrDestination) {
		t.Fatal("second instance or reconnect changed first pin")
	}
	if sentinelRequests.Load() != 1 || sentinelConnections.Load() != 1 || dnsStarts.Load() != 0 {
		t.Fatal("first instance escaped into second destination")
	}

	// 真实factory忽略传入的upstream dial；只连自己解析并冻结的目标。
	upstreamCalls := 0
	u, _ := url.Parse(pinScheme + "://" + strings.TrimPrefix(origin.URL, "http://"))
	dial, err := pinFactory(u, func(context.Context, string, string) (net.Conn, error) {
		upstreamCalls++
		return nil, fmt.Errorf("untrusted upstream dial used")
	})
	if err != nil {
		t.Fatal(err)
	}
	connection, err := dial(ctx, "tcp", u.Host)
	if err != nil {
		t.Fatal("private standard dialer did not connect")
	}
	_ = connection.Close()
	if upstreamCalls != 0 {
		t.Fatal("factory delegated to mutable DefaultDial/upstream")
	}
}
