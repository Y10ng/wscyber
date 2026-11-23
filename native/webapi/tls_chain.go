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
// 只在首次校验为 UnknownAuthorityError 时才补取；任何失败都返回原分类（tls_certificate），不重试目标请求。

const (
	aiaMaxFetches   = 4               // 单次握手最多补取次数（足够补齐常见 1–2 级缺失链）
	aiaFetchTimeout = 5 * time.Second // 单次补取超时
	aiaMaxBytes     = 64 << 10        // 单个证书响应上限
	aiaCacheEntries = 64
)

type tlsChainVerifier struct {
	roots   *x509.CertPool                                           // nil = 系统根库
	fetch   func(ctx context.Context, rawURL string) ([]byte, error) // AIA 补取
	now     func() time.Time
	dialer  *net.Dialer
	cacheMu sync.Mutex
	cache   map[string]*x509.Certificate // URL → 已解析中间证书；只作 untrusted intermediate，错项不能产生信任
}

var directDialer = &net.Dialer{Timeout: 15 * time.Second}

var directTLS = &tlsChainVerifier{fetch: fetchAIACertificate, now: time.Now, dialer: directDialer}

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
	frontier := append([]*x509.Certificate(nil), certs...)
	seen := map[string]bool{}
	for fetches := 0; fetches < aiaMaxFetches && len(frontier) > 0; {
		var next []*x509.Certificate
		for _, cert := range frontier {
			for _, rawURL := range cert.IssuingCertificateURL {
				if seen[rawURL] || fetches >= aiaMaxFetches {
					continue
				}
				seen[rawURL] = true
				fetches++
				issuer := v.issuer(ctx, rawURL)
				if issuer == nil {
					continue
				}
				intermediates.AddCert(issuer)
				if _, err = certs[0].Verify(opts); err == nil {
					return nil
				}
				next = append(next, issuer)
			}
		}
		frontier = next
	}
	return wrapVerification(certs, err)
}

func wrapVerification(certs []*x509.Certificate, err error) error {
	if err == nil {
		return nil
	}
	return &tls.CertificateVerificationError{UnverifiedCertificates: certs, Err: err}
}

func (v *tlsChainVerifier) issuer(ctx context.Context, rawURL string) *x509.Certificate {
	v.cacheMu.Lock()
	cached := v.cache[rawURL]
	v.cacheMu.Unlock()
	if cached != nil {
		return cached
	}
	fetchCtx, cancel := context.WithTimeout(ctx, aiaFetchTimeout)
	defer cancel()
	data, err := v.fetch(fetchCtx, rawURL)
	if err != nil {
		return nil
	}
	cert := parseAIACertificate(data)
	if cert == nil {
		return nil
	}
	v.cacheMu.Lock()
	if v.cache == nil || len(v.cache) >= aiaCacheEntries {
		v.cache = map[string]*x509.Certificate{}
	}
	v.cache[rawURL] = cert
	v.cacheMu.Unlock()
	return cert
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
var aiaClient = &http.Client{
	Transport: &http.Transport{
		Proxy: nil, DisableKeepAlives: true, ForceAttemptHTTP2: false,
		DialContext: (&net.Dialer{Timeout: aiaFetchTimeout, Control: aiaDialControl}).DialContext,
	},
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 3 || req.URL.Scheme != "http" {
			return errors.New("aia redirect refused")
		}
		return nil
	},
}

var errAIAAddress = errors.New("aia: non-public address refused")

func fetchAIACertificate(ctx context.Context, rawURL string) ([]byte, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme != "http" || parsed.Host == "" || parsed.User != nil {
		return nil, errors.New("aia: unsupported url")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := aiaClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, errors.New("aia: unexpected status")
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, aiaMaxBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > aiaMaxBytes {
		return nil, errors.New("aia: response too large")
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
