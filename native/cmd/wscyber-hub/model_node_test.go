package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/chainreactors/cyber/agent/provider"
	agentsession "github.com/chainreactors/cyber/agent/session"
	"github.com/chainreactors/cyber/aop"
	ptypb "github.com/chainreactors/cyber/aop/pty"
	"github.com/chainreactors/cyber/core/operation"
	coretool "github.com/chainreactors/cyber/core/tool"
	"github.com/chainreactors/cyber/core/types"
	"github.com/chainreactors/cyber/pkg/aopws"
	cfg "github.com/chainreactors/cyber/pkg/config"
	"github.com/chainreactors/cyber/pkg/profile"
	"github.com/chainreactors/cyber/pkg/rpc"
	webpkg "github.com/chainreactors/cyber/pkg/web"
	"github.com/gorilla/websocket"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
)

const (
	modelNodeTestModel   = "h4c-local-protocol-fixture"
	modelNodeTestSession = "h4c-session"
	modelNodeTestTurn    = "h4c-turn"
	modelNodeTestCall    = "h4c-bash-1"
	modelNodeTestMarker  = "WSCYBER_H4C_NATIVE_TOOL"
	modelNodeTestCommand = "/usr/bin/printf 'WSCYBER_H4C_NATIVE_TOOL\\n'"
)

func modelNodeTestConfig(endpoint, key, model string) *types.DistributeConfig {
	return &types.DistributeConfig{Llm: &types.LLMConfig{ActiveProfile: "local", Providers: []*types.LLMProviderConfig{{
		Id: "local", Name: "本机受控协议验收", Provider: "openai", BaseUrl: endpoint, ApiKey: key, Model: model, Timeout: 5,
	}}}}
}

func TestModelNodeModesAndStore(t *testing.T) {
	const endpoint = "http://127.0.0.1:18765/v1"
	for name, alter := range map[string]func(*options){
		"no-local":         func(o *options) { o.localHub = false },
		"fixture-conflict": func(o *options) { o.fixtureNode = true },
		"config-conflict":  func(o *options) { o.modelConfig = true },
		"hostname":         func(o *options) { o.modelEndpoint = "http://localhost:18765/v1" },
		"trailing-path":    func(o *options) { o.modelEndpoint += "/" },
		"without-node":     func(o *options) { o.modelNode = false },
	} {
		t.Run(name, func(t *testing.T) {
			opts, _ := testOptions(t)
			opts.modelNode, opts.modelEndpoint = true, endpoint
			alter(&opts)
			called := false
			if err := serveLocalHub(t.Context(), opts, func(string) { called = true }); err == nil || called {
				t.Fatal("mode reached listener")
			}
			if _, err := os.Stat(opts.stateDir); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("rejected mode created state")
			}
		})
	}
	if !modelEndpointAllowed(endpoint) {
		t.Fatal("valid endpoint rejected")
	}
	store := newModelNodeConfigStore(t.TempDir(), endpoint)
	path, loaded, initial, err := store.GetDistributeConfig(t.Context())
	if err != nil || loaded || len(initial.GetLlm().GetProviders()) != 0 {
		t.Fatal("initial store is not empty")
	}
	first := modelNodeTestConfig(endpoint, "h4c-unit-not-a-credential", modelNodeTestModel)
	prepared, err := store.PrepareDistributeConfig(t.Context(), first)
	if err != nil {
		t.Fatal(err)
	}
	defer store.DiscardDistributeConfig(prepared)
	if err = store.CommitDistributeConfig(t.Context(), prepared); err != nil {
		t.Fatal(err)
	}
	updated := modelNodeTestConfig(endpoint, "", "h4c-edited")
	prepared, err = store.PrepareDistributeConfig(t.Context(), updated)
	if err != nil {
		t.Fatal("empty key was rejected before native preservation")
	}
	defer store.DiscardDistributeConfig(prepared)
	if prepared.Config.Llm.Providers[0].ApiKey != first.Llm.Providers[0].ApiKey {
		t.Fatal("native secret preservation failed")
	}
	if err = store.CommitDistributeConfig(t.Context(), prepared); err != nil {
		t.Fatal(err)
	}
	before := modelReadFile(t, path)
	for name, change := range map[string]func(*types.LLMProviderConfig){
		"proxy":          func(p *types.LLMProviderConfig) { p.Proxy = "direct" },
		"blank-proxy":    func(p *types.LLMProviderConfig) { p.Proxy = " " },
		"internal-proxy": func(p *types.LLMProviderConfig) { p.Proxy = "wscyber-model-pin://127.0.0.1:18765" },
		"other-endpoint": func(p *types.LLMProviderConfig) { p.BaseUrl = "http://127.0.0.1:18766/v1" },
		"provider":       func(p *types.LLMProviderConfig) { p.Provider = "anthropic" },
		"new-empty-key":  func(p *types.LLMProviderConfig) { p.Id = "new" },
	} {
		t.Run(name, func(t *testing.T) {
			bad := proto.CloneOf(updated)
			change(bad.Llm.Providers[0])
			bad.Llm.ActiveProfile = bad.Llm.Providers[0].Id
			candidate, err := store.PrepareDistributeConfig(t.Context(), bad)
			if candidate != nil {
				store.DiscardDistributeConfig(candidate)
			}
			if err == nil || !bytes.Equal(before, modelReadFile(t, path)) {
				t.Fatal("bad config accepted or file changed")
			}
		})
	}
	_, _, stored, err := store.GetDistributeConfig(t.Context())
	if err != nil || stored.Llm.Providers[0].Proxy != "" || bytes.Contains(before, []byte("wscyber-model-pin")) {
		t.Fatal("internal pin in user authority")
	}
}

