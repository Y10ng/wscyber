package main

import (
	"context"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"sync"

	"github.com/chainreactors/cyber/agent/provider"
	"github.com/chainreactors/cyber/aop"
	"github.com/chainreactors/cyber/core/extension"
	"github.com/chainreactors/cyber/core/namespaces"
	coreproc "github.com/chainreactors/cyber/core/proc"
	"github.com/chainreactors/cyber/core/telemetry"
	cfg "github.com/chainreactors/cyber/pkg/config"
	consoleapi "github.com/chainreactors/cyber/pkg/console/api"
	"github.com/chainreactors/cyber/pkg/harness"
	"github.com/chainreactors/cyber/pkg/node"
	"github.com/chainreactors/cyber/pkg/profile"
	"github.com/chainreactors/cyber/pkg/web/host"

	businesswebapi "wscyber/native/webapi"
)

const productModelNodeID = "wscyber-model-node"

var errProductModelNodeClosed = errors.New("模型节点正在关闭，不接受配置应用")

// productModelClient 记录借给原生 State 的实例；排空后由本 Profile 关闭。
type productModelClient struct {
	original provider.ProviderConfig
	resolved provider.ProviderConfig
	client   provider.Provider
}

// productModelProfile 只组合原生 Harness/Runtime，不实现另一套会话或运行循环。
type productModelProfile struct {
	*harness.Harness
	capability *businesswebapi.Capability
	namespaces *namespaces.Registry
	protocols  *extension.Set
	processes  *coreproc.Manager
	desired    func() (provider.ProviderConfig, error)

	mu       sync.Mutex
	closing  bool
	closed   bool
	clients  []productModelClient
	released int
}

var _ profile.Profile = (*productModelProfile)(nil)

func newProductModelProfile(request profile.Request, directory string, capability *businesswebapi.Capability) (*productModelProfile, error) {
	// 节点始终以禁用 Provider 的原生 Profile 启动；保存后的广播只走 ReloadProvider。
	if request.ProviderMode != provider.StartupDisabled || request.Session == nil || capability == nil {
		return nil, errors.New("正式模型节点只接受禁用 Provider 的原生 Profile 装配")
	}
	session := *request.Session
	session.NodeName, session.Logger = productModelNodeID, telemetry.NopLogger()
	h, err := harness.New(harness.Config{
		Base: harness.BaseConfig{
			Directory: directory, Logger: telemetry.NopLogger(),
			Provider: provider.StartupConfig{Mode: provider.StartupDisabled}, Egress: capability.EgressExtension(),
		},
		Extensions: []extension.Extension{capability.ToolExtension(), capability.ApprovalExtension(), capability.SubagentExtension(), capability.BrowserExtension(directory)}, Session: &session,
	})
	if err != nil {
		return nil, err
	}
	return &productModelProfile{Harness: h, capability: capability}, nil
}

func (p *productModelProfile) Load(ctx context.Context) error {
	if err := p.Harness.Load(ctx); err != nil {
		return err
	}
	runtime, err := p.Runtime()
	if err != nil {
		return err
	}
	p.processes, err = p.Processes()
	if err != nil {
		return err
	}
	p.namespaces = namespaces.New()
	set, err := extension.New(p.namespaces, extension.Func{LoadFunc: func(scope *extension.Scope) error {
		// 原生会话绑定 + 测试策略审批 Pending/Resolve（不绑定上游 guardrail.NewProtocol）。
		return extension.Add(scope, append(runtime.NamespaceBindings(), p.capability.ApprovalBinding())...)
	}})
	if err != nil {
		return err
	}
	p.protocols = set
	return set.Load(ctx)
}

func (p *productModelProfile) RegisterNamespaces(mux *aop.NamespaceMux) error {
	return p.namespaces.Bind(mux)
}
func (*productModelProfile) ConsoleBindings() *consoleapi.Bindings { return nil }

func (p *productModelProfile) AgentStatus() *aop.AgentStatus {
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
	status.ConfigError = "已保存模型配置尚未在节点应用；当前实例未被替换"
	return status
}

