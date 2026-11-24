package webapi

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"sync"
	"syscall"
	"time"
)

// 目标只发送叶证书（缺中间证书）时，标准 TLS 校验报 unknown authority。浏览器的做法是按证书
// AIA caIssuers 地址补取中间证书。Go 的 tls 内建校验不接受额外中间证书，所以 directTransport
// 自己完成握手：内建校验关闭，改在 VerifyConnection 中做同等的严格校验——
//   - 信任锚只来自 roots（nil = 本机系统根库），补取的证书只作 untrusted intermediates；
//   - 域名取自拨号地址（不是 SNI：IP 目标不发 SNI），有效期、serverAuth 用途、签名链由 x509.Verify 校验；
//   - 握手签名（CertificateVerify 等）仍由 tls 包用叶证书公钥校验。
// 只在首次校验为 UnknownAuthorityError 时才补取；补链走完仍不可信返回 tls_certificate，补链未完成
// （预算耗尽/单次超时/上层截止）返回 tlsChainIncompleteError（tls_chain_incomplete）；都不重试目标请求。
// 每次实际出网的补取登记 tls.aia_fetch 审计（见 tls_trace.go）。

// 时间预算（directClient.Timeout = httpTimeout = 30s，覆盖拨号到读完响应体）：
//   - TCP 拨号 ≤ directDialTimeout（10s，HTTP 与 HTTPS 共用）；
//   - 整个 DialTLS（TCP 拨号 + TLS 握手 + AIA 补链）≤ tlsDialBudget（20s）：transport 的拨号上下文脱离了
//     请求取消且自定义 DialTLS 不受 TLSHandshakeTimeout 约束，这里自行设上限，并接回本次发送的截止/取消；
//   - 其中 AIA 总耗时 ≤ aiaTotalBudget（8s），单次 ≤ aiaFetchTimeout（5s）且不超过剩余补链预算，
//     并在 DialTLS 截止前留 aiaDeadlineMargin，让 verifier 先于握手上下文返回“补链未完成”。
//
// 因此握手阶段最晚 20s 结束，至少留 10s 给请求发送与响应头；补链超时不会被上层误判为通用 timeout。
const (
	directDialTimeout = 10 * time.Second
	tlsDialBudget     = 20 * time.Second
	aiaTotalBudget    = 8 * time.Second
	aiaFetchTimeout   = 5 * time.Second
	aiaDeadlineMargin = 500 * time.Millisecond
	aiaMaxFetches     = 4        // 单次握手最多补取次数（足够补齐常见 1–2 级缺失链）
	aiaMaxBytes       = 64 << 10 // 单个证书响应上限
	aiaCacheEntries   = 64
)

// aiaFetchFunc 返回响应体与实际跟随的跳转次数；错误用 errAIA* 哨兵区分（见 aiaFetchResult）。
type aiaFetchFunc func(ctx context.Context, rawURL string) ([]byte, int, error)

type tlsChainVerifier struct {
	roots   *x509.CertPool // nil = 系统根库
	fetch   aiaFetchFunc   // AIA 补取
	now     func() time.Time
	dialer  *net.Dialer
	cacheMu sync.Mutex
	cache   map[string]*x509.Certificate // URL → 已解析中间证书；只作 untrusted intermediate，错项不能产生信任
	// 预算，零值 = 上面的常量（测试注入缩小值）。
	dialBudget, aiaBudget, fetchTimeout time.Duration
}

func durationOr(value, fallback time.Duration) time.Duration {
	if value > 0 {
		return value
	}
	return fallback
}

var directDialer = &net.Dialer{Timeout: directDialTimeout}

// dialTLS 供 http.Transport.DialTLSContext 使用：拨号 + 握手 + 严格校验（含 AIA 补链）。
func (v *tlsChainVerifier) dialTLS(ctx context.Context, network, addr string) (net.Conn, error) {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	return v.dialTLSAt(ctx, network, addr, host)
}

// dialTLSAt 连接 addr，并以 host 作为 SNI 与证书域名校验对象。
func (v *tlsChainVerifier) dialTLSAt(ctx context.Context, network, addr, host string) (net.Conn, error) {
	// 握手完成后 tls.Conn 不再引用 ctx，返回时取消不影响已建立的连接。
	ctx, cancel := context.WithTimeout(ctx, durationOr(v.dialBudget, tlsDialBudget))
	defer cancel()
	if trace := tlsTraceFrom(ctx); trace != nil && trace.parent != nil {
		stop := context.AfterFunc(trace.parent, cancel)
		defer stop()
	}
	raw, err := v.dialer.DialContext(ctx, network, addr)
	if err != nil {
		return nil, err
	}
	conn := tls.Client(raw, &tls.Config{
		ServerName: host,
		// 内建校验无法加入补取的中间证书；校验由下面的 VerifyConnection 完整执行，失败即中止握手。
		InsecureSkipVerify: true,
		VerifyConnection: func(cs tls.ConnectionState) error {
			return v.verify(ctx, host, cs.PeerCertificates)
		},
	})
	if err := conn.HandshakeContext(ctx); err != nil {
		raw.Close()
		return nil, err
	}
	return conn, nil
}