func TestModelNodeProfileAdmissionAndOwnership(t *testing.T) {
	var requests atomic.Int32
	arrived, canceled := make(chan struct{}, 1), make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body) // 读完body，net/http才能观察客户端取消连接。
		requests.Add(1)
		select {
		case arrived <- struct{}{}:
		default:
		}
		<-r.Context().Done()
		select {
		case canceled <- struct{}{}:
		default:
		}
	}))
	defer server.Close()
	endpoint := server.URL + "/v1"
	webapiCapability := newSchemaCapability(t)
	defer webapiCapability.Close(context.Background())
	p, err := newModelNodeProfile(profile.Request{ProviderMode: provider.StartupDisabled, Session: &agentsession.Config{}}, t.TempDir(), endpoint, webapiCapability)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close(context.Background())
	store := newModelNodeConfigStore(t.TempDir(), endpoint)
	p.desired = modelNodeDesired(store, &cfg.Option{Context: store.native.Runtime.Context, AgentOptions: cfg.AgentOptions{Timeout: 3600}})
	commitDesired := func(config provider.ProviderConfig) {
		t.Helper()
		value := modelNodeTestConfig(endpoint, config.APIKey, config.Model)
		entry := value.Llm.Providers[0]
		entry.Images = cloneModelNodeConfig(config).Images
		entry.MaxTokens, entry.ContextWindow, entry.Timeout = int32(config.MaxTokens), int32(config.ContextWindow), int32(config.Timeout)
		prepared, err := store.PrepareDistributeConfig(t.Context(), value)
		if err != nil {
			t.Fatal(err)
		}
		defer store.DiscardDistributeConfig(prepared)
		if err = store.CommitDistributeConfig(t.Context(), prepared); err != nil {
			t.Fatal(err)
		}
	}
	if err = p.Load(t.Context()); err != nil {
		t.Fatal(err)
	}
	state, _ := p.Providers()
	if active, _ := state.Current(); active != nil || state.Health().State != provider.HealthNotConfigured {
		t.Fatal("initial provider is configured")
	}
	if err = p.ReloadProvider(t.Context(), provider.ProviderConfig{Timeout: 120}); err != nil || state.Health().State != provider.HealthNotConfigured {
		t.Fatal("initial empty broadcast falsely configured provider")
	}
	if _, err = newModelNodeProfile(profile.Request{ProviderMode: provider.StartupOptional, Session: &agentsession.Config{}}, t.TempDir(), endpoint, webapiCapability); err == nil {
		t.Fatal("raw optional construction accepted")
	}

	mux := aop.NewNamespaceMux(t.Context())
	defer func() { mux.Cancel(); _ = mux.Close(context.Background()) }()
	if err = p.RegisterNamespaces(mux); err != nil {
		t.Fatal(err)
	}
	dispatch := func(message proto.Message) proto.Message {
		t.Helper()
		envelope, err := aop.Wrap("h4c-admission", "", message)
		if err != nil {
			t.Fatal(err)
		}
		var answer proto.Message
		handled, err := mux.Dispatch(envelope, func(response *aop.Envelope) error { answer, err = aop.Unwrap(response); return err })
		if err != nil || !handled {
			t.Fatal("native binding not dispatched")
		}
		return answer
	}
	unknown := &anypb.Any{TypeUrl: "type.invalid/model.override", Value: []byte("untrusted-model")}
	opened := dispatch(&aop.ProtocolMessage{Message: &aop.ProtocolMessage_OpenSessionRequest{OpenSessionRequest: &aop.OpenSessionRequest{SessionId: "admission", Extensions: []*anypb.Any{unknown}}}}).(*aop.ProtocolMessage)
	if opened.GetOpenSessionResponse().GetRejected() == nil {
		t.Fatal("Open override accepted")
	}
	opened = dispatch(&aop.ProtocolMessage{Message: &aop.ProtocolMessage_OpenSessionRequest{OpenSessionRequest: &aop.OpenSessionRequest{SessionId: "admission"}}}).(*aop.ProtocolMessage)
	if opened.GetOpenSessionResponse().GetAccepted() == nil {
		t.Fatal("native Open not delegated")
	}
	base := &aop.RunTurnRequest{SessionId: "admission", TurnId: "not-run", Input: provider.TextMessage("user", "not executed"), MaxTurns: 2}
	if answer := dispatch(&aop.ProtocolMessage{Message: &aop.ProtocolMessage_RunTurnRequest{RunTurnRequest: base}}).(*aop.ProtocolMessage); answer.GetRunTurnResponse().GetRejected() == nil {
		t.Fatal("unconfigured Run accepted")
	}
	for _, line := range []string{"/model another-model", "/eval criteria", "!tmux send-keys -t main-repl /model Enter"} {
		answer := dispatch(&types.CommandProtocolMessage{Message: &types.CommandProtocolMessage_Request{Request: &types.CommandRequest{SessionId: "admission", Line: line}}}).(*aop.ProtocolMessage)
		if answer.GetProtocolError().GetCode() != "FAILED_PRECONDITION" {
			t.Fatal("command override accepted")
		}
	}
	ptyEnvelope, err := aop.Wrap("no-pty", "", &ptypb.ProtocolMessage{})
	if err != nil {
		t.Fatal(err)
	}
	if handled, _ := mux.Dispatch(ptyEnvelope, func(*aop.Envelope) error { return nil }); handled {
		t.Fatal("PTY namespace exposed")
	}

	tools, _ := p.Tools()
	// 实际走shell展开→原生registry→BeforeCommand，不只是手动调用hook。
	// 任务显式关闭审批，避免执行前的动态展开审批先拦截，只验证原生命令边界。
	commandCtx := operation.ContextWithInvocation(t.Context(), operation.Invocation{SessionID: approvalOffSession(t, webapiCapability), TurnID: "tmux-turn", CallID: "tmux-call"})
	args, _ := json.Marshal(map[string]any{"command": "control=tmux; \"$control\" send-keys -t main-repl '/model other' Enter", "timeout": 2})
	result, toolErr := tools.ExecuteTool(commandCtx, "bash", string(args))
	if !strings.Contains(coretool.ResultText(result)+fmt.Sprint(toolErr), "暂不支持tmux") {
		t.Fatal("expanded tmux was not denied at native command boundary")
	}
	result, toolErr = tools.ExecuteTool(t.Context(), "subagent", `{"prompt":"not executed"}`)
	if !strings.Contains(coretool.ResultText(result)+fmt.Sprint(toolErr), "多Agent") {
		t.Fatal("subagent was not denied")
	}
	if requests.Load() != 0 {
		t.Fatal("admission sent model request")
	}

	images := false
	config := provider.ProviderConfig{Provider: "openai", Model: "model-a", APIKey: "local-test-not-credential", BaseURL: endpoint, Timeout: 5, Images: &images}
	commitDesired(config)
	if err = p.ReloadProvider(t.Context(), config); err != nil {
		t.Fatal(err)
	}
	first, resolved := state.Current()
	if state.Health().State != provider.HealthConfigured || len(state.Fallbacks()) != 0 || resolved.Proxy == "" || config.Proxy != "" {
		t.Fatal("provider state or config isolation")
	}
	if err = p.ReloadProvider(t.Context(), cloneModelNodeConfig(config)); err != nil || len(p.clients) != 1 {
		t.Fatal("equal Images values allocated new client")
	}
	images = true
	if *p.clients[0].original.Images {
		t.Fatal("original config shares Images")
	}

	for _, change := range []func(*aop.RunTurnRequest){
		func(r *aop.RunTurnRequest) { r.Extensions = []*anypb.Any{unknown} },
		func(r *aop.RunTurnRequest) { r.ContinueSession = true },
		func(r *aop.RunTurnRequest) { r.MaxTurns = 0 },
		func(r *aop.RunTurnRequest) { r.MaxTurns = modelNodeMaxTurns + 1 },
		func(r *aop.RunTurnRequest) { r.ProtoReflect().SetUnknown([]byte{0xfa, 0x03, 0x01, 'x'}) },
	} {
		request := proto.CloneOf(base)
		change(request)
		if answer := dispatch(&aop.ProtocolMessage{Message: &aop.ProtocolMessage_RunTurnRequest{RunTurnRequest: request}}).(*aop.ProtocolMessage); answer.GetRunTurnResponse().GetRejected() == nil {
			t.Fatal("unsupported Run accepted")
		}
	}
	if requests.Load() != 0 {
		t.Fatal("configuration or override triggered probe")
	}

	// 在真实原生Session请求悬挂时替换；旧client留到Runtime取消/排空之后。
	run, err := p.runtime.RunSession(t.Context(), "admission", agentsession.RunInput{TurnID: "cancel-on-profile-close", Message: provider.TextMessage("user", "local cancellation fixture"), MaxTurns: 1})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-arrived:
	case <-time.After(5 * time.Second):
		t.Fatal("native request did not arrive")
	}
	for _, model := range []string{"model-b", "model-c", "model-d"} {
		config.Model = model
		commitDesired(config)
		if err = p.ReloadProvider(t.Context(), config); err != nil {
			t.Fatal(err)
		}
	}
	if p.clients[0].client != first || p.released != 0 {
		t.Fatal("old borrowed provider released before session drain")
	}
	config.Model = "model-e"
	commitDesired(config)
	if err = p.ReloadProvider(t.Context(), config); !errors.Is(err, errModelNodeLimit) || len(p.clients) != 4 {
		t.Fatal("unbounded provider retention")
	}
	_, current := state.Current()
	if current.Model != "model-d" || p.AgentStatus().ConfigError == "" {
		t.Fatal("rejected apply replaced provider or hid error")
	}
	// 只改唯一store回到已应用A，不调用Reload，覆盖原生unchanged的状态语义。
	commitDesired(p.clients[3].original)
	if status := p.AgentStatus(); status.ConfigError != "" || status.Model != "model-d" || len(p.clients) != 4 {
		t.Fatal("return to applied config retained historical error")
	}
	configPath := store.native.Explicit
	committed := modelReadFile(t, configPath)
	if err = os.WriteFile(configPath, []byte("llm: [invalid yaml"), 0600); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(p.AgentStatus().ConfigError, "无法核对") {
		t.Fatal("unreadable desired config reported applied")
	}
	if err = os.WriteFile(configPath, committed, 0600); err != nil {
		t.Fatal(err)
	}
	cancelCtx, cancel := context.WithCancel(t.Context())
	cancel()
	if err = p.Close(cancelCtx); err == nil || p.released != 0 {
		t.Fatal("canceled close treated as drain")
	}
	if err = p.ReloadProvider(t.Context(), p.clients[0].original); !errors.Is(err, errModelNodeClosed) {
		t.Fatal("reload after close admission")
	}
	closeCtx, closeCancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer closeCancel()
	if err = p.Close(closeCtx); err != nil {
		t.Fatal(err)
	}
	if _, err = run.Wait(); !errors.Is(err, context.Canceled) {
		t.Fatal("runtime call did not cancel")
	}
	select {
	case <-canceled:
	case <-time.After(3 * time.Second):
		t.Fatal("model request context not canceled")
	}
	if !p.closed || p.released != 4 || p.runtime.Active() || requests.Load() != 1 {
		t.Fatal("native drain or client ownership failed")
	}
	if err = p.Close(context.Background()); err != nil || p.released != 4 {
		t.Fatal("close not idempotent")
	}
}

