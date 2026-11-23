package main

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"time"

	"github.com/chainreactors/cyber/agent/provider"
	agentsession "github.com/chainreactors/cyber/agent/session"
	"github.com/chainreactors/cyber/aop"
	"github.com/chainreactors/cyber/core/extension"
	"github.com/chainreactors/cyber/core/namespaces"
	coreproc "github.com/chainreactors/cyber/core/proc"
	"github.com/chainreactors/cyber/core/telemetry"
	coretool "github.com/chainreactors/cyber/core/tool"
	"github.com/chainreactors/cyber/core/types"
	sessionext "github.com/chainreactors/cyber/exts/session"
	terminalext "github.com/chainreactors/cyber/exts/terminal"
	cfg "github.com/chainreactors/cyber/pkg/config"
	consoleapi "github.com/chainreactors/cyber/pkg/console/api"
	"github.com/chainreactors/cyber/pkg/harness"
	"github.com/chainreactors/cyber/pkg/node"
	"github.com/chainreactors/cyber/pkg/profile"
	"github.com/chainreactors/cyber/pkg/web/host"
	webservice "github.com/chainreactors/cyber/pkg/web/service"
	"github.com/chainreactors/utils/proc"
)

const (
	fixtureNodeID  = "wscyber-h2b-fixture-node"
	fixtureModel   = "wscyber-h2b-in-memory-only"
	fixtureMarker  = "WSCYBER_H2B_NATIVE_TOOL"
	fixtureCommand = "/usr/bin/printf 'WSCYBER_H2B_NATIVE_TOOL\\n'"
	fixtureCallID  = "h2b-bash-1"
)

// openai 仅满足上游配置 schema；绝不创建网络 Provider。
// 本地不可服务地址是额外防误配标记，不会启动或请求此端口。
func fixtureProviderConfig() provider.ProviderConfig {
	return provider.ProviderConfig{Provider: "openai", Model: fixtureModel, BaseURL: "http://127.0.0.1:1/h2b-no-http", APIKey: "fixture-not-a-credential", Timeout: 5}
}

// 原生 ConfigStore 扩展契约；只分发一个常量 fixture，不发现/写入任何配置文件。
type fixtureConfigStore struct{}

func (fixtureConfigStore) GetDistributeConfig(context.Context) (string, bool, *types.DistributeConfig, error) {
	c := fixtureProviderConfig()
	return "", false, &types.DistributeConfig{Llm: &types.LLMConfig{ActiveProfile: "h2b-fixture", Providers: []*types.LLMProviderConfig{{Id: "h2b-fixture", Name: "本机内存 fixture", Provider: c.Provider, Model: c.Model, BaseUrl: c.BaseURL, ApiKey: c.APIKey, Timeout: int32(c.Timeout)}}}}, nil
}
func (fixtureConfigStore) PrepareDistributeConfig(context.Context, *types.DistributeConfig) (*webservice.PreparedConfig, error) {
	return nil, errors.New("fixture 配置只读")
}
func (fixtureConfigStore) CommitDistributeConfig(context.Context, *webservice.PreparedConfig) error {
	return errors.New("fixture 配置只读")
}
func (fixtureConfigStore) DiscardDistributeConfig(*webservice.PreparedConfig) {}

// 只记录收尾后事实，非事件表、调度状态或第二个执行权威。
type fixtureNodeReport struct {
	NodeReturned      bool        `json:"node_returned"`
	ProfileClosed     bool        `json:"profile_closed"`
	RuntimeClosed     bool        `json:"runtime_closed"`
	NoActiveProcesses bool        `json:"no_active_processes"`
	WorkspaceRemoved  bool        `json:"workspace_removed"`
	ConfigReloads     int32       `json:"config_reloads"`
	ProviderCalls     int32       `json:"provider_calls"`
	ProviderSawTool   bool        `json:"provider_saw_tool"`
	Processes         []proc.Info `json:"native_processes"`
}

// 只补 Profile 呈现/协议契约；运行、资源、会话和协议处理全部归上游。
type fixtureProfile struct {
	*harness.Harness
	protocols  *extension.Set
	namespaces *namespaces.Registry
	runtime    *agentsession.Runtime
	processes  *coreproc.Manager
	provider   *nodeFixtureProvider
	reloads    atomic.Int32
	report     *fixtureNodeReport
	closeErr   error
}

