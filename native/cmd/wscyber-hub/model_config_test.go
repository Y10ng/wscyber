package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/chainreactors/cyber/core/types"
	"github.com/chainreactors/cyber/pkg/rpc"
	webservice "github.com/chainreactors/cyber/pkg/web/service"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
)

func TestModelConfigModeBoundaries(t *testing.T) {
	for _, name := range []string{"requires-local-hub", "exclusive-with-fixture", "existing-state"} {
		t.Run(name, func(t *testing.T) {
			opts, _ := testOptions(t)
			opts.modelConfig = true
			switch name {
			case "requires-local-hub":
				opts.localHub = false
			case "exclusive-with-fixture":
				opts.fixtureNode = true
			case "existing-state":
				if err := os.Mkdir(opts.stateDir, 0700); err != nil {
					t.Fatal(err)
				}
				mustWrite(t, filepath.Join(opts.stateDir, "keep"), []byte("unchanged"))
			}
			called := false
			if err := serveLocalHub(t.Context(), opts, func(string) { called = true }); err == nil || called {
				t.Fatal("invalid model-config mode reached the listener")
			}
			if name == "existing-state" {
				if string(modelReadFile(t, filepath.Join(opts.stateDir, "keep"))) != "unchanged" {
					t.Fatal("existing state was modified")
				}
			} else if _, err := os.Stat(opts.stateDir); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("rejected mode created state")
			}
		})
	}
	var output bytes.Buffer
	if err := run(t.Context(), []string{"--model-config", "--fixture-node", "--local-hub"}, &output); err == nil || output.Len() != 0 {
		t.Fatal("CLI mode conflict was not rejected before ready")
	}
	if err := run(t.Context(), []string{"--help"}, &output); err != nil || !strings.Contains(output.String(), "model-config") {
		t.Fatal("CLI model-config flag is missing")
	}
}

func modelTestConfig(endpoint, key string) *types.DistributeConfig {
	return &types.DistributeConfig{Llm: &types.LLMConfig{
		ActiveProfile: "local-a",
		Providers: []*types.LLMProviderConfig{
			{Id: "local-a", Name: "本机配置A", Provider: "openai", BaseUrl: endpoint, ApiKey: key, Model: "h4a-config-a", Timeout: 3},
			{Id: "local-b", Name: "本机配置B", Provider: "openai", BaseUrl: endpoint, ApiKey: key, Model: "h4a-config-b", Timeout: 3},
		},
	}}
}

func modelReadFile(t *testing.T, path string) []byte {
	t.Helper()
	value, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func modelAssertPrivate(t *testing.T, path string, mode os.FileMode) {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil || info.Mode().Perm() != mode || info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("private path check failed for %s", filepath.Base(path))
	}
	if mode == 0700 && !info.IsDir() || mode == 0600 && !info.Mode().IsRegular() {
		t.Fatal("private path has unexpected type")
	}
}