func (p *productModelProfile) ReloadProvider(ctx context.Context, config provider.ProviderConfig) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closing {
		return errProductModelNodeClosed
	}
	if err := ctx.Err(); err != nil {
		return errors.New("节点配置应用已取消；旧实例不变")
	}
	state, err := p.Providers()
	if err != nil {
		return errors.New("原生 Provider 状态尚未可用")
	}
	if emptyModelNodeConfig(config) {
		if active, _ := state.Current(); active != nil {
			state.Set(nil, provider.ProviderConfig{})
		}
		return nil
	}
	for _, entry := range p.clients {
		if reflect.DeepEqual(entry.original, config) {
			state.Set(entry.client, entry.resolved)
			return nil
		}
	}
	resolved, err := provider.Resolve(&config)
	if err != nil {
		return errors.New("模型供应商或参数无效；当前实例未被替换")
	}
	client, err := provider.NewProviderFromResolved(resolved)
	if err != nil {
		return errors.New("无法构造模型供应商；当前实例未被替换")
	}
	if err := ctx.Err(); err != nil {
		closeProductProvider(client)
		return errors.New("节点配置应用已取消；旧实例不变")
	}
	// Set 仅借入实例，不拥有也不探测；本会话/Harness 排空后才统一关闭。
	p.clients = append(p.clients, productModelClient{original: cloneModelNodeConfig(config), resolved: cloneModelNodeConfig(*resolved), client: client})
	state.Set(client, *resolved)
	return nil
}

func (p *productModelProfile) Close(ctx context.Context) error {
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
		return err
	}
	if p.processes != nil {
		// 原生 REPL 已随 Runtime 取消；Manager.Shutdown 再等待进程登记进入终态。
		p.processes.Shutdown()
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, entry := range p.clients {
		closeProductProvider(entry.client)
		p.released++
	}
	p.closed = true
	return nil
}

func closeProductProvider(client provider.Provider) {
	if closer, ok := client.(interface{ CloseIdleConnections() }); ok {
		closer.CloseIdleConnections()
	}
}

func serveProductModelNode(ctx context.Context, config host.Config, stateDir, key string, report *modelNodeReport, capability *businesswebapi.Capability) (resultErr error) {
	if report == nil {
		report = &modelNodeReport{}
	}
	directory, err := os.MkdirTemp(stateDir, "model-node-")
	if err != nil {
		return err
	}
	defer func() {
		if report.ProfilesClosed != report.Profiles || !report.NoActiveProcesses {
			return
		}
		err := os.RemoveAll(directory)
		report.WorkspaceRemoved = err == nil
		resultErr = errors.Join(resultErr, err)
	}()
	store := newModelConfigStore(stateDir)
	config.Management.ConfigStore = store
	config.Management.ConfigAPI.Sections = store.native.Codec.Sections
	var profiles []*productModelProfile
	var nodeErr error
	config.StartNode = func(nodeCtx context.Context, address string) error {
		defer func() { report.NodeReturned = true }()
		server := (&url.URL{Scheme: "http", Host: address, User: url.User(key)}).String()
		option := &cfg.Option{
			Explicit:     map[string]bool{},
			Context:      &cfg.Context{Directory: directory, Home: directory, Executable: filepath.Join(directory, "model-node"), LookupEnv: func(string) (string, bool) { return "", false }},
			NodeOptions:  cfg.NodeOptions{NodeID: productModelNodeID, NodeName: productModelNodeID},
			MiscOptions:  cfg.MiscOptions{DataDir: directory},
			AgentOptions: cfg.AgentOptions{ServerURL: server, Timeout: 3600},
		}
		err := node.RunWebSocketWithCapabilities(nodeCtx, func(request profile.Request) (profile.Profile, error) {
			p, err := newProductModelProfile(request, directory, capability)
			if err != nil {
				return nil, err
			}
			p.desired = modelNodeDesired(store, option)
			profiles = append(profiles, p)
			return p, nil
		}, option, telemetry.NopLogger(), "wscyber.model.v1", "wscyber.webapi-http.v1")
		if err != nil && !errors.Is(err, context.Canceled) {
			nodeErr = errors.New("正式原生模型节点退出失败；不输出含认证信息的底层错误")
		}
		return nodeErr
	}
	resultErr = errors.Join(host.Serve(ctx, config), nodeErr)
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
		if p.processes == nil {
			report.NoActiveProcesses = false
			continue
		}
		for _, entry := range p.processes.List() {
			if entry.State.Active() {
				report.NoActiveProcesses = false
			}
		}
	}
	if !report.NoActiveProcesses {
		resultErr = errors.Join(resultErr, errors.New("原生节点仍有活跃资源"))
	}
	return resultErr
}
