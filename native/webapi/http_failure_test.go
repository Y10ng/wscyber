package webapi

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"syscall"
	"testing"

	"github.com/chainreactors/cyber/aop/traffic"
	coretool "github.com/chainreactors/cyber/core/tool"
	"google.golang.org/protobuf/proto"
)

// Error must not be called even for an unrecognized error implementation.
type opaqueHTTPFailure struct{}

func (opaqueHTTPFailure) Error() string { panic("must not format transport errors") }

func TestHTTPFailureReasonClassification(t *testing.T) {
	const private = "SYNTHETIC_PRIVATE_MARKER"
	cert := &x509.Certificate{DNSNames: []string{"private.example.test"}}
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"canceled", context.Canceled, "context_canceled：上下文取消"},
		{"deadline", context.DeadlineExceeded, "timeout：超时"},
		{"socket_timeout", &net.OpError{Op: "read", Net: "tcp", Err: os.ErrDeadlineExceeded}, "timeout：超时"},
		{"dns_timeout", &net.DNSError{Err: private, Name: "private.example.test", IsTimeout: true}, "timeout：超时"},
		{"dns", &net.DNSError{Err: private, Name: "private.example.test", Server: "192.0.2.1", IsNotFound: true}, "dns：DNS解析失败"},
		{"refused", &net.OpError{Op: "dial", Net: "tcp", Err: &os.SyscallError{Syscall: "connect", Err: syscall.ECONNREFUSED}}, "connection_refused：连接被拒绝"},
		{"tls_wrapper", &tls.CertificateVerificationError{Err: errors.New(private)}, "tls_certificate：TLS证书校验失败"},
		{"tls_authority", x509.UnknownAuthorityError{Cert: cert}, "tls_certificate：TLS证书校验失败"},
		{"tls_hostname", x509.HostnameError{Certificate: cert, Host: "private.example.test"}, "tls_certificate：TLS证书校验失败"},
		{"tls_expiry", x509.CertificateInvalidError{Cert: cert, Reason: x509.Expired, Detail: private}, "tls_certificate：TLS证书校验失败"},
		// 补链未完成优先于 timeout 与 tls_certificate（内层可能是超时或证书校验错误）。
		{"tls_chain_incomplete_timeout", &tlsChainIncompleteError{err: context.DeadlineExceeded}, "tls_chain_incomplete：证书链补全未完成"},
		{"tls_chain_incomplete_verify", &tlsChainIncompleteError{err: &tls.CertificateVerificationError{Err: errors.New(private)}}, "tls_chain_incomplete：证书链补全未完成"},
		{"eof", io.EOF, "connection_interrupted：连接中断或EOF"},
		{"short_body", io.ErrUnexpectedEOF, "connection_interrupted：连接中断或EOF"},
		{"closed", net.ErrClosed, "connection_interrupted：连接中断或EOF"},
		{"reset", syscall.ECONNRESET, "connection_interrupted：连接中断或EOF"},
		{"aborted", syscall.ECONNABORTED, "connection_interrupted：连接中断或EOF"},
		{"pipe", syscall.EPIPE, "connection_interrupted：连接中断或EOF"},
		{"other", errors.New(private + " response body"), "transport_other：其它传输错误"},
		{"no_string_matching", errors.New("context canceled timeout EOF connection refused " + private), "transport_other：其它传输错误"},
		{"opaque", opaqueHTTPFailure{}, "transport_other：其它传输错误"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, wrapped := range []bool{false, true} {
				err := tc.err
				if wrapped {
					err = &url.Error{Op: private, URL: "https://fixture-user:" + private + "@private.example.test/?token=" + private, Err: err}
					err = errors.Join(err, errors.New(private))
				}
				for _, body := range []bool{false, true} {
					stage := "发送或接收响应头失败"
					if body {
						stage = "响应体读取失败"
					}
					want := stage + "（" + tc.want + "），目标结果未知；不自动重试"
					if got := httpFailureReason(err, body); got != want {
						t.Errorf("wrapped=%v body=%v: got %q, want %q", wrapped, body, got, want)
					}
				}
			}
		})
	}
}