func (v *tlsChainVerifier) verify(ctx context.Context, host string, certs []*x509.Certificate) error {
	if len(certs) == 0 {
		return &tls.CertificateVerificationError{Err: errors.New("tls: server sent no certificate")}
	}
	intermediates := x509.NewCertPool()
	for _, cert := range certs[1:] {
		intermediates.AddCert(cert)
	}
	opts := x509.VerifyOptions{
		Roots:         v.roots,
		Intermediates: intermediates,
		DNSName:       host,
		CurrentTime:   v.now(),
		KeyUsages:     []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	_, err := certs[0].Verify(opts)
	var authority x509.UnknownAuthorityError
	if err == nil || !errors.As(err, &authority) {
		return wrapVerification(certs, err)
	}
	// 缺链：沿 AIA 逐级补取。起点为服务端发来的每张证书（叶或残缺链尾）。
	trace := tlsTraceFrom(ctx)
	trace.setPhase(aiaRunning)
	deadline := v.aiaDeadline(ctx)
	incomplete := false
	frontier := append([]*x509.Certificate(nil), certs...)
	seen := map[string]bool{}
fetching:
	for fetches := 0; fetches < aiaMaxFetches && len(frontier) > 0; {
		var next []*x509.Certificate
		for _, cert := range frontier {
			for _, rawURL := range cert.IssuingCertificateURL {
				if seen[rawURL] || fetches >= aiaMaxFetches {
					continue
				}
				fetchDeadline := v.now().Add(durationOr(v.fetchTimeout, aiaFetchTimeout))
				if deadline.Before(fetchDeadline) {
					fetchDeadline = deadline
				}
				if ctx.Err() != nil || !v.now().Before(fetchDeadline) {
					incomplete = true // 补链预算耗尽或上层已截止：不再出网
					break fetching
				}
				seen[rawURL] = true
				fetches++
				issuer, timedOut := v.issuer(ctx, fetchDeadline, host, rawURL, trace)
				incomplete = incomplete || timedOut
				if issuer == nil {
					continue
				}
				intermediates.AddCert(issuer)
				if _, err = certs[0].Verify(opts); err == nil {
					trace.setPhase(aiaFinished)
					return nil
				}
				next = append(next, issuer)
			}
		}
		frontier = next
	}
	if incomplete {
		trace.setPhase(aiaIncomplete)
		return &tlsChainIncompleteError{err: wrapVerification(certs, err)}
	}
	trace.setPhase(aiaFinished)
	return wrapVerification(certs, err)
}

// aiaDeadline：补链总截止 = min(现在 + 补链预算, 握手上下文截止 − 余量)。
func (v *tlsChainVerifier) aiaDeadline(ctx context.Context) time.Time {
	deadline := v.now().Add(durationOr(v.aiaBudget, aiaTotalBudget))
	if outer, ok := ctx.Deadline(); ok && outer.Add(-aiaDeadlineMargin).Before(deadline) {
		deadline = outer.Add(-aiaDeadlineMargin)
	}
	return deadline
}

func wrapVerification(certs []*x509.Certificate, err error) error {
	if err == nil {
		return nil
	}
	return &tls.CertificateVerificationError{UnverifiedCertificates: certs, Err: err}
}

// issuer 取 rawURL 的中间证书：缓存命中不出网、不登记；否则实际获取一次并登记一条 tls.aia_fetch。
// 返回的 timedOut 表示这次获取未在时限内完成（单次超时、补链预算或上层截止）。
func (v *tlsChainVerifier) issuer(ctx context.Context, deadline time.Time, targetHost, rawURL string, trace *tlsSendTrace) (*x509.Certificate, bool) {
	v.cacheMu.Lock()
	cached := v.cache[rawURL]
	v.cacheMu.Unlock()
	if cached != nil {
		return cached, false
	}
	at := v.now()
	fetchCtx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	data, redirects, err := v.fetch(fetchCtx, rawURL)
	result, cert := aiaResultOK, (*x509.Certificate)(nil)
	if err != nil {
		result = aiaFetchResult(fetchCtx, err)
	} else if cert = parseAIACertificate(data); cert == nil {
		result = aiaResultParseFailed
	}
	if trace != nil && trace.record != nil {
		trace.record(aiaAuditEntry(trace, aiaURLHost(rawURL), targetHost, result, len(data), redirects), at)
	}
	if cert == nil {
		return nil, result == aiaResultTimeout
	}
	v.cacheMu.Lock()
	if v.cache == nil || len(v.cache) >= aiaCacheEntries {
		v.cache = map[string]*x509.Certificate{}
	}
	v.cache[rawURL] = cert
	v.cacheMu.Unlock()
	return cert, false
}

// aiaFetchResult 把获取错误归为固定枚举；获取上下文已结束（超时或上层截止/取消）一律记 timeout。
func aiaFetchResult(fetchCtx context.Context, err error) string {
	var networkError net.Error
	switch {
	case errors.Is(err, errAIAAddress):
		return aiaResultRefusedAddress
	case errors.Is(err, errAIAUnsupported):
		return aiaResultUnsupportedURL
	case errors.Is(err, errAIAStatus), errors.Is(err, errAIARedirects):
		return aiaResultHTTPStatus
	case errors.Is(err, errAIATooLarge):
		return aiaResultTooLarge
	case fetchCtx.Err() != nil, errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled),
		errors.As(err, &networkError) && networkError.Timeout():
		return aiaResultTimeout
	}
	return aiaResultNetwork
}