var _ profile.Profile = (*fixtureProfile)(nil)

func newFixtureProfile(request profile.Request, directory string, report *fixtureNodeReport) (*fixtureProfile, error) {
	if request.ProviderMode != provider.StartupDisabled || request.Session == nil {
		return nil, errors.New("fixture 必须以禁用 Provider 的原生节点注册")
	}
	session := *request.Session // 保留 node 传入的 StandardLoop。
	session.NodeName, session.SkipBaseSkills, session.Logger = fixtureNodeID, true, telemetry.NopLogger()
	h, err := harness.New(harness.Config{Base: harness.BaseConfig{
		Directory: directory, SkillExclude: []string{"cyber"}, Logger: telemetry.NopLogger(),
		Provider: provider.StartupConfig{Mode: provider.StartupDisabled}, Egress: harness.NoEgress(), // fixture 节点不装 webapi 能力，无任务/策略可归属；不发布路由（不代表网络隔离）。
		Terminal: terminalext.Config{Timeout: 5, MaximumTimeout: 5 * time.Second},
	}, Session: &session})
	if err != nil {
		return nil, err
	}
	return &fixtureProfile{Harness: h, provider: &nodeFixtureProvider{}, report: report}, nil
}
func (p *fixtureProfile) Load(ctx context.Context) error {
	if err := p.Harness.Load(ctx); err != nil {
		return err
	}
	var err error
	if p.runtime, err = p.Runtime(); err != nil {
		return err
	}
	if p.processes, err = p.Processes(); err != nil {
		return err
	}
	p.namespaces = namespaces.New()
	p.protocols, err = extension.New(p.namespaces, extension.Provided(p.runtime), sessionext.NewProtocol())
	if err != nil {
		return err
	}
	return p.protocols.Load(ctx)
}
func (p *fixtureProfile) RegisterNamespaces(mux *aop.NamespaceMux) error {
	return p.namespaces.Bind(mux)
}
func (p *fixtureProfile) ConsoleBindings() *consoleapi.Bindings { return nil }
func (p *fixtureProfile) AgentStatus() *aop.AgentStatus {
	state, _ := p.Providers()
	return node.AgentStatus(state)
}
func (p *fixtureProfile) ReloadProvider(ctx context.Context, config provider.ProviderConfig) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !reflect.DeepEqual(config, fixtureProviderConfig()) {
		return errors.New("拒绝非固定 fixture Provider 配置")
	}
	state, err := p.Providers()
	if err != nil {
		return err
	}
	// 只有原生 reload handler 调用此 hook；不更改 Hub 的 ready 标志。
	state.Set(p.provider, config)
	p.reloads.Add(1)
	return nil
}
func (p *fixtureProfile) Close(ctx context.Context) error {
	if p.protocols != nil {
		p.closeErr = errors.Join(p.closeErr, p.protocols.Close(ctx))
	}
	p.closeErr = errors.Join(p.closeErr, p.Harness.Close(ctx))
	r := p.report
	r.ProfileClosed = !p.Active() && p.closeErr == nil
	r.RuntimeClosed = p.runtime != nil && !p.runtime.Active()
	r.ConfigReloads = p.reloads.Load()
	r.ProviderCalls, r.ProviderSawTool = p.provider.calls.Load(), p.provider.sawTool.Load()
	r.NoActiveProcesses = p.processes != nil
	if p.processes != nil {
		r.Processes = p.processes.List()
		for _, entry := range r.Processes {
			if entry.State.Active() {
				r.NoActiveProcesses = false
			}
		}
	}
	if !r.NoActiveProcesses {
		p.closeErr = errors.Join(p.closeErr, errors.New("原生资源未完全停止"))
	}
	return p.closeErr
}

