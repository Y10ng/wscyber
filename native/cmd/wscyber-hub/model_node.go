package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/chainreactors/cyber/agent/provider"
	agentsession "github.com/chainreactors/cyber/agent/session"
	"github.com/chainreactors/cyber/aop"
	"github.com/chainreactors/cyber/core/extension"
	"github.com/chainreactors/cyber/core/hooks"
	"github.com/chainreactors/cyber/core/namespaces"
	coreproc "github.com/chainreactors/cyber/core/proc"
	"github.com/chainreactors/cyber/core/telemetry"
	toolhooks "github.com/chainreactors/cyber/core/tool/hooks"
	"github.com/chainreactors/cyber/core/types"
	terminalext "github.com/chainreactors/cyber/exts/terminal"
	cfg "github.com/chainreactors/cyber/pkg/config"
	consoleapi "github.com/chainreactors/cyber/pkg/console/api"
	"github.com/chainreactors/cyber/pkg/harness"
	"github.com/chainreactors/cyber/pkg/node"
	"github.com/chainreactors/cyber/pkg/profile"
	webapi "github.com/chainreactors/cyber/pkg/web/api"
	"github.com/chainreactors/cyber/pkg/web/host"
	webservice "github.com/chainreactors/cyber/pkg/web/service"
	"google.golang.org/protobuf/proto"

	"wscyber/native/modeltransport"
	businesswebapi "wscyber/native/webapi"
)

const (
	modelNodeID         = "wscyber-local-model-node"
	modelNodeMaxClients = 4 // 本片保留旧turn快照的资源上限，不是产品配置版本/计费预算。
	modelNodeMaxTurns   = 3 // 原生循环先判预算再判自然完成；为两次模型交互留完成余量，仍有界。
)

var (
	errModelNodeConfig = errors.New("本机节点仅接受独立允许的 OpenAI 端点、非空模型/key及空用户代理")
	errModelNodeClosed = errors.New("本机模型节点正在关闭，不接受配置应用")
	errModelNodeLimit  = errors.New("本机节点已保留4份模型实例；新配置已保存但未应用，旧实例不变")
)

func validateModelNodeOptions(opts options) error {
	if !opts.modelNode {
		if opts.modelEndpoint != "" {
			return errors.New("--model-endpoint 仅用于 --model-node")
		}
		return nil
	}
	if opts.modelConfig || opts.fixtureNode {
		return errors.New("--model-node 与其他节点/配置模式互斥")
	}
	if opts.modelEndpoint != "" && !modelEndpointAllowed(opts.modelEndpoint) {
		return errors.New("显式 --model-endpoint 必须是规范的本机受控验收端点")
	}
	return nil
}

// 这里只验证CLI允许值；逐连接约束仍全部归已验 modeltransport。
func modelEndpointAllowed(endpoint string) bool {
	u, err := url.Parse(endpoint)
	if err != nil {
		return false
	}
	address, err := netip.ParseAddrPort(u.Host)
	return err == nil && address.Addr() == netip.AddrFrom4([4]byte{127, 0, 0, 1}) && address.Port() != 0 &&
		endpoint == "http://"+address.String()+"/v1"
}

// 复用H4-A存储，只在原生秘密保留之后追加此模式约束。
// Commit/Discard、文件格式/锁/原子写入和key保留仍归FileConfigStore。
type modelNodeConfigStore struct {
	*modelConfigStore
	endpoint string
}

var _ webservice.ConfigStore = (*modelNodeConfigStore)(nil)

func newModelNodeConfigStore(directory, endpoint string) *modelNodeConfigStore {
	store := &modelNodeConfigStore{modelConfigStore: newModelConfigStore(directory), endpoint: endpoint}
	store.native.Codec.Validate = store.validate
	return store
}

func (s *modelNodeConfigStore) validate(value *types.DistributeConfig) error {
	if err := validateModelConfig(value); err != nil {
		return err
	}
	for _, entry := range value.GetLlm().GetProviders() {
		if entry.GetProvider() != provider.ProviderOpenAI || entry.GetBaseUrl() != s.endpoint || entry.GetProxy() != "" ||
			strings.TrimSpace(entry.GetModel()) == "" || strings.TrimSpace(entry.GetApiKey()) == "" {
			return webapi.Errorf(webapi.CodeInvalidArgument, "%s", errModelNodeConfig)
		}
	}
	return nil
}