func TestModelConfigStoreTransactions(t *testing.T) {
	root := t.TempDir()
	// 只在本用例目录放宿主/项目发现诱饵；不触碰真实HOME或开发配置。
	foreign := filepath.Join(root, "foreign")
	if err := os.MkdirAll(filepath.Join(foreign, ".cyber"), 0700); err != nil {
		t.Fatal(err)
	}
	const hostKey = "h4a-host-key-must-not-be-loaded"
	bait := []byte("llm:\n  provider: openai\n  model: host-model\n  api_key: " + hostKey + "\n")
	for _, path := range []string{filepath.Join(foreign, "cyber.yaml"), filepath.Join(foreign, ".cyber", "cyber.yaml")} {
		mustWrite(t, path, bait)
	}
	t.Setenv("HOME", foreign)
	t.Setenv("CYBER_API_KEY", hostKey)
	t.Setenv("CYBER_MODEL", "host-model")
	t.Chdir(foreign)
	stateDir := filepath.Join(root, "state")
	if err := os.Mkdir(stateDir, 0700); err != nil {
		t.Fatal(err)
	}
	store := newModelConfigStore(stateDir)
	path, loaded, value, err := store.GetDistributeConfig(t.Context())
	if err != nil || loaded || len(value.GetLlm().GetProviders()) != 0 || store.native.RuntimeLLM().APIKey != "" {
		t.Fatal("new explicit store discovered host configuration")
	}
	if path != filepath.Join(stateDir, "model-config", "cyber.yaml") {
		t.Fatal("unexpected configuration authority")
	}
	const key = "h4a-private-test-key-not-a-credential"
	input := modelTestConfig("http://127.0.0.1:1/h4a-not-running/v1", key)
	input.Extensions = map[string]*structpb.Struct{}
	input.Agent, input.Traffic, input.Node = &types.AgentConfig{}, &types.TrafficConfig{}, &types.NodeConfig{}
	prepared, err := store.PrepareDistributeConfig(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.DiscardDistributeConfig(prepared) })
	modelAssertPrivate(t, prepared.RuntimePath, 0600)
	modelAssertPrivate(t, filepath.Dir(path), 0700)
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) || store.native.RuntimeLLM().APIKey != "" {
		t.Fatal("prepare published or committed configuration")
	}
	if input.Agent == nil || input.Traffic == nil || input.Node == nil {
		t.Fatal("adapter modified caller's non-LLM messages")
	}
	if err := store.CommitDistributeConfig(t.Context(), prepared); err != nil {
		t.Fatal(err)
	}
	modelAssertPrivate(t, path, 0600)
	if prepared.RuntimePath != "" || store.native.RuntimeLLM().APIKey != key {
		t.Fatal("native commit did not publish runtime snapshot")
	}
	before := modelReadFile(t, path)
	if bytes.Contains(before, []byte(hostKey)) || bytes.Contains(before, []byte("agent:")) || bytes.Contains(before, []byte("traffic:")) || bytes.Contains(before, []byte("node:")) {
		t.Fatal("unexpected configuration source or non-LLM section persisted")
	}

	input = modelTestConfig("http://127.0.0.1:1/h4a-not-running/v1", "")
	input.Llm.Providers[0].Model = "h4a-edited"
	staged, err := store.PrepareDistributeConfig(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	stagedPath := staged.RuntimePath
	store.DiscardDistributeConfig(staged)
	if _, err := os.Stat(stagedPath); !errors.Is(err, os.ErrNotExist) || staged.RuntimePath != "" || !bytes.Equal(before, modelReadFile(t, path)) {
		t.Fatal("discard changed the committed file or retained the staged file")
	}
	if store.native.RuntimeLLM().Model != "h4a-config-a" {
		t.Fatal("discard changed runtime")
	}

	staged, err = store.PrepareDistributeConfig(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.CommitDistributeConfig(t.Context(), staged); err != nil {
		store.DiscardDistributeConfig(staged)
		t.Fatal(err)
	}
	_, loaded, value, err = newModelConfigStore(stateDir).GetDistributeConfig(t.Context())
	if err != nil || !loaded || len(value.GetLlm().GetProviders()) != 2 || value.GetLlm().GetProviders()[0].GetModel() != "h4a-edited" {
		t.Fatal("native file reread failed")
	}
	for _, p := range value.Llm.Providers {
		if p.ApiKey != key {
			t.Fatal("blank API key did not preserve the same profile's key")
		}
	}
	before = modelReadFile(t, path)

	t.Run("canceled-commit", func(t *testing.T) {
		candidate, err := store.PrepareDistributeConfig(t.Context(), input)
		if err != nil {
			t.Fatal(err)
		}
		defer store.DiscardDistributeConfig(candidate)
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		if err := store.CommitDistributeConfig(ctx, candidate); !errors.Is(err, context.Canceled) {
			t.Fatal("canceled commit was accepted")
		}
	})
	t.Run("missing-staged-file", func(t *testing.T) {
		candidate, err := store.PrepareDistributeConfig(t.Context(), input)
		if err != nil {
			t.Fatal(err)
		}
		defer store.DiscardDistributeConfig(candidate)
		modelAssertPrivate(t, candidate.RuntimePath, 0600)
		if err := os.Remove(candidate.RuntimePath); err != nil {
			t.Fatal(err)
		}
		if err := store.CommitDistributeConfig(t.Context(), candidate); err == nil || strings.Contains(err.Error(), key) {
			t.Fatal("commit failure was accepted or exposed a key")
		}
	})
	for name, mutate := range modelNonLLMChanges() {
		t.Run(name, func(t *testing.T) {
			rejected := proto.CloneOf(input)
			mutate(rejected)
			if candidate, err := store.PrepareDistributeConfig(t.Context(), rejected); err == nil || candidate != nil {
				store.DiscardDistributeConfig(candidate)
				t.Fatal("non-LLM change was accepted")
			}
		})
	}
	invalid := proto.CloneOf(input)
	invalid.Llm.Providers[0].Model = ""
	if candidate, err := store.PrepareDistributeConfig(t.Context(), invalid); err == nil || candidate != nil {
		store.DiscardDistributeConfig(candidate)
		t.Fatal("invalid native model configuration accepted")
	}
	if !bytes.Equal(before, modelReadFile(t, path)) || store.native.RuntimeLLM().Model != "h4a-edited" {
		t.Fatal("failed transaction damaged committed configuration")
	}
	matches, err := filepath.Glob(filepath.Join(filepath.Dir(path), ".cyber.yaml.tmp-*.yaml"))
	if err != nil || len(matches) != 0 {
		t.Fatal("staged config leaked")
	}
	// 原生YAML错误可能引用原文；适配层只返回固定、不含秘密的错误。
	badState := filepath.Join(root, "bad-state")
	if err := os.MkdirAll(filepath.Join(badState, "model-config"), 0700); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(badState, "model-config", "cyber.yaml"), []byte("llm: ["+key))
	if _, _, _, err := newModelConfigStore(badState).GetDistributeConfig(t.Context()); err == nil || strings.Contains(err.Error(), key) {
		t.Fatal("invalid file was accepted or exposed its contents")
	}
}

