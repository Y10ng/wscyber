package webapi

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// 合成 PKI：root → inter1 → inter2 → leaf（example.test / 127.0.0.1）。全部本地生成，不接触真实目标。
type synthCA struct {
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
}

var synthSerial atomic.Int64

func synthIssue(t *testing.T, parent *synthCA, tmpl *x509.Certificate) *synthCA {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl.SerialNumber = big.NewInt(synthSerial.Add(1))
	if tmpl.NotBefore.IsZero() {
		tmpl.NotBefore = time.Now().Add(-time.Hour)
		tmpl.NotAfter = time.Now().Add(24 * time.Hour)
	}
	signer, signerKey := tmpl, key
	if parent != nil {
		signer, signerKey = parent.cert, parent.key
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, signer, &key.PublicKey, signerKey)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return &synthCA{cert: cert, key: key}
}

func synthCATemplate(name string, aia string) *x509.Certificate {
	tmpl := &x509.Certificate{
		Subject: pkix.Name{CommonName: name}, IsCA: true, BasicConstraintsValid: true,
		KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
	}
	if aia != "" {
		tmpl.IssuingCertificateURL = []string{aia}
	}
	return tmpl
}

type synthChain struct {
	root, inter1, inter2 *synthCA
	roots                *x509.CertPool
}

func newSynthChain(t *testing.T) synthChain {
	root := synthIssue(t, nil, synthCATemplate("Synthetic Root", ""))
	inter1 := synthIssue(t, root, synthCATemplate("Synthetic Intermediate 1", "http://aia.example.test/root.der"))
	inter2 := synthIssue(t, inter1, synthCATemplate("Synthetic Intermediate 2", "http://aia.example.test/inter1.der"))
	roots := x509.NewCertPool()
	roots.AddCert(root.cert)
	return synthChain{root: root, inter1: inter1, inter2: inter2, roots: roots}
}

type leafOptions struct {
	dns     []string
	ip      []net.IP
	eku     []x509.ExtKeyUsage
	aia     []string
	expired bool
}

func (c synthChain) leaf(t *testing.T, opts leafOptions) *synthCA {
	if opts.eku == nil {
		opts.eku = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}
	}
	if opts.aia == nil {
		opts.aia = []string{"http://aia.example.test/inter2.der"}
	}
	tmpl := &x509.Certificate{
		Subject: pkix.Name{CommonName: "leaf"}, DNSNames: opts.dns, IPAddresses: opts.ip,
		ExtKeyUsage: opts.eku, KeyUsage: x509.KeyUsageDigitalSignature, IssuingCertificateURL: opts.aia,
	}
	if opts.expired {
		tmpl.NotBefore, tmpl.NotAfter = time.Now().Add(-48*time.Hour), time.Now().Add(-24*time.Hour)
	}
	return synthIssue(t, c.inter2, tmpl)
}

// 本地 AIA 替身：按 URL 返回 DER，计数每次补取；errs 指定某 URL 返回的错误，hang 为真时挂起直到 ctx 结束。
type aiaStub struct {
	docs    map[string][]byte
	errs    map[string]error
	hang    bool
	fetches atomic.Int32
}

func (s *aiaStub) fetch(ctx context.Context, rawURL string) ([]byte, int, error) {
	s.fetches.Add(1)
	if s.hang {
		<-ctx.Done()
		return nil, 0, ctx.Err()
	}
	if err, ok := s.errs[rawURL]; ok {
		return nil, 0, err
	}
	if data, ok := s.docs[rawURL]; ok {
		return data, 0, nil
	}
	return nil, 0, errors.New("not found")
}

func (c synthChain) stub() *aiaStub {
	return &aiaStub{docs: map[string][]byte{
		"http://aia.example.test/inter2.der": c.inter2.cert.Raw,
		"http://aia.example.test/inter1.der": c.inter1.cert.Raw,
		"http://aia.example.test/root.der":   c.root.cert.Raw,
	}}
}

// startLeafOnlyServer：HTTPS 替身只发送 chain 中给定的证书（默认只有叶证书）。
func startLeafOnlyServer(t *testing.T, key *ecdsa.PrivateKey, chain ...*x509.Certificate) string {
	t.Helper()
	var raw [][]byte
	for _, cert := range chain {
		raw = append(raw, cert.Raw)
	}
	listener, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{{Certificate: raw, PrivateKey: key}}})
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "ok") }), ErrorLog: nil}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close() })
	return listener.Addr().String()
}