// 只供主预先编译后连接一个实际CLI；无环境时跳过，不创建Hub或模型服务。
func TestModelNodeExternal(t *testing.T) {
	addr := os.Getenv("WSCYBER_H4C_ADDRESS")
	if addr == "" {
		t.Skip("compiled external native model-node acceptance only")
	}
	endpoint, evidence := os.Getenv("WSCYBER_H4C_ENDPOINT"), os.Getenv("WSCYBER_H4C_EVIDENCE")
	if !fixtureAddressAllowed(addr) || !modelEndpointAllowed(endpoint) || evidence == "" {
		t.Fatal("literal local endpoints/evidence required")
	}
	key, err := readPrivateKey(os.Getenv("WSCYBER_H4C_KEY_FILE"))
	if err != nil {
		t.Fatal(err)
	}
	modelKey, err := readPrivateKey(os.Getenv("WSCYBER_H4C_MODEL_KEY_FILE"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 35*time.Second)
	defer cancel()
	transport := &http.Transport{Proxy: nil}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 4 * time.Second}
	base := "http://" + addr
	agents, configs := rpc.NewAgentServiceClient(client, base), rpc.NewConfigServiceClient(client, base)
	waitAgent := func(model string, failed bool) *types.AgentView {
		t.Helper()
		ticker := time.NewTicker(40 * time.Millisecond)
		defer ticker.Stop()
		for {
			request := connect.NewRequest(&types.ListAgentsRequest{})
			request.Header().Set("Authorization", "Bearer "+key)
			response, err := agents.ListAgents(ctx, request)
			if err != nil {
				t.Fatal("native ListAgents failed")
			}
			if len(response.Msg.Agents) > 1 {
				t.Fatal("unexpected additional node")
			}
			for _, candidate := range response.Msg.Agents {
				status := candidate.GetStatus()
				if candidate.GetHello().GetNodeId() == modelNodeID && status.GetModel() == model && (status.GetConfigError() != "") == failed {
					return proto.CloneOf(candidate)
				}
			}
			select {
			case <-ticker.C:
			case <-ctx.Done():
				t.Fatal("native configuration application timeout")
			}
		}
	}
	input := bufio.NewReader(os.Stdin)
	checkpoint := func(stage string) {
		t.Helper()
		fmt.Println(stage) // 固定无秘密标记；driver此刻核fixture零请求再放行。
		ack, err := input.ReadString('\n')
		if err != nil || ack != "continue\n" {
			t.Fatal("fixture checkpoint not acknowledged")
		}
	}
	empty := waitAgent("", false)
	if empty.GetStatus().GetProvider() != "" {
		t.Fatal("initial empty node synthesized a provider")
	}
	checkpoint("H4C_EMPTY_NODE")

	dialer := websocket.Dialer{HandshakeTimeout: 3 * time.Second}
	conn, response, err := dialer.DialContext(ctx, "ws://"+addr+webpkg.ApplicationWebSocketPath, http.Header{"Authorization": {"Bearer " + key}})
	if response != nil {
		response.Body.Close()
	}
	if err != nil {
		t.Fatal("native application WebSocket failed")
	}
	defer conn.Close()
	conn.SetReadDeadline(time.Now().Add(30 * time.Second))
	stream, err := aopws.New(ctx, conn, aopws.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	wire := &wireProbe{t: t, stream: stream} // 复用H2原生编解码，不复用其固定模型/执行链。
	open := func(id string, extensions ...*anypb.Any) *aop.OpenSessionResponse {
		return wire.call(&aop.ProtocolMessage{Message: &aop.ProtocolMessage_OpenSessionRequest{OpenSessionRequest: &aop.OpenSessionRequest{NodeId: modelNodeID, SessionId: id, Extensions: extensions}}}).GetOpenSessionResponse()
	}
	unknown := &anypb.Any{TypeUrl: "type.invalid/model.override", Value: []byte("rejected")}
	if open("h4c-rejected-open", unknown).GetRejected() == nil {
		t.Fatal("Open extensions accepted")
	}
	if open("h4c-negative").GetAccepted() == nil {
		t.Fatal("native negative-test session not opened")
	}
	rejectRun := func(turn string, change func(*aop.RunTurnRequest)) {
		t.Helper()
		request := &aop.RunTurnRequest{SessionId: "h4c-negative", TurnId: turn, Input: provider.TextMessage("user", "not executed"), MaxTurns: 2}
		if change != nil {
			change(request)
		}
		result := wire.call(&aop.ProtocolMessage{Message: &aop.ProtocolMessage_RunTurnRequest{RunTurnRequest: request}}).GetRunTurnResponse()
		// Hub先持久接收，再异步转发。必须等原生节点拒绝所生成的失败终态，
		// 不能把Hub Accepted当节点接受，更不能把它改成同步伪拒绝。
		if result.GetAccepted().GetTurnId() != turn {
			t.Fatal("Hub did not return native receipt")
		}
		ticker := time.NewTicker(30 * time.Millisecond)
		defer ticker.Stop()
		for {
			history := wire.call(&aop.ProtocolMessage{Message: &aop.ProtocolMessage_ListEventsRequest{ListEventsRequest: &aop.ListEventsRequest{SessionId: "h4c-negative", Limit: 100}}}).GetListEventsResponse()
			for _, delivery := range history.GetEvents() {
				event := delivery.GetEvent()
				if event.GetTurnId() != turn {
					continue
				}
				if event.GetToolCall() != nil || event.GetToolResult() != nil {
					t.Fatal("rejected turn executed a tool")
				}
				if end := event.GetTurnEnded(); end != nil {
					if !modelNodeRejectedTurn(end) {
						t.Fatal("node rejection not reflected in native terminal event")
					}
					return
				}
			}
			select {
			case <-ticker.C:
			case <-ctx.Done():
				t.Fatal("node rejection not persisted")
			}
		}
	}
	rejectRun("h4c-unconfigured", nil)
	value := modelNodeTestConfig(endpoint, modelKey, modelNodeTestModel)
	save := func(config *types.DistributeConfig) *types.ConfigView {
		t.Helper()
		request := connect.NewRequest(&types.UpdateConfigRequest{Config: config})
		request.Header().Set("Authorization", "Bearer "+key)
		response, err := configs.UpdateConfig(ctx, request)
		if err != nil {
			t.Fatal("native configuration save failed")
		}
		return response.Msg.Config
	}
	view := save(value)
	if view.GetLlm().GetActive().GetModel() != modelNodeTestModel || !view.GetLlm().GetActive().GetApiKeyConfigured() {
		t.Fatal("configuration view not saved")
	}
	applied := waitAgent(modelNodeTestModel, false)
	if applied.GetStatus().GetProvider() != "openai" {
		t.Fatal("wrong applied provider")
	}
	// 保存空key同配置，必须原生保留且不产生新实例/请求。
	value.Llm.Providers[0].ApiKey = ""
	save(value)
	for _, method := range []string{"TestLLM", "ListModels", "TestConnection"} {
		body := bytes.NewBufferString("{}")
		request, _ := http.NewRequestWithContext(ctx, http.MethodPost, base+"/cyber.rpc.config.ConfigService/"+method, body)
		request.Header.Set("Authorization", "Bearer "+key)
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Connect-Protocol-Version", "1")
		response, err := client.Do(request)
		if err != nil {
			t.Fatal("probe denial transport failed")
		}
		var denied map[string]any
		err = json.NewDecoder(response.Body).Decode(&denied)
		response.Body.Close()
		if err != nil || denied["code"] != "failed_precondition" {
			t.Fatal("probe was not explicitly denied")
		}
	}
	rejectRun("h4c-override", func(r *aop.RunTurnRequest) { r.Extensions = []*anypb.Any{unknown} })
	eval, _ := anypb.New(&types.AgentRunOptions{EvalCriteria: "not executed"})
	rejectRun("h4c-eval", func(r *aop.RunTurnRequest) { r.Extensions = []*anypb.Any{eval} })
	rejectRun("h4c-continue", func(r *aop.RunTurnRequest) { r.ContinueSession = true })
	rejectRun("h4c-unbounded", func(r *aop.RunTurnRequest) { r.MaxTurns = 0 })
	closed := wire.call(&aop.ProtocolMessage{Message: &aop.ProtocolMessage_CloseSessionRequest{CloseSessionRequest: &aop.CloseSessionRequest{SessionId: "h4c-negative", Reason: "completed"}}})
	if closed.GetCloseSessionResponse().GetAccepted() == nil {
		t.Fatal("negative session close failed")
	}
	checkpoint("H4C_CONFIG_APPLIED")
	if open(modelNodeTestSession).GetAccepted() == nil {
		t.Fatal("native execution session not opened")
	}
	watchID := wire.send(&aop.ProtocolMessage{Message: &aop.ProtocolMessage_WatchEventsRequest{WatchEventsRequest: &aop.WatchEventsRequest{SessionId: modelNodeTestSession}}})
	run := wire.call(&aop.ProtocolMessage{Message: &aop.ProtocolMessage_RunTurnRequest{RunTurnRequest: &aop.RunTurnRequest{SessionId: modelNodeTestSession, TurnId: modelNodeTestTurn, MaxTurns: modelNodeMaxTurns, Input: provider.TextMessage("user", "H4C local wire fixture: one fixed tool, no target")}}})
	if run.GetRunTurnResponse().GetAccepted().GetTurnId() != modelNodeTestTurn {
		t.Fatal("native turn not accepted")
	}
	ended := func() bool {
		for _, envelope := range wire.watch {
			message, _ := aop.Unwrap(envelope)
			event := message.(*aop.ProtocolMessage).GetEvent()
			if event.GetTurnId() == modelNodeTestTurn && event.GetTurnEnded() != nil {
				return true
			}
		}
		return false
	}
	for !ended() {
		wire.recv()
	}
	history := func() []*aop.EventDelivery {
		return wire.call(&aop.ProtocolMessage{Message: &aop.ProtocolMessage_ListEventsRequest{ListEventsRequest: &aop.ListEventsRequest{SessionId: modelNodeTestSession, Limit: 100}}}).GetListEventsResponse().GetEvents()
	}
	items := history()
	// 先保留实际原生事件，再做断言；失败也不能丢失用于定位的终态。
	writeProtoEvidence(t, filepath.Join(evidence, "precheck-hub-events.jsonl"), items)
	writeProtoEvidence(t, filepath.Join(evidence, "precheck-watch-envelopes.jsonl"), wire.watch)
	verifyModelNodeEvents(t, items, wire.watch)
	closed = wire.call(&aop.ProtocolMessage{Message: &aop.ProtocolMessage_CloseSessionRequest{CloseSessionRequest: &aop.CloseSessionRequest{SessionId: modelNodeTestSession, Reason: "completed"}}})
	if closed.GetCloseSessionResponse().GetAccepted() == nil {
		t.Fatal("execution session close failed")
	}
	ticker := time.NewTicker(30 * time.Millisecond)
	defer ticker.Stop()
	for {
		items = history()
		found := false
		for _, item := range items {
			if item.GetEvent().GetSessionEnded() != nil {
				found = true
			}
		}
		if found {
			break
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			t.Fatal("session close not persisted")
		}
	}
	wire.send(&aop.ProtocolMessage{Message: &aop.ProtocolMessage_CancelOperation{CancelOperation: &aop.CancelOperation{TargetId: watchID}}})
	if err = stream.Close(); err != nil {
		t.Fatal(err)
	}

	// 原生保存与应用分开核验：第5份保存成功，但节点仍使用第4份并报告应用失败。
	for _, model := range []string{"h4c-second", "h4c-third", "h4c-fourth"} {
		value.Llm.Providers[0].Model = model
		save(value)
		waitAgent(model, false)
	}
	value.Llm.Providers[0].Model = "h4c-over-limit"
	view = save(value)
	if view.GetLlm().GetActive().GetModel() != "h4c-over-limit" {
		t.Fatal("save was not acknowledged")
	}
	rejectedApply := waitAgent("h4c-fourth", true)
	if !strings.Contains(rejectedApply.GetStatus().GetConfigError(), "未") {
		t.Fatal("node apply mismatch missing")
	}
	// 回A与node.currentConfig相同，原生短路不再调用ReloadProvider。
	value.Llm.Providers[0].Model = "h4c-fourth"
	save(value)
	restored := waitAgent("h4c-fourth", false)
	writeProtoEvidence(t, filepath.Join(evidence, "hub-events.jsonl"), items)
	writeProtoEvidence(t, filepath.Join(evidence, "watch-envelopes.jsonl"), wire.watch)
	writeProtoEvidence(t, filepath.Join(evidence, "agent-states.jsonl"), []*types.AgentView{empty, applied, rejectedApply, restored})
	summary, _ := json.MarshalIndent(map[string]any{"success": true, "node_id": modelNodeID, "session_id": modelNodeTestSession, "turn_id": modelNodeTestTurn, "call_id": modelNodeTestCall, "tool_output": modelNodeTestMarker, "stop_reason": "completed", "native_websocket": true, "durable_events": len(items), "watch_envelopes": len(wire.watch), "saved_but_apply_rejected": true, "unchanged_return_to_applied_cleared_error": true, "session_closed": true}, "", "  ")
	if err = os.WriteFile(filepath.Join(evidence, "flow-summary.json"), append(summary, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	files, _ := os.ReadDir(evidence)
	for _, file := range files {
		if file.IsDir() {
			continue
		}
		data := modelReadFile(t, filepath.Join(evidence, file.Name()))
		if bytes.Contains(data, []byte(key)) || bytes.Contains(data, []byte(modelKey)) || bytes.Contains(data, []byte("wscyber-model-pin")) {
			t.Fatal("internal config leaked in evidence")
		}
	}
}

func TestModelNodeTurnBudgetBoundary(t *testing.T) {
	for _, turns := range []uint32{0, 1, 2, 3, 4} {
		message := &aop.ProtocolMessage{Message: &aop.ProtocolMessage_RunTurnRequest{RunTurnRequest: &aop.RunTurnRequest{MaxTurns: turns}}}
		want := turns >= 1 && turns <= 3
		if modelNodeCoreAllowed(message) != want {
			t.Fatalf("unexpected local turn budget admission: %d", turns)
		}
	}
}

// 原生broadcastHubTurnEnded使用stop_reason=error，具体原因在error.code，不能混同。
func modelNodeRejectedTurn(end *aop.TurnEnded) bool {
	return end.GetStopReason() == "error" && end.GetError().GetCode() == "agent_run_failed" && end.GetError().GetMessage() != ""
}

func TestModelNodeRejectedTurnSemantics(t *testing.T) {
	valid := &aop.TurnEnded{StopReason: "error", Error: &aop.ProtocolError{Code: "agent_run_failed", Message: "node rejected"}}
	if !modelNodeRejectedTurn(valid) {
		t.Fatal("native rejection shape not recognized")
	}
	for _, invalid := range []*aop.TurnEnded{
		nil,
		{StopReason: "agent_run_failed", Error: valid.Error},
		{StopReason: "completed", Error: valid.Error},
		{StopReason: "error", Error: &aop.ProtocolError{Code: "agent_disconnected", Message: "not this case"}},
		{StopReason: "error", Error: &aop.ProtocolError{Code: "agent_run_failed"}},
	} {
		if modelNodeRejectedTurn(invalid) {
			t.Fatal("non-matching terminal event treated as node rejection")
		}
	}
}

func verifyModelNodeEvents(t *testing.T, items []*aop.EventDelivery, live []*aop.Envelope) {
	t.Helper()
	calls, results, starts, ends := 0, 0, 0, 0
	byID := make(map[string]*aop.Event)
	for _, item := range items {
		event := item.GetEvent()
		if item.Cursor == "" || event.GetId() == "" || event.GetSessionId() != modelNodeTestSession {
			t.Fatal("native event identity missing")
		}
		byID[event.Id] = event
		if event.TurnId != modelNodeTestTurn {
			continue
		}
		if event.GetTurnStarted() != nil {
			starts++
		}
		if call := event.GetToolCall(); call != nil {
			calls++
			args, err := aop.DecodeJSON[map[string]any](call.Arguments)
			if err != nil || call.Id != modelNodeTestCall || call.Name != "bash" || args["command"] != modelNodeTestCommand {
				t.Fatal("wrong model-issued tool call")
			}
		}
		if result := event.GetToolResult(); result != nil {
			results++
			if result.CallId != modelNodeTestCall || result.IsError || !strings.Contains(coretool.ResultText(result), modelNodeTestMarker) {
				t.Fatal("actual native tool result missing")
			}
		}
		if end := event.GetTurnEnded(); end != nil {
			ends++
			if end.StopReason != "completed" {
				t.Fatalf("native turn did not complete: stop_reason=%q error_code=%q", end.StopReason, end.GetError().GetCode())
			}
		}
	}
	if calls != 1 || results != 1 || starts != 1 || ends != 1 {
		t.Fatal("native call/result/turn counts differ")
	}
	matched := 0
	for _, envelope := range live {
		message, _ := aop.Unwrap(envelope)
		event := message.(*aop.ProtocolMessage).GetEvent()
		if event.GetToolCall() != nil || event.GetToolResult() != nil || event.GetTurnEnded() != nil {
			if envelope.DeliveryCursor == "" || !proto.Equal(event, byID[event.Id]) {
				t.Fatal("live vs durable mismatch")
			}
			matched++
		}
	}
	if matched != 3 {
		t.Fatal("live native events missing")
	}
}