func (s *modelNodeConfigStore) PrepareDistributeConfig(ctx context.Context, value *types.DistributeConfig) (*webservice.PreparedConfig, error) {
	// 在原生归一化之前拒绝调用方代理/别名/地址；key留到原生保留后再核。
	for _, entry := range value.GetLlm().GetProviders() {
		if entry.GetProxy() != "" || entry.GetProvider() != provider.ProviderOpenAI || entry.GetBaseUrl() != s.endpoint {
			return nil, webapi.Errorf(webapi.CodeInvalidArgument, "%s", errModelNodeConfig)
		}
	}
	return s.modelConfigStore.PrepareDistributeConfig(ctx, value)
}

func (s *modelNodeConfigStore) GetDistributeConfig(ctx context.Context) (string, bool, *types.DistributeConfig, error) {
	path, loaded, value, err := s.modelConfigStore.GetDistributeConfig(ctx)
	if err == nil {
		err = s.validate(value)
	}
	if err != nil {
		return "", false, nil, err
	}
	return path, loaded, value, nil
}

type modelNodeReport struct {
	NodeReturned      bool `json:"node_returned"`
	WorkspaceRemoved  bool `json:"workspace_removed"`
	Profiles          int  `json:"profiles"`
	ProfilesClosed    int  `json:"profiles_closed"`
	ClientsCreated    int  `json:"clients_created"`
	ClientsReleased   int  `json:"clients_idle_closed_after_drain"`
	NoActiveProcesses bool `json:"no_active_processes"`
}

type modelNodeClient struct {
	original provider.ProviderConfig // 独立原始用户快照，不含内部pin。
	client   *modeltransport.Client
}

// 薄组合根；不实现Loop、Session、配置协议或模型协议。
type modelNodeProfile struct {
	*harness.Harness
	endpoint   string
	capability *businesswebapi.Capability
	protocols  *extension.Set
	namespaces *namespaces.Registry
	runtime    *agentsession.Runtime
	processes  *coreproc.Manager

	// desired只读唯一ConfigStore，装配后不变；不是第二份期望状态。
	desired  func() (provider.ProviderConfig, error)
	mu       sync.Mutex // 发布与开始关闭共用；不在此锁内等待session排空。
	closeMu  sync.Mutex // 串行整个 Load/Close，避免关闭后继续发布资源。
	closing  bool
	closed   bool
	clients  []modelNodeClient
	released int
}

var _ profile.Profile = (*modelNodeProfile)(nil)

func newModelNodeProfile(request profile.Request, directory, endpoint string, capability *businesswebapi.Capability) (*modelNodeProfile, error) {
	// 原生首次空配置可能重建一次禁用Profile；任何其它图重建都明确拒绝。
	// 绝不将StartupOptional传给Harness的原生Provider构造器。
	if request.ProviderMode != provider.StartupDisabled || request.Session == nil || !modelEndpointAllowed(endpoint) || capability == nil {
		return nil, errors.New("本机模型节点只接受禁用Provider的原生Profile装配及现有webapi能力")
	}
	p := &modelNodeProfile{endpoint: endpoint, capability: capability}
	session := *request.Session
	session.NodeName, session.SkipBaseSkills, session.Logger = modelNodeID, true, telemetry.NopLogger()
	session.MaxPending = 1
	h, err := harness.New(harness.Config{
		Base: harness.BaseConfig{
			Directory: directory, SkillExclude: []string{"cyber"}, Logger: telemetry.NopLogger(),
			Provider: provider.StartupConfig{Mode: provider.StartupDisabled}, Egress: capability.EgressExtension(),
			Terminal: terminalext.Config{Timeout: 5, MaximumTimeout: 5 * time.Second},
		},
		Extensions: []extension.Extension{modelNodeAdmissionExtension(), capability.ToolExtension(), capability.ApprovalExtension(), capability.SubagentExtension(), capability.BrowserExtension(directory)}, Session: &session,
	})
	if err != nil {
		return nil, err
	}
	p.Harness = h
	return p, nil
}

func (p *modelNodeProfile) Load(ctx context.Context) error {
	p.closeMu.Lock()
	defer p.closeMu.Unlock()
	p.mu.Lock()
	closing := p.closing
	p.mu.Unlock()
	if closing {
		return errModelNodeClosed
	}
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
	p.protocols, err = extension.New(p.namespaces, extension.Func{LoadFunc: func(scope *extension.Scope) error {
		// 使用Runtime的同一handler，仅加准入，不复制Open/Run/Cancel/Close。
		return extension.Add(scope,
			aop.Shared(&aop.ProtocolMessage{}, p.handleCore),
			aop.Shared(&types.CommandProtocolMessage{}, modelNodeCommandDenied),
			// 测试策略审批的 Pending/Resolve；上游 guardrail 保持 provider=none，不绑定其 NewProtocol。
			p.capability.ApprovalBinding(),
		)
	}})
	if err != nil {
		return err
	}
	return p.protocols.Load(ctx)
}