func modelNonLLMChanges() map[string]func(*types.DistributeConfig) {
	return map[string]func(*types.DistributeConfig){
		"agent":     func(c *types.DistributeConfig) { c.Agent = &types.AgentConfig{Timeout: proto.Int32(0)} },
		"traffic":   func(c *types.DistributeConfig) { c.Traffic = &types.TrafficConfig{BodyStorage: "disk"} },
		"node":      func(c *types.DistributeConfig) { c.Node = &types.NodeConfig{Name: "another-node"} },
		"extension": func(c *types.DistributeConfig) { c.Extensions = map[string]*structpb.Struct{"unavailable": {}} },
	}
}

func modelAuthRequest[T any](message *T, key string) *connect.Request[T] {
	req := connect.NewRequest(message)
	if key != "" {
		req.Header().Set("Authorization", "Bearer "+key)
	}
	return req
}

func TestModelConfigNativeRPC(t *testing.T) {
	opts, key := testOptions(t)
	opts.modelConfig = true
	const modelKey = "h4a-model-test-key-never-return-or-log"
	// 只作零请求哨兵，不实现/返回模型结果；任何命中都使测试失败。
	var outbound atomic.Int32
	canary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		outbound.Add(1)
		w.WriteHeader(http.StatusTeapot)
	}))
	defer canary.Close()
	capture, err := os.CreateTemp(t.TempDir(), "output-")
	if err != nil {
		t.Fatal(err)
	}
	oldOut, oldErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = capture, capture
	t.Cleanup(func() {
		os.Stdout, os.Stderr = oldOut, oldErr
		_ = capture.Close()
		output := modelReadFile(t, capture.Name())
		if bytes.Contains(output, []byte(key)) || bytes.Contains(output, []byte(modelKey)) {
			t.Error("private key appeared in host output")
		}
	})
	ctx, cancel := context.WithCancel(t.Context())
	ready, done := make(chan string, 1), make(chan error, 1)
	go func() { done <- serveLocalHub(ctx, opts, func(addr string) { ready <- addr }) }()
	joined := false
	t.Cleanup(func() {
		cancel()
		if !joined {
			select {
			case err := <-done:
				if err != nil {
					t.Error(err)
				}
			case <-time.After(12 * time.Second):
				t.Error("model-config Hub did not close")
			}
		}
	})
	var addr string
	select {
	case addr = <-ready:
	case err := <-done:
		joined = true
		t.Fatal(err)
	case <-time.After(10 * time.Second):
		t.Fatal("model-config Hub did not start")
	}
	client := &http.Client{Transport: &http.Transport{Proxy: nil}, Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	defer client.CloseIdleConnections()
	base := "http://" + addr
	binary := rpc.NewConfigServiceClient(client, base)
	jsonClient := rpc.NewConfigServiceClient(client, base, connect.WithProtoJSON())
	for _, token := range []string{"", "wrong-key"} {
		if _, err := binary.GetConfig(t.Context(), modelAuthRequest(&types.GetConfigRequest{}, token)); connect.CodeOf(err) != connect.CodeUnauthenticated {
			t.Fatal("native config read authentication was bypassed")
		}
		if _, err := jsonClient.UpdateConfig(t.Context(), modelAuthRequest(&types.UpdateConfigRequest{Config: modelTestConfig(canary.URL+"/v1", modelKey)}, token)); connect.CodeOf(err) != connect.CodeUnauthenticated {
			t.Fatal("native config write authentication was bypassed")
		}
	}
	initial, err := binary.GetConfig(t.Context(), modelAuthRequest(&types.GetConfigRequest{}, key))
	if err != nil || initial.Msg.GetConfig().GetLoaded() || len(initial.Msg.GetConfig().GetLlm().GetProviders()) != 0 {
		t.Fatal("new store was not empty")
	}
	config := modelTestConfig(canary.URL+"/v1", modelKey)
	saved, err := jsonClient.UpdateConfig(t.Context(), modelAuthRequest(&types.UpdateConfigRequest{Config: config}, key))
	if err != nil {
		t.Fatal(err)
	}
	modelAssertView(t, saved.Msg.GetConfig(), modelKey, "local-a", "h4a-config-a")
	path := filepath.Join(opts.stateDir, "model-config", "cyber.yaml")
	modelAssertPrivate(t, opts.stateDir, 0700)
	modelAssertPrivate(t, filepath.Dir(path), 0700)
	modelAssertPrivate(t, path, 0600)
	if !bytes.Contains(modelReadFile(t, path), []byte(modelKey)) {
		t.Fatal("secret was not saved to the native private file")
	}

	// ConfigPanel实际形状：编辑其他字段时apiKey留空、空extensions、agent省略；
	// 同时覆盖traffic:null/{}无实际变更，不能误拒原生页面的模型保存。
	for _, traffic := range []any{nil, map[string]any{}} {
		body, err := json.Marshal(map[string]any{"config": map[string]any{
			"llm": map[string]any{"activeProfile": "local-a", "providers": []any{
				map[string]any{"id": "local-a", "name": "本机配置A", "provider": "openai", "baseUrl": canary.URL + "/v1", "apiKey": "", "model": "h4a-edited", "proxy": "", "timeout": 3, "maxTokens": 0, "contextWindow": 0},
				map[string]any{"id": "local-b", "name": "本机配置B", "provider": "openai", "baseUrl": canary.URL + "/v1", "apiKey": "", "model": "h4a-config-b", "proxy": "", "timeout": 3, "maxTokens": 0, "contextWindow": 0},
			}}, "extensions": map[string]any{}, "traffic": traffic,
		}})
		if err != nil {
			t.Fatal(err)
		}
		status, response := modelRawRPC(t, client, base+rpc.ConfigServiceUpdateConfigProcedure, key, body)
		if status != http.StatusOK {
			t.Fatalf("native ConfigPanel shape was rejected: status=%d", status)
		}
		var update types.UpdateConfigResponse
		if err := protojson.Unmarshal(response, &update); err != nil {
			t.Fatal(err)
		}
		modelAssertView(t, update.Config, modelKey, "local-a", "h4a-edited")
	}
	activated, err := binary.ActivateProfile(t.Context(), modelAuthRequest(&types.ActivateProfileRequest{ProfileId: "local-b"}, key))
	if err != nil {
		t.Fatal(err)
	}
	modelAssertView(t, activated.Msg.GetConfig(), modelKey, "local-b", "h4a-config-b")
	before := modelReadFile(t, path)
	if _, err := binary.ActivateProfile(t.Context(), modelAuthRequest(&types.ActivateProfileRequest{ProfileId: "missing"}, key)); connect.CodeOf(err) != connect.CodeNotFound {
		t.Fatal("missing profile activation was accepted")
	}
	for name, mutate := range modelNonLLMChanges() {
		t.Run(name, func(t *testing.T) {
			invalid := proto.CloneOf(config)
			mutate(invalid)
			if _, err := binary.UpdateConfig(t.Context(), modelAuthRequest(&types.UpdateConfigRequest{Config: invalid}, key)); connect.CodeOf(err) != connect.CodeInvalidArgument {
				t.Fatal("non-LLM RPC change was accepted")
			}
		})
	}
	invalid := proto.CloneOf(config)
	invalid.Llm.Providers[0].Model = ""
	if _, err := jsonClient.UpdateConfig(t.Context(), modelAuthRequest(&types.UpdateConfigRequest{Config: invalid}, key)); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatal("invalid native LLM config was accepted")
	}
	if !bytes.Equal(before, modelReadFile(t, path)) {
		t.Fatal("failed RPC changed native configuration")
	}
	read, err := binary.GetConfig(t.Context(), modelAuthRequest(&types.GetConfigRequest{}, key))
	if err != nil {
		t.Fatal(err)
	}
	modelAssertView(t, read.Msg.GetConfig(), modelKey, "local-b", "h4a-config-b")
	status, err := rpc.NewSystemServiceClient(client, base).GetStatus(t.Context(), modelAuthRequest(&types.GetStatusRequest{}, key))
	if err != nil || status.Msg.GetStatus().GetLlmAvailable() || !status.Msg.GetStatus().GetLlmApiKeyConfigured() || status.Msg.GetStatus().GetLlmModel() != "h4a-config-b" {
		t.Fatal("configuration-only mode claimed provider availability or lost active config")
	}
	agents, err := rpc.NewAgentServiceClient(client, base).ListAgents(t.Context(), modelAuthRequest(&types.ListAgentsRequest{}, key))
	if err != nil || len(agents.Msg.GetAgents()) != 0 {
		t.Fatal("configuration-only mode created an execution node")
	}
	modelCheckProbeDenials(t, client, base, key, modelKey, canary.URL, binary, jsonClient)
	if outbound.Load() != 0 {
		t.Fatal("configuration save/activation/probe rejection sent a network request")
	}
	cancel()
	select {
	case err := <-done:
		joined = true
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(12 * time.Second):
		t.Fatal("configuration-only Hub shutdown timed out")
	}
	assertNoStateFD(t, opts.stateDir)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatal("Hub listener not released")
	}
	_ = listener.Close()
	_, loaded, stored, err := newModelConfigStore(opts.stateDir).GetDistributeConfig(t.Context())
	if err != nil || !loaded || stored.GetLlm().GetActiveProfile() != "local-b" {
		t.Fatal("committed configuration missing after Hub shutdown")
	}
	for _, p := range stored.Llm.Providers {
		if p.ApiKey != modelKey {
			t.Fatal("RPC blank key did not retain saved secret")
		}
	}
}