// clientFor：与生产相同的握手/校验路径（dialTLSAt），信任锚换成合成根、补取换成本地替身；
// 连接替身监听地址，SNI 与域名校验仍按 host（example.test）。
func clientFor(v *tlsChainVerifier, host, addr string) *http.Client {
	return &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{
		Proxy: nil, DisableKeepAlives: true,
		DialTLSContext: func(ctx context.Context, network, target string) (net.Conn, error) {
			if targetHost, _, _ := net.SplitHostPort(target); targetHost != host {
				return nil, errors.New("unexpected host")
			}
			return v.dialTLSAt(ctx, network, addr, host)
		},
	}}
}

func newTestVerifier(c synthChain, stub *aiaStub) *tlsChainVerifier {
	return &tlsChainVerifier{roots: c.roots, fetch: stub.fetch, now: time.Now, dialer: &net.Dialer{Timeout: 5 * time.Second}}
}

func getVia(client *http.Client, rawURL string) error {
	resp, err := client.Get(rawURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "ok" {
		return errors.New("unexpected body")
	}
	return nil
}

func TestTLSChainLeafOnlyCompletedViaAIA(t *testing.T) {
	c := newSynthChain(t)
	leaf := c.leaf(t, leafOptions{dns: []string{"example.test"}})
	addr := startLeafOnlyServer(t, leaf.key, leaf.cert)
	stub := c.stub()
	v := newTestVerifier(c, stub)
	if err := getVia(clientFor(v, "example.test", addr), "https://example.test/"); err != nil {
		t.Fatalf("leaf-only server not completed via AIA: %v", err)
	}
	if got := stub.fetches.Load(); got != 2 {
		t.Fatalf("fetches = %d, want 2 (inter2, inter1)", got)
	}
	// 第二次走缓存，不再补取。
	if err := getVia(clientFor(v, "example.test", addr), "https://example.test/"); err != nil || stub.fetches.Load() != 2 {
		t.Fatalf("cache not used: err=%v fetches=%d", err, stub.fetches.Load())
	}
}

func TestTLSChainPartialChainCompleted(t *testing.T) {
	c := newSynthChain(t)
	leaf := c.leaf(t, leafOptions{dns: []string{"example.test"}})
	addr := startLeafOnlyServer(t, leaf.key, leaf.cert, c.inter2.cert)
	stub := c.stub()
	if err := getVia(clientFor(newTestVerifier(c, stub), "example.test", addr), "https://example.test/"); err != nil {
		t.Fatalf("partial chain not completed: %v", err)
	}
}

func TestTLSChainFullChainNoFetch(t *testing.T) {
	c := newSynthChain(t)
	leaf := c.leaf(t, leafOptions{dns: []string{"example.test"}})
	addr := startLeafOnlyServer(t, leaf.key, leaf.cert, c.inter2.cert, c.inter1.cert)
	stub := c.stub()
	if err := getVia(clientFor(newTestVerifier(c, stub), "example.test", addr), "https://example.test/"); err != nil || stub.fetches.Load() != 0 {
		t.Fatalf("complete chain: err=%v fetches=%d", err, stub.fetches.Load())
	}
}

func TestTLSChainIPTargetVerifiesIPSAN(t *testing.T) {
	c := newSynthChain(t)
	leaf := c.leaf(t, leafOptions{ip: []net.IP{net.ParseIP("127.0.0.1")}})
	addr := startLeafOnlyServer(t, leaf.key, leaf.cert)
	v := newTestVerifier(c, c.stub())
	client := &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{Proxy: nil, DisableKeepAlives: true, DialTLSContext: v.dialTLS}}
	if err := getVia(client, "https://"+addr+"/"); err != nil {
		t.Fatalf("IP target: %v", err)
	}
}

