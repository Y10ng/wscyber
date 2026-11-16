package execution

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"wscyber/internal/domain"
)

// writeTrackedConn 仅在 HTTP 层尝试写入后标记效果可能未知；TLS 握手自身不算请求发送。
// Write 报错也不能证明对端未收到，因此在调用底层 Write 前保守置位。
type writeTrackedConn struct {
	net.Conn
	written *atomic.Bool
}

func (c *writeTrackedConn) Write(p []byte) (int, error) {
	if len(p) > 0 {
		c.written.Store(true)
	}
	return c.Conn.Write(p)
}

func (e *Executor) executeHTTP(ctx context.Context, input domain.ActionInput, origins []string) (result domain.ActionResult, err error) {
	result.ExitCode = -1
	u, err := e.validateHTTP(input, origins)
	if err != nil {
		return result, err
	}
	ip, err := e.resolve(ctx, u.Hostname())
	if err != nil {
		return result, err
	}
	result.ResolvedIP = ip.String()
	port := u.Port()
	if port == "" {
		port = "80"
		if u.Scheme == "https" {
			port = "443"
		}
	}
	expectedAddress := net.JoinHostPort(u.Hostname(), port)
	pinnedAddress := net.JoinHostPort(ip.String(), port)
	var written atomic.Bool
	actionContext := ctx
	dialer := net.Dialer{Timeout: e.config.Timeout, KeepAlive: -1}
	dial := func(_ context.Context, network, address string) (net.Conn, error) {
		if network != "tcp" || address != expectedAddress {
			return nil, fmt.Errorf("拒绝非预期连接地址")
		}
		// 连接参数为解析时已经核验的 IP，绝不再次解析域名。
		// Transport 可能让拨号脱离请求取消；强制绑定本次动作 context。
		return dialer.DialContext(actionContext, "tcp", pinnedAddress)
	}
	tlsConfig := e.tlsConfig.Clone()
	tlsConfig.ServerName = u.Hostname()
	tlsConfig.NextProtos = []string{"http/1.1"}
	transport := &http.Transport{
		Proxy:                  nil, // 不读取 HTTP_PROXY、HTTPS_PROXY 或 NO_PROXY。
		DisableKeepAlives:      true,
		DisableCompression:     true, // 不隐式解压，响应体保留收到的表示字节。
		MaxResponseHeaderBytes: maxHeaderBytes,
		ResponseHeaderTimeout:  e.config.Timeout,
		TLSHandshakeTimeout:    e.config.Timeout,
		ExpectContinueTimeout:  time.Second,
		ForceAttemptHTTP2:      false,
		TLSClientConfig:        tlsConfig,
		TLSNextProto:           map[string]func(string, *tls.Conn) http.RoundTripper{},
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			conn, err := dial(ctx, network, address)
			if err != nil {
				return nil, err
			}
			return &writeTrackedConn{Conn: conn, written: &written}, nil
		},
		DialTLSContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			conn, err := dial(ctx, network, address)
			if err != nil {
				return nil, err
			}
			tlsConn := tls.Client(conn, tlsConfig)
			if err := tlsConn.HandshakeContext(actionContext); err != nil {
				conn.Close()
				return nil, err
			}
			return &writeTrackedConn{Conn: tlsConn, written: &written}, nil
		},
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{
		Transport: transport,
		Timeout:   e.config.Timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	method := input.Method
	if method == "" {
		method = http.MethodGet
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), strings.NewReader(input.Body))
	if err != nil {
		return result, invalid("无法构造 HTTP 请求")
	}
	// 不提供 GetBody，避免传输层在连接故障时重放请求。
	req.GetBody = nil
	for name, value := range input.Headers {
		req.Header.Set(name, value)
	}
	response, err := client.Do(req)
	if err != nil {
		result.EffectUnknown = written.Load()
		return result, fmt.Errorf("HTTP 请求失败：%w", err)
	}
	defer response.Body.Close()
	result.StatusCode = response.StatusCode
	result.Headers = response.Header.Clone()
	if response.StatusCode == http.StatusSwitchingProtocols {
		result.EffectUnknown = true
		return result, fmt.Errorf("首期不支持协议升级或 WebSocket")
	}
	// 这是结构化响应的正文，不是 TLS、HTTP framing 或压缩前的原始线上字节流。
	body, readErr := io.ReadAll(io.LimitReader(response.Body, e.config.MaxOutputBytes+1))
	if int64(len(body)) > e.config.MaxOutputBytes {
		body = body[:e.config.MaxOutputBytes]
		result.Truncated = true
	}
	result.Body = body
	if readErr != nil {
		result.Truncated = true
		result.EffectUnknown = written.Load()
		return result, fmt.Errorf("响应读取未完成：%w", readErr)
	}
	result.ExitCode = 0
	return result, nil
}