func modelAssertView(t *testing.T, view *types.ConfigView, secret, active, model string) {
	t.Helper()
	if !view.GetLoaded() || view.GetLlm().GetActiveProfile() != active || view.GetLlm().GetActive().GetModel() != model || !view.GetLlm().GetActive().GetApiKeyConfigured() {
		t.Fatal("native ConfigView metadata mismatch")
	}
	raw, err := protojson.Marshal(view)
	if err != nil || bytes.Contains(raw, []byte(secret)) {
		t.Fatal("native ConfigView exposed a secret")
	}
}

func modelRawRPC(t *testing.T, client *http.Client, endpoint, key string, body []byte) (int, []byte) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Connect-Protocol-Version", "1")
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		t.Fatal(err)
	}
	return res.StatusCode, raw
}

func modelCheckProbeDenials(t *testing.T, client *http.Client, base, key, modelKey, endpoint string, clients ...rpc.ConfigServiceClient) {
	t.Helper()
	for i, cfgClient := range clients {
		for _, token := range []string{"", "wrong-key", key} {
			want := connect.CodeUnauthenticated
			if token == key {
				want = connect.CodeFailedPrecondition
			}
			for _, request := range []*types.LLMProbeRequest{
				{},
				{ProfileId: "local-b", Provider: "openai", BaseUrl: endpoint + "/independent/v1", ApiKey: modelKey, Proxy: endpoint, Model: "h4a-independent"},
				{ProfileId: "local-b", Provider: "openai", BaseUrl: endpoint + "/borrow-stored-key/v1", Model: "h4a-independent"},
			} {
				if _, err := cfgClient.TestLLM(t.Context(), modelAuthRequest(request, token)); connect.CodeOf(err) != want || strings.Contains(err.Error(), modelKey) {
					t.Fatalf("TestLLM denial codec=%d: unexpected code or exposed secret", i)
				}
				if _, err := cfgClient.ListModels(t.Context(), modelAuthRequest(request, token)); connect.CodeOf(err) != want || strings.Contains(err.Error(), modelKey) {
					t.Fatalf("ListModels denial codec=%d: unexpected code or exposed secret", i)
				}
			}
			fields, err := structpb.NewStruct(map[string]any{"url": endpoint, "key": modelKey, "proxy": endpoint})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := cfgClient.TestConnection(t.Context(), modelAuthRequest(&types.TestConnectionRequest{Section: "cyberhub", Config: &types.DistributeConfig{Extensions: map[string]*structpb.Struct{"cyberhub": fields}}}, token)); connect.CodeOf(err) != want || strings.Contains(err.Error(), modelKey) {
				t.Fatalf("TestConnection denial codec=%d: unexpected code or exposed secret", i)
			}
		}
		// 与原生RPC一致：自然会话Cookie可认证，错误显式Bearer不能回退Cookie。
		cookieRequest := connect.NewRequest(&types.LLMProbeRequest{})
		cookieRequest.Header().Set("Cookie", webservice.CookieName+"="+webservice.SessionValue(key))
		if _, err := cfgClient.TestLLM(t.Context(), cookieRequest); connect.CodeOf(err) != connect.CodeFailedPrecondition {
			t.Fatal("native session cookie not accepted by probe denial")
		}
		cookieRequest.Header().Set("Authorization", "Bearer wrong-key")
		if _, err := cfgClient.TestLLM(t.Context(), cookieRequest); connect.CodeOf(err) != connect.CodeUnauthenticated {
			t.Fatal("wrong explicit bearer fell back to cookie")
		}
	}
	for _, procedure := range []string{rpc.ConfigServiceTestLLMProcedure, rpc.ConfigServiceListModelsProcedure, rpc.ConfigServiceTestConnectionProcedure} {
		for _, body := range [][]byte{nil, []byte("{")} {
			status, raw := modelRawRPC(t, client, base+procedure, key, body)
			if status < 400 || status >= 500 || bytes.Contains(raw, []byte(modelKey)) {
				t.Fatal("missing/malformed probe body did not fail safely")
			}
		}
	}
}