// aiaURLHost 只取发布点 host（含端口），不保留路径/查询/凭据。
func aiaURLHost(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return parsed.Host
}

// parseAIACertificate 接受 DER（RFC 5280 规定的常见形式）或 PEM；PKCS#7 证书包不支持。
func parseAIACertificate(data []byte) *x509.Certificate {
	if block, _ := pem.Decode(data); block != nil && block.Type == "CERTIFICATE" {
		data = block.Bytes
	}
	cert, err := x509.ParseCertificate(data)
	if err != nil {
		return nil
	}
	return cert
}

// AIA 地址来自目标证书，属目标可控输入：只发 GET、只走 http（RFC 5280 caIssuers；https 会循环依赖），
// 不带任何目标数据；连接时校验实际 IP 仅限公网地址（防借补链访问本机/内网，含 DNS 重绑定）。
var aiaClient = newAIAClient(aiaDialControl)

var directTLS = &tlsChainVerifier{fetch: aiaFetcher(aiaClient), now: time.Now, dialer: directDialer}

type aiaRedirectsKey struct{}

// newAIAClient：control 为空只用于测试（允许连本地替身）；最多跟随 2 次 http 跳转（第 3 次拒绝），实际次数计入请求 ctx。
func newAIAClient(control func(network, address string, c syscall.RawConn) error) *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			Proxy: nil, DisableKeepAlives: true, ForceAttemptHTTP2: false,
			DialContext: (&net.Dialer{Timeout: aiaFetchTimeout, Control: control}).DialContext,
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if req.URL.Scheme != "http" || req.URL.User != nil {
				return errAIAUnsupported
			}
			if len(via) >= 3 {
				return errAIARedirects
			}
			if counter, ok := req.Context().Value(aiaRedirectsKey{}).(*int); ok {
				*counter++
			}
			return nil
		},
	}
}

var (
	errAIAAddress     = errors.New("aia: non-public address refused")
	errAIAUnsupported = errors.New("aia: unsupported url")
	errAIAStatus      = errors.New("aia: unexpected status")
	errAIARedirects   = errors.New("aia: too many redirects")
	errAIATooLarge    = errors.New("aia: response too large")
)

func aiaFetcher(client *http.Client) aiaFetchFunc {
	return func(ctx context.Context, rawURL string) ([]byte, int, error) {
		redirects := 0
		data, err := fetchAIAWith(context.WithValue(ctx, aiaRedirectsKey{}, &redirects), client, rawURL)
		return data, redirects, err
	}
}

func fetchAIACertificate(ctx context.Context, rawURL string) ([]byte, error) {
	return fetchAIAWith(ctx, aiaClient, rawURL)
}

func fetchAIAWith(ctx context.Context, client *http.Client, rawURL string) ([]byte, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme != "http" || parsed.Host == "" || parsed.User != nil {
		return nil, errAIAUnsupported
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return nil, errAIAUnsupported
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, errAIAStatus
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, aiaMaxBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > aiaMaxBytes {
		return nil, errAIATooLarge
	}
	return data, nil
}

func aiaDialControl(_, address string, _ syscall.RawConn) error {
	addrPort, err := netip.ParseAddrPort(address)
	if err != nil || !aiaAddressAllowed(addrPort.Addr()) {
		return errAIAAddress
	}
	return nil
}

var aiaBlockedPrefixes = func() []netip.Prefix {
	var prefixes []netip.Prefix
	for _, cidr := range []string{
		"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "198.18.0.0/15",
		"198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4", "64:ff9b::/96", "2001:db8::/32",
	} {
		prefixes = append(prefixes, netip.MustParsePrefix(cidr))
	}
	return prefixes
}()

func aiaAddressAllowed(addr netip.Addr) bool {
	addr = addr.Unmap()
	if !addr.IsValid() || !addr.IsGlobalUnicast() || addr.IsPrivate() || addr.IsLoopback() ||
		addr.IsLinkLocalUnicast() || addr.IsUnspecified() || addr.IsMulticast() {
		return false
	}
	for _, prefix := range aiaBlockedPrefixes {
		if prefix.Contains(addr) {
			return false
		}
	}
	return true
}
