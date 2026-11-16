package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/chainreactors/cyber/agent/provider"
	agentsession "github.com/chainreactors/cyber/agent/session"
	"github.com/chainreactors/cyber/aop"
	coretool "github.com/chainreactors/cyber/core/tool"
	"github.com/chainreactors/cyber/core/types"
	"github.com/chainreactors/cyber/pkg/aopws"
	"github.com/chainreactors/cyber/pkg/profile"
	"github.com/chainreactors/cyber/pkg/rpc"
	webpkg "github.com/chainreactors/cyber/pkg/web"
	webservice "github.com/chainreactors/cyber/pkg/web/service"
	"github.com/gorilla/websocket"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	webapitasks "wscyber/native/webapi"
)

func TestFixtureProfileRejectsUnknownConfig(t *testing.T) {
	report := &fixtureNodeReport{}
	p, err := newFixtureProfile(profile.Request{ProviderMode: provider.StartupDisabled, Session: &agentsession.Config{}}, t.TempDir(), report)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close(context.Background())
	if err = p.Load(t.Context()); err != nil {
		t.Fatal(err)
	}
	state, _ := p.Providers()
	if active, _ := state.Current(); active != nil {
		t.Fatal("provider activated before handshake")
	}
	wrong := fixtureProviderConfig()
	wrong.Model = "not-fixture"
	if err = p.ReloadProvider(t.Context(), wrong); err == nil {
		t.Fatal("unknown config accepted")
	}
	if active, _ := state.Current(); active != nil || p.provider.calls.Load() != 0 {
		t.Fatal("rejected config ran provider")
	}
}

