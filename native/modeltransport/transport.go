// Package modeltransport 为固定 Cyber OpenAI Provider 装配单一连接目的地。
// 它不是代理服务、HTTP 路径授权器、工具出口或新的 Harness。
package modeltransport

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"github.com/chainreactors/cyber/agent/provider"
	"github.com/chainreactors/proxyclient"
)

const pinScheme = "wscyber-model-pin"

var (
	ErrConfiguration = errors.New("受控模型配置无效：仅支持独立指定的本机 OpenAI HTTP 端点且不得配置代理")
	ErrDestination   = errors.New("模型连接目的地不在本实例允许范围内")
)

func init() {
	// 上游注册表无锁：只在包初始化时注册一次，不在构造/请求期间更改。
	// 不覆盖内建 scheme 或 DefaultDial，也不使用全局可变目的地。
	proxyclient.RegisterScheme(pinScheme, pinFactory)
}

// Client 私有持有原生 Provider，仅暴露本片支持的完成请求与 SSE。
// 不匿名嵌入原生对象，因此不会额外发布 ListModels/WebSearch 等能力。
// 请求/响应编码、SSE、超时及错误语义仍完全由原生 Provider 实现。
// 与原生接口一样，请求对象可能被修改，调用方不要并发复用同一请求。
// 零值不可用；必须通过 NewLoopbackOpenAI 构造。
type Client struct {
	native *provider.OpenAIProvider
	config provider.ProviderConfig
}

var _ provider.StreamingProvider = (*Client)(nil)

// NewLoopbackOpenAI 不请求网络、不探测模型。
// allowedEndpoint 必须由可信装配独立指定，不能从待执行模型配置自行派生授权。
// 当前仅接受规范 http://127.0.0.1:<非零端口>/v1，且 config.BaseURL 必须完全相同。
// 连接级 pin 会检查每次新连接；同一目的地的路径/协议重定向不保证拒绝。
func NewLoopbackOpenAI(allowedEndpoint string, config provider.ProviderConfig) (*Client, error) {
	address, err := parseEndpoint(allowedEndpoint)
	if err != nil || config.Provider != provider.ProviderOpenAI || config.BaseURL != allowedEndpoint || config.Proxy != "" || strings.TrimSpace(config.Model) == "" {
		return nil, ErrConfiguration
	}

	config = cloneConfig(config)
	// 该值只是给原生 Transport.DialContext 的内部装配参数，不是网络代理地址。
	config.Proxy = pinScheme + "://" + address.String()
	resolved, err := provider.Resolve(&config)
	if err != nil {
		return nil, ErrConfiguration
	}
	// 原生构造器保存传入指针；内部配置、调用方输入、可返回快照分别拥有副本。
	nativeConfig := cloneConfig(*resolved)
	p, err := provider.NewOpenAIProvider(&nativeConfig)
	if err != nil {
		return nil, ErrConfiguration
	}
	return &Client{native: p, config: cloneConfig(*resolved)}, nil
}

// Config 返回可用于 State.Set 的运行时快照，包含秘密和内部 pin scheme。
// 不得记录、回显或把内部 Proxy 写回用户配置；每次返回独立副本。
// 本片不接 State.Reload/Update、ForModel、fallback 或节点重载。
func (c *Client) Config() provider.ProviderConfig { return cloneConfig(c.config) }

func (c *Client) Name() string { return c.native.Name() }

func (c *Client) ChatCompletion(ctx context.Context, request *provider.ChatCompletionRequest) (*provider.ChatCompletionResponse, error) {
	return c.native.ChatCompletion(ctx, request)
}

func (c *Client) ChatCompletionStream(ctx context.Context, request *provider.ChatCompletionRequest) (<-chan provider.ChatCompletionStreamEvent, error) {
	return c.native.ChatCompletionStream(ctx, request)
}

// CloseIdleConnections 应在所有依赖调用/流排空后执行，不会终止活跃调用。
// 这不是 Close：之后再次请求仍会建立连接，并继续经过同一不可变 pin。
// 发布替代实例不负责关闭旧实例；未来组合根须在旧 turn/session 排空后关闭它。
func (c *Client) CloseIdleConnections() { c.native.CloseIdleConnections() }

func cloneConfig(config provider.ProviderConfig) provider.ProviderConfig {
	if config.Images != nil {
		images := *config.Images
		config.Images = &images
	}
	return config
}

func parseEndpoint(endpoint string) (netip.AddrPort, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "http" {
		return netip.AddrPort{}, ErrConfiguration
	}
	address, err := parseAddress(u.Host)
	if err != nil || endpoint != "http://"+address.String()+"/v1" {
		return netip.AddrPort{}, ErrConfiguration
	}
	return address, nil
}

func parseAddress(value string) (netip.AddrPort, error) {
	address, err := netip.ParseAddrPort(value)
	if err != nil || address.Addr() != netip.AddrFrom4([4]byte{127, 0, 0, 1}) || address.Port() == 0 || address.String() != value {
		return netip.AddrPort{}, ErrDestination
	}
	return address, nil
}

func pinFactory(u *url.URL, _ proxyclient.Dial) (proxyclient.Dial, error) {
	// 忽略可变的上游 DefaultDial；只使用本实例自己的标准 net.Dialer。
	if u == nil || !strings.EqualFold(u.Scheme, pinScheme) || u.User != nil || u.Opaque != "" || u.OmitHost || u.Path != "" || u.RawPath != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.RawFragment != "" {
		return nil, ErrConfiguration
	}
	address, err := parseAddress(u.Host)
	if err != nil {
		return nil, ErrConfiguration
	}
	dialer := &net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}
	return pinnedDial(address, dialer.DialContext), nil
}

func pinnedDial(allowed netip.AddrPort, connect proxyclient.Dial) proxyclient.Dial {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		// ParseAddrPort 不解析 DNS；拒绝发生在任何底层拨号之前。
		if network != "tcp" && network != "tcp4" {
			return nil, ErrDestination
		}
		requested, err := parseAddress(address)
		if err != nil || requested != allowed {
			return nil, ErrDestination
		}
		// 不将请求字符串传给另一个可变解析器/代理，拨号固定字面 IPv4。
		return connect(ctx, "tcp4", allowed.String())
	}
}