// Both failures use only a loopback fixture and an isolated testdb. The server
// accepts the request before disconnecting, so a Do error must not imply unsent.
func TestWebAPIHTTPFailureReasonPersistence(t *testing.T) {
	for _, bodyFailure := range []bool{false, true} {
		stage := "发送或接收响应头失败"
		if bodyFailure {
			stage = "响应体读取失败"
		}
		t.Run(stage, func(t *testing.T) {
			capability, tool, task, inv, count, _ := newHTTPToolFixture(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				conn, rw, err := w.(http.Hijacker).Hijack()
				if err != nil {
					t.Error(err)
					return
				}
				defer conn.Close()
				if bodyFailure {
					// A short Content-Length body deterministically produces unexpected EOF.
					_, _ = rw.WriteString("HTTP/1.1 200 OK\r\nContent-Length: 20\r\nContent-Type: text/plain\r\n\r\npartial")
					_ = rw.Flush()
				}
			}))
			const path = "/fixture?token=SYNTHETIC_PRIVATE_MARKER"
			result, item := executeHTTP(t, tool, inv, "POST", task.Origins[0]+path, "fixture", "")
			if !result.IsError || !result.Terminate || item.Status != requestUnknown || count.Load() != 1 {
				t.Fatalf("failure semantics changed: %+v sends=%d", item, count.Load())
			}
			model, err := capability.store.evidenceByID(t.Context(), task.ID, item.EvidenceID)
			if err != nil || model == nil {
				t.Fatalf("missing evidence: %v", err)
			}
			want := stage + "（connection_interrupted：连接中断或EOF），目标结果未知；不自动重试"
			if model.Reason != want {
				t.Errorf("reason = %q, want %q", model.Reason, want)
			}
			if !strings.Contains(coretool.ResultText(result), want) || evidenceMetadata(*model).Reason != want {
				t.Error("tool/API metadata did not retain safe reason")
			}
			if strings.Contains(model.Reason, "SYNTHETIC_PRIVATE_MARKER") || strings.Contains(model.Reason, task.Origins[0]) {
				t.Error("reason leaked fixture URL")
			}
			if model.Status != requestUnknown || model.CompletedAt.String == "" || model.StatusCode.Valid != bodyFailure || model.Sent.Valid != bodyFailure {
				t.Fatalf("persisted unknown/sent semantics changed: %+v", model)
			}
			if bodyFailure && (!model.Sent.Bool || model.StatusCode.Int64 != 200 || item.Sent == nil || !*item.Sent || item.HTTPStatus != 200) {
				t.Fatal("received headers must preserve sent=true and HTTP status")
			}
			if !bodyFailure && (item.Sent != nil || item.HTTPStatus != 0) {
				t.Fatal("Do error gained an invented send/response fact")
			}
			var flow traffic.Flow
			if err := proto.Unmarshal(model.FlowPB, &flow); err != nil || flow.GetComplete() {
				t.Fatalf("failure evidence marked complete: %v", err)
			}
			if bodyFailure && string(flow.GetResponse().GetBody()) != "partial" {
				t.Fatal("partial response evidence lost")
			}
			blocked, err := capability.store.unresolvedTurn(t.Context(), inv.SessionID, inv.TurnID)
			if err != nil || !blocked {
				t.Fatalf("unknown no longer blocks next model request: blocked=%v err=%v", blocked, err)
			}
			for _, callID := range []string{inv.CallID, "new-call-same-request"} {
				inv.CallID = callID
				replayResult, replay := executeHTTP(t, tool, inv, "POST", task.Origins[0]+path, "fixture", "")
				if !replayResult.IsError || replay.EvidenceID != item.EvidenceID || count.Load() != 1 {
					t.Fatal("unresolved request was replayed")
				}
			}
		})
	}
}