func serveFixtureNode(ctx context.Context, config host.Config, stateDir, key string, report *fixtureNodeReport) (resultErr error) {
	if report == nil {
		report = &fixtureNodeReport{}
	}
	directory, err := os.MkdirTemp(stateDir, "fixture-node-")
	if err != nil {
		return err
	}
	defer func() {
		err := os.RemoveAll(directory)
		report.WorkspaceRemoved = err == nil
		resultErr = errors.Join(resultErr, err)
	}()
	config.Management.ConfigStore = fixtureConfigStore{}
	var p *fixtureProfile
	var nodeErr error
	config.StartNode = func(nodeCtx context.Context, addr string) error {
		defer func() { report.NodeReturned = true }()
		// 上游 agent Option 没有独立 token 字段；仅在内存编码，原生 DialURL
		// 立即剥离 userinfo 转 Authorization header。禁止输出 URL/原始错误。
		endpoint := (&url.URL{Scheme: "http", Host: addr, User: url.User(key)}).String()
		option := &cfg.Option{
			Explicit:     map[string]bool{},
			Context:      &cfg.Context{Directory: directory, Home: directory, Executable: filepath.Join(directory, "fixture-node"), LookupEnv: func(string) (string, bool) { return "", false }},
			NodeOptions:  cfg.NodeOptions{NodeID: fixtureNodeID, NodeName: fixtureNodeID},
			MiscOptions:  cfg.MiscOptions{DataDir: directory},
			AgentOptions: cfg.AgentOptions{ServerURL: endpoint, Timeout: 3600}, // 与原生 ApplyDefaults 一致，避免首个 LLM 配置误判为图变更。
		}
		err := node.RunWebSocket(nodeCtx, func(request profile.Request) (profile.Profile, error) {
			if p != nil {
				return nil, errors.New("fixture 不接受额外 Profile 重建")
			}
			var err error
			p, err = newFixtureProfile(request, directory, report)
			return p, err
		}, option, telemetry.NopLogger())
		if err != nil && !errors.Is(err, context.Canceled) {
			nodeErr = errors.New("fixture 原生节点退出失败；原始错误未输出以保护认证信息")
		}
		return nodeErr
	}
	// host 取消并等待 StartNode 后才关闭 Web 扩展/原生存储连接池。
	resultErr = host.Serve(ctx, config)
	if p != nil {
		resultErr = errors.Join(resultErr, p.closeErr)
	}
	return errors.Join(resultErr, nodeErr)
}

// 确定性模型边界；不运行工具、不调用 exec、不读用户动态命令。
// 一个进程只允许一次固定调用，之后必须收到原生工具的实际结果。
type nodeFixtureProvider struct {
	calls   atomic.Int32
	sawTool atomic.Bool
}

func (*nodeFixtureProvider) Name() string { return "wscyber-h2b-fixture" }
func (p *nodeFixtureProvider) ChatCompletion(ctx context.Context, req *provider.ChatCompletionRequest) (*provider.ChatCompletionResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	switch p.calls.Add(1) {
	case 1:
		found := false
		for _, tool := range req.Tools {
			if tool.Name == "bash" {
				found = true
			}
		}
		if !found {
			return nil, errors.New("原生 bash 未发布")
		}
		args, err := aop.JSONValue(map[string]any{"command": fixtureCommand, "timeout": 5})
		if err != nil {
			return nil, err
		}
		return &provider.ChatCompletionResponse{Choices: []provider.Choice{{Message: &aop.Message{Role: "assistant", Content: []*aop.Content{{Value: &aop.Content_ToolCall{ToolCall: &aop.ToolCall{Id: fixtureCallID, Name: "bash", Arguments: args}}}}}, FinishReason: "tool_calls"}}}, nil
	case 2:
		for _, message := range req.Messages {
			result := provider.MessageToolResult(message)
			if result != nil && result.CallId == fixtureCallID && !result.IsError && strings.Contains(coretool.ResultText(result), fixtureMarker) {
				p.sawTool.Store(true)
			}
		}
		if !p.sawTool.Load() {
			return nil, errors.New("未收到原生工具结果")
		}
		return &provider.ChatCompletionResponse{Choices: []provider.Choice{{Message: provider.TextMessage("assistant", "H2-B fixture：已收到原生 bash 结果"), FinishReason: "stop"}}}, nil
	default:
		return nil, fmt.Errorf("fixture 仅允许一个工具回合，不接受额外模型请求")
	}
}