func TestFixtureNodeNativeFlowAndShutdown(t *testing.T) {
	opts, key := testOptions(t)
	opts.fixtureNode, opts.fixtureReport = true, &fixtureNodeReport{}
	capture, err := os.CreateTemp(t.TempDir(), "output-")
	if err != nil {
		t.Fatal(err)
	}
	oldOut, oldErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = capture, capture
	defer func() { os.Stdout, os.Stderr = oldOut, oldErr; capture.Close() }()
	ctx, cancel := context.WithCancel(t.Context())
	ready, done := make(chan string, 1), make(chan error, 1)
	go func() { done <- serveLocalHub(ctx, opts, func(addr string) { ready <- addr }) }()
	joined := false
	defer func() {
		cancel()
		if !joined {
			select {
			case <-done:
			case <-time.After(12 * time.Second):
				t.Error("native host did not drain")
			}
		}
	}()
	var addr string
	select {
	case addr = <-ready:
	case err = <-done:
		joined = true
		t.Fatal(err)
	case <-time.After(10 * time.Second):
		t.Fatal("hub startup timeout")
	}
	exerciseNativeNode(t, addr, key, t.TempDir())
	cancel()
	select {
	case err = <-done:
		joined = true
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(12 * time.Second):
		t.Fatal("node/Hub shutdown timeout")
	}
	r := opts.fixtureReport
	if !r.NodeReturned || !r.ProfileClosed || !r.RuntimeClosed || !r.NoActiveProcesses || !r.WorkspaceRemoved || r.ConfigReloads != 1 || r.ProviderCalls != 2 || !r.ProviderSawTool {
		t.Fatalf("native cleanup: %+v", r)
	}
	tools, repls := 0, 0
	for _, p := range r.Processes {
		if p.Name == "main-repl" {
			repls++
		} else if p.Command == fixtureCommand {
			tools++
			if p.State.Active() {
				t.Fatal("tool active")
			}
		}
	}
	if tools != 1 || repls != 1 {
		t.Fatalf("native resources tools=%d repls=%d", tools, repls)
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	listener.Close()
	assertNoStateFD(t, opts.stateDir)
	store, err := webservice.NewSQLiteStore(filepath.Join(opts.stateDir, "hub.sqlite"), webapitasks.SchemaModule())
	if err != nil {
		t.Fatal(err)
	}
	persisted, _, err := store.ListAOPEventsAfter(t.Context(), "h2b-session", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	verifyHubEvents(t, persisted, nil)
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	assertNoStateFD(t, opts.stateDir)
	captured, err := os.ReadFile(capture.Name())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(captured), key) {
		t.Fatal("private key in output")
	}
}

// 供本轮编译 CLI 的外部协议验收使用。无环境参数时跳过，不启动额外 Hub。
func TestFixtureNodeExternal(t *testing.T) {
	addr, keyPath, evidence := os.Getenv("WSCYBER_H2B_ADDRESS"), os.Getenv("WSCYBER_H2B_KEY_FILE"), os.Getenv("WSCYBER_H2B_EVIDENCE")
	if addr == "" {
		t.Skip("only for compiled local CLI acceptance")
	}
	if !fixtureAddressAllowed(addr) {
		t.Fatal("external acceptance requires literal loopback")
	}
	key, err := readPrivateKey(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	if evidence == "" {
		t.Fatal("evidence directory required")
	}
	exerciseNativeNode(t, addr, key, evidence)
}

// 只解析明确 IP，不做 DNS；外部客户端需要已经绑定的非零端口。
func fixtureAddressAllowed(address string) bool {
	parsed, err := netip.ParseAddrPort(address)
	return err == nil && parsed.Addr().IsLoopback() && parsed.Addr().Zone() == "" && parsed.Port() != 0
}

func TestFixtureExternalAddress(t *testing.T) {
	for address, want := range map[string]bool{
		"127.0.0.1:38767": true,
		"[::1]:38767":     true,
		"localhost:38767": false,
		"192.0.2.1:38767": false,
		"0.0.0.0:38767":   false,
		"[::]:38767":      false,
		"[::1%lo]:38767":  false,
		"127.0.0.1:0":     false,
	} {
		t.Run(address, func(t *testing.T) {
			if got := fixtureAddressAllowed(address); got != want {
				t.Fatalf("address allowed=%t, want %t", got, want)
			}
		})
	}
}

type wireProbe struct {
	t        *testing.T
	stream   *aopws.Stream
	sequence int
	watch    []*aop.Envelope
}

func (p *wireProbe) send(message *aop.ProtocolMessage) string {
	p.t.Helper()
	p.sequence++
	id := fmt.Sprintf("h2b-request-%d", p.sequence)
	envelope, err := aop.Wrap(id, "", message)
	if err != nil {
		p.t.Fatal(err)
	}
	if err = p.stream.Send(envelope); err != nil {
		p.t.Fatal(err)
	}
	return id
}
func (p *wireProbe) recv() (*aop.Envelope, *aop.ProtocolMessage) {
	p.t.Helper()
	envelope, err := p.stream.Recv()
	if err != nil {
		p.t.Fatal(err)
	}
	decoded, err := aop.Unwrap(envelope)
	if err != nil {
		p.t.Fatal(err)
	}
	message, ok := decoded.(*aop.ProtocolMessage)
	if !ok {
		p.t.Fatalf("unexpected namespace %T", decoded)
	}
	if message.GetProtocolError() != nil {
		p.t.Fatalf("native protocol error: %v", message.GetProtocolError())
	}
	if message.GetEvent() != nil {
		p.watch = append(p.watch, proto.CloneOf(envelope))
	}
	return envelope, message
}
func (p *wireProbe) call(message *aop.ProtocolMessage) *aop.ProtocolMessage {
	p.t.Helper()
	id := p.send(message)
	for {
		envelope, response := p.recv()
		if envelope.ReplyTo == id {
			return response
		}
	}
}
func (p *wireProbe) history() []*aop.EventDelivery {
	p.t.Helper()
	response := p.call(&aop.ProtocolMessage{Message: &aop.ProtocolMessage_ListEventsRequest{ListEventsRequest: &aop.ListEventsRequest{SessionId: "h2b-session", Limit: 100}}})
	if response.GetListEventsResponse() == nil {
		p.t.Fatal("no native history response")
	}
	return response.GetListEventsResponse().Events
}

func exerciseNativeNode(t *testing.T, addr, key, evidence string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	transport := &http.Transport{Proxy: nil}
	defer transport.CloseIdleConnections()
	httpClient := &http.Client{Transport: transport, Timeout: 3 * time.Second}
	endpoint := "http://" + addr
	dialer := websocket.Dialer{HandshakeTimeout: 3 * time.Second} // 不继承 HTTP_PROXY。
	for _, token := range []string{"", "wrong-private-key"} {
		headers := http.Header{}
		if token != "" {
			headers.Set("Authorization", "Bearer "+token)
		}
		c, response, err := dialer.DialContext(ctx, "ws://"+addr+webpkg.ApplicationWebSocketPath, headers)
		if c != nil {
			c.Close()
		}
		if response != nil {
			response.Body.Close()
		}
		if err == nil || response == nil || response.StatusCode != http.StatusUnauthorized {
			t.Fatal("AOP missing/wrong key was not rejected")
		}
	}
	agentClient := rpc.NewAgentServiceClient(httpClient, endpoint)
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	var agent *types.AgentView
	var lastStatus string
	for agent == nil {
		request := connect.NewRequest(&types.ListAgentsRequest{})
		request.Header().Set("Authorization", "Bearer "+key)
		response, err := agentClient.ListAgents(ctx, request)
		if err != nil {
			t.Fatal(err)
		}
		if len(response.Msg.Agents) > 1 {
			t.Fatal("more than one node")
		}
		for _, candidate := range response.Msg.Agents {
			lastStatus = strings.ReplaceAll(candidate.GetStatus().String(), key, "[redacted]")
			if candidate.GetHello().GetNodeId() == fixtureNodeID && candidate.GetStatus().GetModel() == fixtureModel && candidate.GetStatus().GetConfigError() == "" {
				agent = candidate
			}
		}
		if agent == nil {
			select {
			case <-ticker.C:
			case <-ctx.Done():
				t.Fatalf("native configuration handshake did not become ready; last status=%s", lastStatus)
			}
		}
	}
	conn, response, err := dialer.DialContext(ctx, "ws://"+addr+webpkg.ApplicationWebSocketPath, http.Header{"Authorization": {"Bearer " + key}})
	if response != nil {
		response.Body.Close()
	}
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetReadDeadline(time.Now().Add(15 * time.Second))
	stream, err := aopws.New(ctx, conn, aopws.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	p := &wireProbe{t: t, stream: stream}
	missing := p.call(&aop.ProtocolMessage{Message: &aop.ProtocolMessage_OpenSessionRequest{OpenSessionRequest: &aop.OpenSessionRequest{NodeId: "h2b-missing-node", SessionId: "not-executed"}}})
	if missing.GetOpenSessionResponse().GetRejected().GetCode() != "UNAVAILABLE" {
		t.Fatal("missing node not rejected")
	}
	opened := p.call(&aop.ProtocolMessage{Message: &aop.ProtocolMessage_OpenSessionRequest{OpenSessionRequest: &aop.OpenSessionRequest{NodeId: fixtureNodeID, SessionId: "h2b-session", Title: "本机 H2-B fixture"}}})
	if opened.GetOpenSessionResponse().GetAccepted().GetId() != "h2b-session" {
		t.Fatalf("native session rejected: %v", opened)
	}
	watchID := p.send(&aop.ProtocolMessage{Message: &aop.ProtocolMessage_WatchEventsRequest{WatchEventsRequest: &aop.WatchEventsRequest{SessionId: "h2b-session"}}})
	run := p.call(&aop.ProtocolMessage{Message: &aop.ProtocolMessage_RunTurnRequest{RunTurnRequest: &aop.RunTurnRequest{SessionId: "h2b-session", TurnId: "h2b-turn", Input: &aop.Message{Role: "user", Content: []*aop.Content{aop.Text("显式本机 fixture：仅固定 printf，无真实目标。")}}}}})
	if run.GetRunTurnResponse().GetAccepted().GetTurnId() != "h2b-turn" {
		t.Fatalf("native turn not accepted: %v", run)
	}
	ended := func() bool {
		for _, envelope := range p.watch {
			message, _ := aop.Unwrap(envelope)
			event := message.(*aop.ProtocolMessage).GetEvent()
			if event.GetTurnId() == "h2b-turn" && event.GetTurnEnded() != nil {
				return true
			}
		}
		return false
	}
	for !ended() {
		p.recv()
	}
	items := p.history()
	verifyHubEvents(t, items, p.watch)
	closed := p.call(&aop.ProtocolMessage{Message: &aop.ProtocolMessage_CloseSessionRequest{CloseSessionRequest: &aop.CloseSessionRequest{SessionId: "h2b-session", Reason: "completed"}}})
	if closed.GetCloseSessionResponse().GetAccepted().GetState() != "closed" {
		t.Fatalf("native session close failed: %v", closed)
	}
	for {
		items = p.history()
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
			t.Fatal("session terminal not persisted")
		}
	}
	p.send(&aop.ProtocolMessage{Message: &aop.ProtocolMessage_CancelOperation{CancelOperation: &aop.CancelOperation{TargetId: watchID}}})
	if err = stream.Close(); err != nil {
		t.Fatal(err)
	}
	writeProtoEvidence(t, filepath.Join(evidence, "hub-events.jsonl"), items)
	writeProtoEvidence(t, filepath.Join(evidence, "watch-envelopes.jsonl"), p.watch)
	writeProtoEvidence(t, filepath.Join(evidence, "agent.jsonl"), []*types.AgentView{agent})
	summary := map[string]any{"native_websocket": true, "authentication_rejected": 2, "missing_node_rejected": true, "node_id": fixtureNodeID, "session_id": "h2b-session", "turn_id": "h2b-turn", "call_id": fixtureCallID, "tool_output": fixtureMarker, "stop_reason": "completed", "session_closed": true, "durable_events": len(items), "watch_envelopes": len(p.watch), "watch_closed": true}
	data, _ := json.MarshalIndent(summary, "", "  ")
	if err = os.WriteFile(filepath.Join(evidence, "flow-summary.json"), append(data, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	files, _ := os.ReadDir(evidence)
	for _, file := range files {
		if file.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(evidence, file.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), key) {
			t.Fatal("key leaked into evidence")
		}
	}
}

func writeProtoEvidence[T proto.Message](t *testing.T, path string, items []T) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	for _, item := range items {
		data, err := protojson.Marshal(item)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = f.Write(append(data, '\n')); err != nil {
			t.Fatal(err)
		}
	}
}
func verifyHubEvents(t *testing.T, items []*aop.EventDelivery, live []*aop.Envelope) {
	t.Helper()
	calls, results, ended, started := 0, 0, 0, 0
	byID := map[string]*aop.Event{}
	for _, item := range items {
		event := item.GetEvent()
		if item.Cursor == "" || event.GetId() == "" || event.GetSessionId() != "h2b-session" {
			t.Fatal("missing native identity/cursor")
		}
		byID[event.Id] = event
		if event.TurnId != "h2b-turn" {
			continue
		}
		if event.GetTurnStarted() != nil {
			started++
		}
		if tool := event.GetToolCall(); tool != nil {
			calls++
			args, err := aop.DecodeJSON[map[string]any](tool.Arguments)
			if err != nil || tool.Id != fixtureCallID || tool.Name != "bash" || args["command"] != fixtureCommand {
				t.Fatal("wrong native tool call")
			}
		}
		if result := event.GetToolResult(); result != nil {
			results++
			if result.CallId != fixtureCallID || result.IsError || !strings.Contains(coretool.ResultText(result), fixtureMarker) {
				t.Fatal("wrong actual tool result")
			}
		}
		if terminal := event.GetTurnEnded(); terminal != nil {
			ended++
			if terminal.StopReason != "completed" {
				t.Fatalf("turn ended: %v", terminal)
			}
		}
	}
	if calls != 1 || results != 1 || ended != 1 || started != 1 {
		t.Fatalf("native chain counts %d/%d/%d/%d", calls, results, ended, started)
	}
	matched := 0
	for _, envelope := range live {
		message, _ := aop.Unwrap(envelope)
		event := message.(*aop.ProtocolMessage).GetEvent()
		if event.GetToolCall() != nil || event.GetToolResult() != nil || event.GetTurnEnded() != nil {
			if envelope.DeliveryCursor == "" || !proto.Equal(event, byID[event.Id]) {
				t.Fatal("live vs durable event mismatch")
			}
			matched++
		}
	}
	if live != nil && matched != 3 {
		t.Fatalf("live durable matches=%d", matched)
	}
}
