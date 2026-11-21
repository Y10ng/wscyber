package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chainreactors/cyber/agent/provider"
	agentsession "github.com/chainreactors/cyber/agent/session"
	coretool "github.com/chainreactors/cyber/core/tool"
	"github.com/chainreactors/cyber/core/types"
	"github.com/chainreactors/cyber/pkg/profile"
	"github.com/chainreactors/cyber/pkg/rpc"

	"wscyber/native/webapi"
)

// 一个端到端短专项覆盖正式分流、保存不探测、原生应用回执、同库重开及显式探测。
func TestProductModelNativeEntryAndReopen(t *testing.T) {
	var requests atomic.Int32
	canary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"native-model"}]}`))
	}))
	defer canary.Close()
	opts, key := testOptions(t)
	opts.modelNode = true
	config := modelNodeTestConfig(canary.URL+"/v1", "product-test-not-credential", "native-model")
	providerCount := func(address string) int {
		client := &http.Client{Transport: &http.Transport{Proxy: nil}, Timeout: 3 * time.Second}
		defer client.CloseIdleConnections()
		agents := rpc.NewAgentServiceClient(client, "http://"+address)
		deadline := time.Now().Add(8 * time.Second)
		for {
			listed, err := agents.ListAgents(t.Context(), modelAuthRequest(&types.ListAgentsRequest{}, key))
			if err == nil && len(listed.Msg.GetAgents()) == 1 {
				agent := listed.Msg.GetAgents()[0]
				if !slices.Contains(agent.GetHello().GetCapabilities(), "wscyber.model.v1") ||
					!slices.Contains(agent.GetHello().GetCapabilities(), "wscyber.webapi-http.v1") ||
					slices.Contains(agent.GetHello().GetCapabilities(), "wscyber.local-model.v1") {
					t.Fatal("正式节点能力公告不符合契约")
				}
				if agent.GetStatus().GetModel() == "native-model" && agent.GetStatus().GetConfigError() == "" {
					if len(agent.GetCommands()) == 0 {
						t.Fatal("正式节点没有原生会话命令")
					}
					return 1
				}
				if agent.GetStatus().GetModel() == "native-model" && agent.GetStatus().GetConfigError() != "" {
					t.Fatalf("原生节点应用配置失败: %s", agent.GetStatus().GetConfigError())
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if time.Now().After(deadline) {
				t.Fatal("正式节点未登记或未通过原生回执应用配置")
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	run := func() string {
		ctx, cancel := context.WithCancel(t.Context())
		ready, done := make(chan string, 1), make(chan error, 1)
		go func() { done <- serveLocalHub(ctx, opts, func(addr string) { ready <- addr }) }()
		var address string
		select {
		case address = <-ready:
		case err := <-done:
			t.Fatal(err)
		case <-time.After(10 * time.Second):
			t.Fatal("正式 Hub 未就绪")
		}
		base := "http://" + address
		client := &http.Client{Transport: &http.Transport{Proxy: nil}, Timeout: 3 * time.Second}
		defer client.CloseIdleConnections()
		configuration := rpc.NewConfigServiceClient(client, base)
		if _, err := configuration.UpdateConfig(t.Context(), modelAuthRequest(&types.UpdateConfigRequest{Config: config}, key)); err != nil {
			t.Fatal(err)
		}
		providerCount(address)
		if requests.Load() != 0 {
			t.Fatal("启动、保存或配置广播隐式发送了模型请求")
		}
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(12 * time.Second):
			t.Fatal("正式 Hub 未关闭")
		}
		assertNoStateFD(t, opts.stateDir)
		listener, err := net.Listen("tcp", address)
		if err != nil {
			t.Fatal(err)
		}
		_ = listener.Close()
		return address
	}
	firstAddress := run()
	if opts.modelEndpoint != "" {
		t.Fatal("正式分支意外使用验收端点")
	}
	// 同一 state-dir 第二次启动，验证配置已保留且仍不主动探测。
	secondAddress := run()
	if secondAddress == "" || firstAddress == "" {
		t.Fatal("正式运行地址为空")
	}
	// 第三次只验证用户显式 listModels 的原生路径确实能发请求；保存语义已在上面核验。
	ctx, cancel := context.WithCancel(t.Context())
	ready, done := make(chan string, 1), make(chan error, 1)
	go func() { done <- serveLocalHub(ctx, opts, func(addr string) { ready <- addr }) }()
	address := <-ready
	client := &http.Client{Transport: &http.Transport{Proxy: nil}, Timeout: 3 * time.Second}
	defer client.CloseIdleConnections()
	configuration := rpc.NewConfigServiceClient(client, "http://"+address)
	probe := modelNodeTestConfig(canary.URL+"/v1", "product-test-not-credential", "native-model").Llm.Providers[0]
	models, err := configuration.ListModels(t.Context(), modelAuthRequest(&types.LLMProbeRequest{ProfileId: "local", Provider: probe.Provider, BaseUrl: probe.BaseUrl, ApiKey: probe.ApiKey, Model: probe.Model}, key))
	if err != nil || !slices.Contains(models.Msg.GetModels(), "native-model") || requests.Load() != 1 {
		t.Fatalf("显式模型列表未走原生联网路径: models=%v err=%v requests=%d", models, err, requests.Load())
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestProductModelSubagentDenied(t *testing.T) {
	var requests atomic.Int32
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.Error(w, "unexpected model request", http.StatusInternalServerError)
	}))
	defer endpoint.Close()
	capability, err := webapi.NewCapability(t.TempDir() + "/hub.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	defer capability.Close(context.Background())
	p, err := newProductModelProfile(profile.Request{ProviderMode: provider.StartupDisabled, Session: &agentsession.Config{}}, t.TempDir(), capability)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close(context.Background())
	if err := p.Load(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := p.ReloadProvider(t.Context(), provider.ProviderConfig{Provider: "openai", BaseURL: endpoint.URL + "/v1", APIKey: "not-a-credential", Model: "synthetic-model", Timeout: 2}); err != nil {
		t.Fatal(err)
	}
	tools, err := p.Tools()
	if err != nil {
		t.Fatal(err)
	}
	result, toolErr := tools.ExecuteTool(t.Context(), "subagent", `{"prompt":"not executed"}`)
	if !strings.Contains(coretool.ResultText(result)+fmt.Sprint(toolErr), "已拒绝 subagent") {
		t.Fatalf("product subagent was not denied: result=%v err=%v", result, toolErr)
	}
	// 命名 oracle_explorer 由平台准入：无可信会话上下文同样拒绝，且 Catalog 已注册该定义。
	result, toolErr = tools.ExecuteTool(t.Context(), "subagent", `{"name":"oracle_explorer","prompt":"O-001 not executed"}`)
	if !strings.Contains(coretool.ResultText(result)+fmt.Sprint(toolErr), "已拒绝 subagent：缺少可信原生 session/call 上下文") {
		t.Fatalf("product oracle_explorer admission: result=%v err=%v", result, toolErr)
	}
	result, toolErr = tools.ExecuteTool(t.Context(), "subagent", `{"action":"catalog"}`)
	if toolErr != nil || !strings.Contains(coretool.ResultText(result), "oracle_explorer (default=async)") {
		t.Fatalf("product catalog lacks oracle_explorer: result=%v err=%v", result, toolErr)
	}
	if requests.Load() != 0 {
		t.Fatalf("subagent admission sent %d model requests", requests.Load())
	}
}

// 第二个短专项只核对 borrowed Provider 的关闭时机和原生 Runtime 命名空间。
func TestProductModelProfileOwnershipAndNativeProtocol(t *testing.T) {
	stateDir := t.TempDir()
	capability, err := webapi.NewCapability(stateDir + "/hub.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	defer capability.Close(context.Background())
	p, err := newProductModelProfile(profile.Request{ProviderMode: provider.StartupDisabled, Session: &agentsession.Config{}}, t.TempDir(), capability)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Load(t.Context()); err != nil {
		t.Fatal(err)
	}
	// 正式节点接上全工具出口（不是 NoEgress）：Load 即启动代理并在 state-dir/egress-ca 导出 CA。
	if _, err := os.Stat(stateDir + "/egress-ca/mitmproxy-ca-cert.pem"); err != nil {
		t.Fatalf("正式节点未接全工具出口: %v", err)
	}
	state, _ := p.Providers()
	first := provider.ProviderConfig{Provider: "openai", BaseURL: "https://example.invalid/v1", APIKey: "not-a-credential", Model: "model-a"}
	second := provider.ProviderConfig{Provider: "anthropic", BaseURL: "https://example.invalid", APIKey: "not-a-credential", Model: "model-b", Proxy: "http://127.0.0.1:8080"}
	for _, config := range []provider.ProviderConfig{first, second, first} {
		if err := p.ReloadProvider(t.Context(), config); err != nil {
			t.Fatal(err)
		}
	}
	if len(p.clients) != 2 {
		t.Fatal("切换没有复用已构造实例或引入了实例数上限")
	}
	if active, current := state.Current(); active != p.clients[0].client || current.Timeout != 120 || current.Model != "model-a" {
		t.Fatal("原生 State.Set 未发布解析后的正式配置")
	}
	if p.namespaces == nil {
		t.Fatal("正式节点缺少原生会话/命令命名空间")
	}
	if err := p.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !p.closed || p.released != 2 {
		t.Fatal("借给 State 的自有 Provider 未在原生排空后关闭")
	}
	if err := p.ReloadProvider(t.Context(), first); !errors.Is(err, errProductModelNodeClosed) {
		t.Fatal("关闭后仍接受模型配置")
	}
}