func (p *modelNodeProfile) RegisterNamespaces(mux *aop.NamespaceMux) error {
	return p.namespaces.Bind(mux)
}
func (*modelNodeProfile) ConsoleBindings() *consoleapi.Bindings { return nil }

func (p *modelNodeProfile) AgentStatus() *aop.AgentStatus {
	// desired在Load前固定；先读唯一store，不持Profile锁做文件I/O。
	var expected provider.ProviderConfig
	var err error
	if p.desired != nil {
		expected, err = p.desired()
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	state, _ := p.Providers()
	status := node.AgentStatus(state)
	if state == nil || p.desired == nil {
		return status
	}
	// 尝试错误归原生ReloadResult；持续提示只能由已提交期望与实际实例派生。
	// A→失败B→保存回A时，即使原生unchanged短路，也不会残留历史错误。
	if err != nil {
		status.ConfigError = "无法核对已保存模型配置；当前实例未被替换"
		return status
	}
	active, _ := state.Current()
	if active == nil && emptyModelNodeConfig(expected) {
		return status
	}
	for _, entry := range p.clients {
		if entry.client == active && reflect.DeepEqual(entry.original, expected) {
			return status
		}
	}
	status.ConfigError = "已保存模型配置尚未在本机节点应用；当前实例未被替换"
	return status
}

func emptyModelNodeConfig(config provider.ProviderConfig) bool {
	return (config.Provider == "" || config.Provider == provider.ProviderOpenAI) && config.Model == "" && config.APIKey == "" && config.BaseURL == "" && config.Proxy == ""
}

func modelNodeDesired(store webservice.ConfigStore, option *cfg.Option) func() (provider.ProviderConfig, error) {
	return func() (provider.ProviderConfig, error) {
		_, _, value, err := store.GetDistributeConfig(context.Background())
		if err != nil {
			return provider.ProviderConfig{}, err
		}
		resolved, err := cfg.ResolveDistributedRuntime(value, option)
		if err != nil {
			return provider.ProviderConfig{}, err
		}
		return cloneModelNodeConfig(cfg.ProviderConfig(resolved)), nil
	}
}

func cloneModelNodeConfig(config provider.ProviderConfig) provider.ProviderConfig {
	if config.Images != nil {
		images := *config.Images
		config.Images = &images
	}
	return config
}

func (p *modelNodeProfile) ReloadProvider(ctx context.Context, config provider.ProviderConfig) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closing {
		return errModelNodeClosed
	}
	if err := ctx.Err(); err != nil {
		return errors.New("节点配置应用已取消；旧实例不变")
	}
	state, err := p.Providers()
	if err != nil {
		return errors.New("原生Provider状态尚未可用")
	}
	if emptyModelNodeConfig(config) {
		// 初始空广播保留StartupDisabled的not_configured健康，不伪发布。
		// 已配置后的显式清空只移除当前实例引用，旧client仍保留到排空。
		if active, _ := state.Current(); active != nil {
			state.Set(nil, provider.ProviderConfig{})
		}
		return nil
	}
	for _, entry := range p.clients {
		if reflect.DeepEqual(entry.original, config) {
			state.Set(entry.client, entry.client.Config())
			return nil
		}
	}
	if len(p.clients) >= modelNodeMaxClients {
		return errModelNodeLimit
	}
	client, err := modeltransport.NewLoopbackOpenAI(p.endpoint, config)
	if err != nil {
		return errModelNodeConfig
	}
	if err := ctx.Err(); err != nil {
		client.CloseIdleConnections()
		return errors.New("节点配置应用已取消；旧实例不变")
	}
	p.clients = append(p.clients, modelNodeClient{original: cloneModelNodeConfig(config), client: client})
	state.Set(client, client.Config()) // 运行时快照含内部pin，绝不回写store/Hub RuntimeLLM。
	return nil
}

