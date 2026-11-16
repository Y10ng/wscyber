// Package modelprovider 只向显式配置的模型端点发有界请求，不执行模型返回的工具动作。
package modelprovider

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

const (
	maxResponseBytes = 1 << 20
	// 256KiB上下文JSON转义最多六倍，另含32KiB参数与协议信封；线上的体另设硬上限。
	maxRequestBytes = 2 << 20
)

var errResponse = errors.New("模型响应格式无效或超过上限")

type boundedTransport struct {
	transport    *http.Transport
	endpoint     *url.URL
	attempted    bool
	status       int
	incomplete   bool
	response     []byte // 仅本次调用的有界内存；是否加密归档由账本决定。
	received     bool
	complete     bool
	jsonResponse bool
	mediaType    string
	allowSend    func() bool
}

// 仅在拨号时解析一次，并直接拨选定IP；不让DNS检查与实际连接分离。
func allowedAddress(ip netip.Addr) bool {
	ip = ip.Unmap()
	return ip.IsValid() && !ip.IsUnspecified() && !ip.IsMulticast() && !ip.IsLinkLocalUnicast() && !ip.IsLinkLocalMulticast()
}

func dialEndpoint(ctx context.Context, network, address string) (net.Conn, error) {
	return dialResolved(ctx, network, address, net.DefaultResolver.LookupNetIP, (&net.Dialer{Timeout: 10 * time.Second}).DialContext)
}

func dialResolved(ctx context.Context, network, address string, lookup func(context.Context, string, string) ([]netip.Addr, error), dial func(context.Context, string, string) (net.Conn, error)) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, errors.New("模型端点无效")
	}
	addresses, err := lookup(ctx, "ip", host)
	if err != nil || len(addresses) == 0 {
		return nil, errors.New("模型端点解析失败")
	}
	for _, ip := range addresses {
		if !allowedAddress(ip) {
			return nil, errors.New("模型端点解析到禁止地址")
		}
	}
	// 不因一次连接失败尝试多个地址，避免隐式重试。
	return dial(ctx, network, net.JoinHostPort(addresses[0].Unmap().String(), port))
}

func newTransport(endpoint *url.URL) *boundedTransport {
	return &boundedTransport{endpoint: endpoint, transport: &http.Transport{
		Proxy: nil, DialContext: dialEndpoint, DisableKeepAlives: true, DisableCompression: true,
		TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}, TLSHandshakeTimeout: 10 * time.Second,
		ResponseHeaderTimeout: 0, MaxResponseHeaderBytes: 32 << 10,
		// 新连接、禁HTTP/2及连接复用，避免底层重试可能已发送的POST。
		ForceAttemptHTTP2: false, TLSNextProto: map[string]func(string, *tls.Conn) http.RoundTripper{},
	}}
}

func (t *boundedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Body != nil {
		defer req.Body.Close()
	}
	if t.attempted || req.Context().Err() != nil || req.Method != http.MethodPost || req.URL.String() != t.endpoint.String() || (req.Host != "" && req.Host != t.endpoint.Host) {
		return nil, errors.New("模型请求超出固定端点、已取消或重复发送")
	}
	if req.Body == nil || req.ContentLength > maxRequestBytes {
		return nil, errors.New("模型请求体为空或超过上限")
	}
	body, err := io.ReadAll(io.LimitReader(req.Body, maxRequestBytes+1))
	if err != nil || len(body) > maxRequestBytes {
		return nil, errors.New("模型请求体读取失败或超过上限")
	}
	req = req.Clone(req.Context())
	req.Body = io.NopCloser(bytes.NewReader(body))
	req.ContentLength = int64(len(body))
	req.GetBody = nil // 传输层不可重建请求重放。
	if t.allowSend != nil && !t.allowSend() {
		return nil, errors.New("模型调用账本已停止派发")
	}
	t.attempted = true
	resp, err := t.transport.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	t.status = resp.StatusCode
	defer resp.Body.Close()
	data, readErr := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	t.received = true
	t.response = data[:min(len(data), maxResponseBytes)]
	t.complete = readErr == nil && len(data) <= maxResponseBytes
	t.incomplete = !t.complete
	t.mediaType = "application/octet-stream"
	mediaType, _, mediaErr := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	encoding := resp.Header.Get("Content-Encoding")
	plain := encoding == "" || strings.EqualFold(encoding, "identity")
	isJSON := mediaErr == nil && (mediaType == "application/json" || (strings.HasPrefix(mediaType, "application/") && strings.HasSuffix(mediaType, "+json")))
	if isJSON && plain {
		t.mediaType = "application/json"
	}
	t.jsonResponse = t.complete && isJSON && plain && validResponseJSON(data)
	// 错误响应同样有界读取供usage/可选原文使用，但不让SDK错误对象携带远端正文。
	if resp.StatusCode != http.StatusOK {
		resp.Body = http.NoBody
		resp.ContentLength = 0
		return resp, nil
	}
	if !t.jsonResponse {
		return nil, errResponse
	}
	resp.Body = io.NopCloser(bytes.NewReader(data))
	resp.ContentLength = int64(len(data))
	return resp, nil
}
