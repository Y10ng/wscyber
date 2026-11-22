package webapi

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// egressRelay 是给 Chromium 用的本机无认证代理：Chrome 的 --proxy-server 不接受用户名口令，
// 无法直接带出口令牌，所以每个浏览器实例配一个只监听 127.0.0.1 的中继，由它在转发到
// ProxyHub 时补上当前调用的 Proxy-Authorization（令牌）。中继不做策略判定，
// 放行/拒绝与记录全部仍在 ProxyHub 的鉴权回调（egressEndpoint.authorize）与 flow 观察里。
//
// 归属：每次浏览器/爬虫调用 activate 一个令牌，调用结束 deactivate——关闭该令牌下的
// 全部 CONNECT 隧道与在途转发，Chrome 之后的请求必须在新调用里重新建连、重新鉴权。
// 没有进行中的调用时（两次调用之间页面的后台请求）中继直接拒绝并经 onIdle 记录，不发送。
type egressRelay struct {
	hubAddr  string
	listener net.Listener
	server   *http.Server
	onIdle   func(*http.Request)

	mu       sync.Mutex
	active   []*relayRoute // 后激活者在后；新连接用最后一个
	idleSeen map[string]bool
	closed   bool
}

type relayRoute struct {
	auth      string
	transport *http.Transport
	ctx       context.Context
	cancel    context.CancelFunc

	mu    sync.Mutex
	conns map[net.Conn]struct{}
	done  bool
}

func newEgressRelay(hubProxyURL string, onIdle func(*http.Request)) (*egressRelay, error) {
	hub, err := url.Parse(hubProxyURL)
	if err != nil || hub.Host == "" {
		return nil, errors.New("出口代理地址无效")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("浏览器出口中继监听失败: %w", err)
	}
	r := &egressRelay{hubAddr: hub.Host, listener: listener, onIdle: onIdle, idleSeen: map[string]bool{}}
	r.server = &http.Server{Handler: r, ReadHeaderTimeout: 30 * time.Second, ErrorLog: nil}
	go func() { _ = r.server.Serve(listener) }()
	return r, nil
}

// URL 是传给 Chromium --proxy-server 的地址（不含令牌）。
func (r *egressRelay) URL() string { return "http://" + r.listener.Addr().String() }

// activate 让之后新建的连接使用 tokenProxyURL 里的令牌；返回的函数在调用结束时执行。
func (r *egressRelay) activate(tokenProxyURL string) func() {
	proxy, err := url.Parse(tokenProxyURL)
	token := egressTokenFromURL(tokenProxyURL)
	if err != nil || token == "" {
		return func() {}
	}
	ctx, cancel := context.WithCancel(context.Background())
	route := &relayRoute{
		auth: "Basic " + base64.StdEncoding.EncodeToString([]byte(token+":")),
		transport: &http.Transport{
			Proxy: http.ProxyURL(proxy), DisableCompression: true, MaxIdleConnsPerHost: 8,
			IdleConnTimeout: 30 * time.Second, ResponseHeaderTimeout: 2 * time.Minute,
		},
		ctx: ctx, cancel: cancel, conns: map[net.Conn]struct{}{},
	}
	r.mu.Lock()
	r.active = append(r.active, route)
	r.idleSeen = map[string]bool{}
	r.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			r.mu.Lock()
			for i, item := range r.active {
				if item == route {
					r.active = append(r.active[:i], r.active[i+1:]...)
					break
				}
			}
			r.mu.Unlock()
			route.close()
		})
	}
}

func (r *egressRelay) current() *relayRoute {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed || len(r.active) == 0 {
		return nil
	}
	return r.active[len(r.active)-1]
}

func (r *egressRelay) Close() {
	r.mu.Lock()
	r.closed = true
	routes := r.active
	r.active = nil
	r.mu.Unlock()
	for _, route := range routes {
		route.close()
	}
	_ = r.server.Close()
}

func (route *relayRoute) track(conns ...net.Conn) bool {
	route.mu.Lock()
	defer route.mu.Unlock()
	if route.done {
		return false
	}
	for _, conn := range conns {
		route.conns[conn] = struct{}{}
	}
	return true
}

func (route *relayRoute) untrack(conns ...net.Conn) {
	route.mu.Lock()
	for _, conn := range conns {
		delete(route.conns, conn)
	}
	route.mu.Unlock()
}