func (p *modelNodeProfile) Close(ctx context.Context) error {
	p.closeMu.Lock()
	defer p.closeMu.Unlock()
	p.mu.Lock()
	p.closing = true
	closed := p.closed
	p.mu.Unlock()
	if closed {
		return nil
	}
	if p.protocols != nil {
		if err := p.protocols.Close(ctx); err != nil {
			return err
		}
	}
	if err := p.Harness.Close(ctx); err != nil {
		// Active=false不证明排空；关闭失败保留所有client供后续Close重试。
		return err
	}
	if p.processes != nil {
		p.processes.Shutdown()
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, entry := range p.clients {
		entry.client.CloseIdleConnections()
		p.released++
	}
	p.closed = true
	return nil
}

func modelNodeAdmissionExtension() extension.Extension {
	var subscriptions []*hooks.Subscription
	return extension.Func{
		LoadFunc: func(scope *extension.Scope) error {
			registry, err := extension.Use[*hooks.Registry](scope)
			if err != nil {
				return err
			}
			subscriptions = append(subscriptions,
				toolhooks.BeforeCommand.On(registry, "wscyber-model-node-console", func(_ context.Context, event toolhooks.CommandEvent) (toolhooks.Admission, error) {
					// 原生registry在shell展开后调用；阻止bash的内置tmux写main-repl。
					if event.Name == "tmux" || (len(event.Args) > 0 && event.Args[0] == "tmux") {
						return toolhooks.Admission{Deny: errors.New("本机模型节点暂不支持tmux/交互REPL及其模型覆盖")}, nil
					}
					return toolhooks.Admission{}, nil
				}),
				toolhooks.Before.On(registry, "wscyber-model-node-subagent", func(_ context.Context, event toolhooks.CallEvent) (toolhooks.Admission, error) {
					// oracle_explorer 由平台准入（webapi ApprovalExtension）统一决定；其它子 Agent 本机节点不开放。
					if event.Call.GetName() == "subagent" {
						var args struct {
							Action string `json:"action"`
							Name   string `json:"name"`
						}
						_ = json.Unmarshal(event.Call.GetArguments().GetData(), &args)
						if args.Name != businesswebapi.OracleExplorerName && args.Action != "catalog" && args.Action != "list" && args.Action != "kill" {
							return toolhooks.Admission{Deny: errors.New("本机模型节点只开放命名子 Agent oracle_explorer，尚未接入其它多Agent预算与模型选择")}, nil
						}
					}
					return toolhooks.Admission{}, nil
				}),
			)
			return nil
		},
		CloseFunc: func(ctx context.Context) error {
			var result error
			for _, sub := range subscriptions {
				result = errors.Join(result, sub.Close(ctx))
			}
			return result
		},
	}
}

func modelNodeCommandDenied(_ context.Context, envelope *aop.Envelope, _ proto.Message, send aop.SendFunc) error {
	return send(aop.Reply(envelope.Id, aop.NewProtocolError("FAILED_PRECONDITION", "本机模型节点暂不开放会话命令、模型覆盖、评估或定时循环")))
}

func modelNodeCoreAllowed(value *aop.ProtocolMessage) bool {
	if value == nil || len(value.ProtoReflect().GetUnknown()) != 0 {
		return false
	}
	if request := value.GetOpenSessionRequest(); request != nil {
		return len(request.Extensions) == 0 && len(request.ProtoReflect().GetUnknown()) == 0
	}
	if request := value.GetRunTurnRequest(); request != nil {
		return len(request.Extensions) == 0 && len(request.ProtoReflect().GetUnknown()) == 0 &&
			!request.ContinueSession && request.MaxTurns > 0 && request.MaxTurns <= modelNodeMaxTurns
	}
	return value.GetCancelTurnRequest() != nil || value.GetCloseSessionRequest() != nil
}

func (p *modelNodeProfile) handleCore(ctx context.Context, envelope *aop.Envelope, message proto.Message, send aop.SendFunc) error {
	value, _ := message.(*aop.ProtocolMessage)
	reason := "本机模型节点仅接受无扩展Open/Run及Cancel/Close；Run须1至3回合，不支持模型覆盖、eval或continue"
	allowed := modelNodeCoreAllowed(value)
	if allowed && value.GetRunTurnRequest() != nil {
		p.mu.Lock()
		state, err := p.Providers()
		if err != nil || p.closing {
			allowed = false
		} else {
			active, _ := state.Current()
			allowed = active != nil
		}
		p.mu.Unlock()
		if !allowed {
			reason = "本机节点未应用有效模型配置或正在关闭；未发送模型请求"
		}
	}
	if allowed {
		return p.runtime.HandleCoreNamespace(ctx, envelope, message, send)
	}
	// 仅拒绝回执使用原生类型；接收成功/执行/事件全部由原生runtime负责。
	rejected := &aop.Rejection{Code: "FAILED_PRECONDITION", Message: reason}
	var response proto.Message = aop.NewProtocolError("FAILED_PRECONDITION", reason)
	if value.GetOpenSessionRequest() != nil {
		response = &aop.ProtocolMessage{Message: &aop.ProtocolMessage_OpenSessionResponse{OpenSessionResponse: &aop.OpenSessionResponse{Outcome: &aop.OpenSessionResponse_Rejected{Rejected: rejected}}}}
	} else if value.GetRunTurnRequest() != nil {
		response = &aop.ProtocolMessage{Message: &aop.ProtocolMessage_RunTurnResponse{RunTurnResponse: &aop.RunTurnResponse{Outcome: &aop.RunTurnResponse_Rejected{Rejected: rejected}}}}
	}
	return send(aop.Reply(envelope.Id, response))
}

func serveModelNode(ctx context.Context, config host.Config, stateDir, key, endpoint string, report *modelNodeReport, capability *businesswebapi.Capability) (resultErr error) {
	if report == nil {
		report = &modelNodeReport{}
	}
	directory, err := os.MkdirTemp(stateDir, "model-node-")
	if err != nil {
		return err
	}
	defer func() {
		if report.ProfilesClosed != report.Profiles || !report.NoActiveProcesses {
			return // 未排空不删仍被使用的工作区，由操作者按本次归属处理。
		}
		err := os.RemoveAll(directory)
		report.WorkspaceRemoved = err == nil
		resultErr = errors.Join(resultErr, err)
	}()
	store := newModelNodeConfigStore(stateDir, endpoint)
	config.Management.ConfigStore = store
	config.Management.ConfigAPI.Sections = store.native.Codec.Sections
	// RuntimeLLM/InitialProfile/BuildProfile均不安装；只广播原始store配置。
	config.Extensions = append(config.Extensions, modelProbeDenialExtension())
	var profiles []*modelNodeProfile
	var nodeErr error
	config.StartNode = func(nodeCtx context.Context, address string) error {
		defer func() { report.NodeReturned = true }()
		server := (&url.URL{Scheme: "http", Host: address, User: url.User(key)}).String()
		option := &cfg.Option{
			Explicit:     map[string]bool{},
			Context:      &cfg.Context{Directory: directory, Home: directory, Executable: filepath.Join(directory, "model-node"), LookupEnv: func(string) (string, bool) { return "", false }},
			NodeOptions:  cfg.NodeOptions{NodeID: modelNodeID, NodeName: modelNodeID},
			MiscOptions:  cfg.MiscOptions{DataDir: directory},
			AgentOptions: cfg.AgentOptions{ServerURL: server, Timeout: 3600},
		}
		err := node.RunWebSocketWithCapabilities(nodeCtx, func(request profile.Request) (profile.Profile, error) {
			if len(profiles) >= 2 {
				return nil, errors.New("本机节点不支持额外运行图重建")
			}
			p, err := newModelNodeProfile(request, directory, endpoint, capability)
			if err != nil {
				return nil, err
			}
			p.desired = modelNodeDesired(store, option)
			profiles = append(profiles, p)
			return p, nil
		}, option, telemetry.NopLogger(), "wscyber.local-model.v1", "wscyber.webapi-http.v1")
		if err != nil && !errors.Is(err, context.Canceled) {
			nodeErr = errors.New("本机原生模型节点退出失败；不输出含认证信息的底层错误")
		}
		return nodeErr
	}
	resultErr = errors.Join(host.Serve(ctx, config), nodeErr)
	// host已取消并等待StartNode；这里再核原生关闭，失败可重试排空，不提前关client。
	return errors.Join(resultErr, closeModelNodeProfiles(profiles, report))
}

func closeModelNodeProfiles(profiles []*modelNodeProfile, report *modelNodeReport) (resultErr error) {
	report.Profiles, report.NoActiveProcesses = len(profiles), true
	for _, p := range profiles {
		err := p.Close(context.Background())
		resultErr = errors.Join(resultErr, err)
		p.mu.Lock()
		if p.closed {
			report.ProfilesClosed++
		}
		report.ClientsCreated += len(p.clients)
		report.ClientsReleased += p.released
		p.mu.Unlock()
		// 未加载/加载失败时 Manager 尚未发布；Close 的错误仍向上传递。
		if p.processes != nil {
			for _, entry := range p.processes.List() {
				if entry.State.Active() {
					report.NoActiveProcesses = false
				}
			}
		}
	}
	if !report.NoActiveProcesses {
		resultErr = errors.Join(resultErr, errors.New("原生节点仍有活跃资源"))
	}
	return resultErr
}