func TestTLSChainStrictFailures(t *testing.T) {
	c := newSynthChain(t)
	other := newSynthChain(t) // 不受信任的另一套 PKI
	cases := []struct {
		name        string
		leaf        func() *synthCA
		stub        func() *aiaStub
		wantFetches int32 // -1 = 不检查
	}{
		{"aia_unavailable", func() *synthCA { return c.leaf(t, leafOptions{dns: []string{"example.test"}}) },
			func() *aiaStub { return &aiaStub{docs: map[string][]byte{}} }, 1},
		{"no_aia_url", func() *synthCA { return c.leaf(t, leafOptions{dns: []string{"example.test"}, aia: []string{}}) },
			c.stub, 0},
		{"hostname_mismatch_no_fetch", func() *synthCA { return c.leaf(t, leafOptions{dns: []string{"other.example.test"}}) },
			c.stub, -1},
		{"expired", func() *synthCA { return c.leaf(t, leafOptions{dns: []string{"example.test"}, expired: true}) },
			c.stub, -1},
		// Go 在建链后才查 EKU：会先补链，补齐后仍因用途不符失败。
		{"client_auth_only_eku", func() *synthCA {
			return c.leaf(t, leafOptions{dns: []string{"example.test"}, eku: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}})
		}, c.stub, -1},
		// 叶证书由不受信 PKI 签发：补取得到的全套链（含其“根”）只作 intermediate，不能成为信任锚。
		{"untrusted_chain_fetched", func() *synthCA { return other.leaf(t, leafOptions{dns: []string{"example.test"}}) },
			other.stub, 3},
		// AIA 自环：补取次数有上限。
		{"aia_loop_bounded", func() *synthCA {
			return c.leaf(t, leafOptions{dns: []string{"example.test"}, aia: []string{"http://aia.example.test/a", "http://aia.example.test/b",
				"http://aia.example.test/c", "http://aia.example.test/d", "http://aia.example.test/e", "http://aia.example.test/f"}})
		}, func() *aiaStub { return &aiaStub{docs: map[string][]byte{}} }, aiaMaxFetches},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			leaf := tc.leaf()
			addr := startLeafOnlyServer(t, leaf.key, leaf.cert)
			stub := tc.stub()
			err := getVia(clientFor(newTestVerifier(c, stub), "example.test", addr), "https://example.test/")
			if err == nil {
				t.Fatal("verification unexpectedly passed")
			}
			if reason := httpFailureReason(err, false); !strings.Contains(reason, "tls_certificate：TLS证书校验失败") {
				t.Fatalf("classification = %q", reason)
			}
			if tc.wantFetches >= 0 && stub.fetches.Load() != tc.wantFetches {
				t.Fatalf("fetches = %d, want %d", stub.fetches.Load(), tc.wantFetches)
			}
			if strings.HasPrefix(tc.name, "hostname") || tc.name == "expired" {
				if stub.fetches.Load() != 0 {
					t.Fatalf("non-authority failure triggered %d fetches", stub.fetches.Load())
				}
			}
		})
	}
}

func TestTLSChainDirectTransportUsesSystemRoots(t *testing.T) {
	// 生产路径：系统根库（roots=nil）不信任合成根，即使服务端发全链也必须失败。
	if directTLS.roots != nil || directTransport.DialTLSContext == nil || directTransport.TLSClientConfig != nil {
		t.Fatal("directTransport must verify against system roots via directTLS")
	}
	c := newSynthChain(t)
	leaf := c.leaf(t, leafOptions{ip: []net.IP{net.ParseIP("127.0.0.1")}})
	addr := startLeafOnlyServer(t, leaf.key, leaf.cert, c.inter2.cert, c.inter1.cert)
	err := getVia(&http.Client{Timeout: 10 * time.Second, Transport: directTransport}, "https://"+addr+"/")
	if err == nil || !strings.Contains(httpFailureReason(err, false), "tls_certificate") {
		t.Fatalf("synthetic root trusted by production transport: %v", err)
	}
}

func TestAIAFetchRefusesNonPublicAddresses(t *testing.T) {
	for _, raw := range []string{"127.0.0.1", "10.1.2.3", "192.168.1.2", "172.16.0.1", "169.254.169.254", "100.64.0.1", "::1", "fe80::1", "fc00::1", "0.0.0.0", "::ffff:127.0.0.1"} {
		if aiaAddressAllowed(netip.MustParseAddr(raw)) {
			t.Errorf("%s allowed", raw)
		}
	}
	for _, raw := range []string{"8.8.8.8", "2606:4700::1"} {
		if !aiaAddressAllowed(netip.MustParseAddr(raw)) {
			t.Errorf("%s refused", raw)
		}
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var hits atomic.Int32
	server := &http.Server{Handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits.Add(1) })}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close() })
	for _, raw := range []string{"http://" + listener.Addr().String() + "/ca.der", "https://example.test/ca.der", "ldap://example.test/ca", "http://user:pw@example.test/ca"} {
		if _, err := fetchAIACertificate(t.Context(), raw); err == nil {
			t.Errorf("%s fetched", raw)
		}
	}
	if hits.Load() != 0 {
		t.Fatal("AIA fetch reached loopback")
	}
}

func TestParseAIACertificateFormats(t *testing.T) {
	c := newSynthChain(t)
	pemData := "-----BEGIN CERTIFICATE-----\n" + base64Encode(c.inter1.cert.Raw) + "\n-----END CERTIFICATE-----\n"
	if parseAIACertificate(c.inter1.cert.Raw) == nil || parseAIACertificate([]byte(pemData)) == nil || parseAIACertificate([]byte("junk")) != nil {
		t.Fatal("AIA certificate parsing")
	}
}