func (route *relayRoute) close() {
	route.mu.Lock()
	route.done = true
	conns := route.conns
	route.conns = map[net.Conn]struct{}{}
	route.mu.Unlock()
	route.cancel()
	for conn := range conns {
		_ = conn.Close()
	}
	route.transport.CloseIdleConnections()
}

func (r *egressRelay) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	route := r.current()
	if route == nil {
		r.recordIdle(req)
		http.Error(w, "wscyber: 没有进行中的浏览器调用，后台请求未发送", http.StatusForbidden)
		return
	}
	if req.Method == http.MethodConnect {
		r.tunnel(w, req, route)
		return
	}
	if req.URL == nil || !req.URL.IsAbs() {
		http.Error(w, "wscyber: 出口中继只接受代理请求", http.StatusBadRequest)
		return
	}
	r.forward(w, req, route)
}

// recordIdle 每个“方法 + URL”在一段空闲期内只记一次，避免页面定时轮询刷屏。
func (r *egressRelay) recordIdle(req *http.Request) {
	if r.onIdle == nil {
		return
	}
	key := req.Method + " " + req.Host
	if req.Method != http.MethodConnect && req.URL != nil {
		key = req.Method + " " + req.URL.String()
	}
	r.mu.Lock()
	seen := r.idleSeen[key]
	r.idleSeen[key] = true
	r.mu.Unlock()
	if !seen {
		r.onIdle(req)
	}
}

// tunnel 对 ProxyHub 发同一 CONNECT（补令牌），之后双向透传；ProxyHub 的 200/407 原样回给 Chrome。
func (r *egressRelay) tunnel(w http.ResponseWriter, req *http.Request, route *relayRoute) {
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "wscyber: 中继不支持隧道", http.StatusInternalServerError)
		return
	}
	hub, err := (&net.Dialer{Timeout: 10 * time.Second}).DialContext(route.ctx, "tcp", r.hubAddr)
	if err != nil {
		http.Error(w, "wscyber: 连接出口代理失败", http.StatusBadGateway)
		return
	}
	client, buffered, err := hijacker.Hijack()
	if err != nil {
		_ = hub.Close()
		return
	}
	if !route.track(client, hub) {
		_ = client.Close()
		_ = hub.Close()
		return
	}
	defer route.untrack(client, hub)
	authority := req.Host
	if _, err := fmt.Fprintf(hub, "CONNECT %s HTTP/1.1\r\nHost: %s\r\nProxy-Authorization: %s\r\n\r\n", authority, authority, route.auth); err != nil {
		_ = client.Close()
		_ = hub.Close()
		return
	}
	if n := buffered.Reader.Buffered(); n > 0 {
		pending, _ := buffered.Reader.Peek(n)
		if _, err := hub.Write(pending); err != nil {
			_ = client.Close()
			_ = hub.Close()
			return
		}
	}
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(hub, client)
		if tcp, ok := hub.(*net.TCPConn); ok {
			_ = tcp.CloseWrite()
		}
		close(done)
	}()
	_, _ = io.Copy(client, hub)
	_ = client.Close()
	_ = hub.Close()
	<-done
}

var relayHopHeaders = []string{"Proxy-Connection", "Proxy-Authorization", "Proxy-Authenticate", "Connection", "Keep-Alive", "Te", "Trailer", "Upgrade"}

// forward 处理明文 HTTP（绝对 URI）：经带令牌的 Transport 交给 ProxyHub，响应（含 407）原样写回。
func (r *egressRelay) forward(w http.ResponseWriter, req *http.Request, route *relayRoute) {
	ctx, cancel := context.WithCancel(req.Context())
	defer cancel()
	stop := context.AfterFunc(route.ctx, cancel)
	defer stop()
	out := req.Clone(ctx)
	out.RequestURI = ""
	for _, name := range strings.Split(req.Header.Get("Connection"), ",") {
		if name = strings.TrimSpace(name); name != "" {
			out.Header.Del(name)
		}
	}
	for _, name := range relayHopHeaders {
		out.Header.Del(name)
	}
	if req.ContentLength == 0 {
		out.Body = nil
	}
	response, err := route.transport.RoundTrip(out)
	if err != nil {
		http.Error(w, "wscyber: 出口代理转发失败", http.StatusBadGateway)
		return
	}
	defer response.Body.Close()
	for _, name := range relayHopHeaders {
		response.Header.Del(name)
	}
	for key, values := range response.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	w.WriteHeader(response.StatusCode)
	_, _ = io.Copy(w, response.Body)
}
