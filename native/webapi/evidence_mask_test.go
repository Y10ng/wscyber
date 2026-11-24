package webapi

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chainreactors/cyber/aop/traffic"
	"github.com/uptrace/bun"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

const evidenceLongSecret = "tiny-long-synthetic"

func evidenceMaskFixture(t *testing.T) *authzFixture {
	t.Helper()
	f := newAuthzFixture(t)
	for id, secret := range map[string]string{"tiny": evidenceLongSecret, "short": "tiny", "one": "!"} {
		status, _, raw := f.do("PUT", "/api/webapi/assets/a1/credentials/"+id, mustToolArgumentsJSON(t, map[string]string{"secret": secret}))
		if status != 200 {
			t.Fatalf("credential: %d %s", status, raw)
		}
	}
	return f
}

func insertMaskEvidence(t *testing.T, f *assetFixture, taskID, id, reason string, flow *traffic.Flow, preview []byte) *requestModel {
	t.Helper()
	pb, err := proto.Marshal(flow)
	if err != nil {
		t.Fatal(err)
	}
	model := &requestModel{ID: id, TaskID: taskID, SessionID: "mask-session", TurnID: id, CallID: id, Fingerprint: id,
		Method: "POST", URL: flow.GetRequest().GetUrl(), Reason: reason, Status: requestCompleted,
		Sent: sql.NullBool{Bool: true, Valid: true}, StatusCode: sql.NullInt64{Int64: 200, Valid: true},
		FlowPB: pb, Preview: preview, CreatedAt: "2026-11-21T00:00:00Z"}
	if err := f.capability.store.write(t.Context(), nil, func(ctx context.Context, tx bun.Tx) error {
		_, err := tx.NewInsert().Model(model).Exec(ctx)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return model
}

func decodeMaskedDetail(t *testing.T, raw string) (*traffic.Flow, map[string]json.RawMessage) {
	t.Helper()
	var detail map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &detail); err != nil {
		t.Fatal(err)
	}
	var flow traffic.Flow
	if err := protojson.Unmarshal(detail["flow"], &flow); err != nil {
		t.Fatal(err)
	}
	return &flow, detail
}

func TestEvidenceMaskSecretsDetailListAndImage(t *testing.T) {
	f := evidenceMaskFixture(t)
	long, short, one := credentialPlaceholder("tiny", "secret"), credentialPlaceholder("short", "secret"), credentialPlaceholder("one", "secret")
	flow := &traffic.Flow{
		Request: &traffic.HttpRequest{Method: "POST", Url: "https://example.test/login?password=" + evidenceLongSecret,
			Headers: []*traffic.Header{{Name: "X-Test", Value: evidenceLongSecret + "|tiny"}},
			Body:    []byte(evidenceLongSecret + "|tiny|!")},
		Response: &traffic.HttpResponse{StatusCode: 200, ReasonPhrase: "tiny",
			Headers: []*traffic.Header{{Name: "X-Result", Value: evidenceLongSecret}}, Body: []byte(evidenceLongSecret + "|tiny")},
		Error: evidenceLongSecret,
	}
	model := insertMaskEvidence(t, f.assetFixture, "t1", "we_mask", "failure tiny", flow, []byte(evidenceLongSecret+"|tiny"))
	base := "/api/webapi/tasks/t1/evidence/" + model.ID
	status, _, original := f.do("GET", base, "")
	if status != 200 {
		t.Fatalf("original: %d %s", status, original)
	}
	_, _, unmasked := f.do("GET", base+"?mask=0", "")
	if original != unmasked {
		t.Fatal("mask=0 differs from default")
	}
	_, originalDetail := decodeMaskedDetail(t, original)
	if _, ok := originalDetail["masked_secrets"]; ok {
		t.Fatal("default unexpectedly includes masked_secrets")
	}
	var originalPB string
	_ = json.Unmarshal(originalDetail["flow_protobuf_base64"], &originalPB)
	if originalPB != base64Encode(model.FlowPB) {
		t.Fatal("default original protobuf changed")
	}
	status, data, raw := f.do("GET", base+"?mask=1", "")
	if status != 200 || data["masked_secrets"] != float64(15) {
		t.Fatalf("masked: %d %s", status, raw)
	}
	maskedFlow, detail := decodeMaskedDetail(t, raw)
	if _, exists := detail["flow_protobuf_base64"]; exists {
		t.Fatal("masked response includes original protobuf")
	}
	if strings.Contains(raw, evidenceLongSecret) || strings.Contains(raw, "failure tiny") {
		t.Fatalf("secret in masked response: %s", raw)
	}
	if got := string(maskedFlow.Request.Body); got != long+"|"+short+"|"+one {
		t.Fatalf("body = %q", got)
	}
	if maskedFlow.Request.Url != "https://example.test/login?password="+long || maskedFlow.Request.Headers[0].Value != long+"|"+short ||
		string(maskedFlow.Response.Body) != long+"|"+short || maskedFlow.Response.Headers[0].Value != long || maskedFlow.Error != long || maskedFlow.Response.ReasonPhrase != short {
		t.Fatalf("flow not fully masked: %v", maskedFlow)
	}
	meta := data["metadata"].(map[string]any)
	if meta["url"] != maskedFlow.Request.Url || meta["reason"] != "failure "+short || data["preview"] != long+"|"+short {
		t.Fatalf("metadata/preview: %s", raw)
	}
	listBase := "/api/webapi/tasks/t1/evidence"
	_, _, defaultList := f.do("GET", listBase, "")
	_, _, zeroList := f.do("GET", listBase+"?mask=0", "")
	if defaultList != zeroList {
		t.Fatal("list mask=0 differs from default")
	}
	status, list, raw := f.do("GET", listBase+"?mask=1&limit=1", "")
	if status != 200 || list["masked_secrets"] != float64(2) {
		t.Fatalf("list: %d %s", status, raw)
	}
	item := list["items"].([]any)[0].(map[string]any)
	if item["url"] != meta["url"] || item["reason"] != meta["reason"] {
		t.Fatalf("list fields: %s", raw)
	}

	// 与手工构造的遮盖后 Flow 像素逐字节比较，验证 HTTP 图片确实使用遮盖副本。
	imageMeta := requestEvidenceImageMeta(*model)
	imageMeta.Reason, imageMeta.SecretsMasked = "failure "+short, true
	expected, err := renderEvidenceImage(maskedFlow, imageMeta, evidenceImageOptions{})
	if err != nil {
		t.Fatal(err)
	}
	layout := testLayout(t, maskedFlow, imageMeta, evidenceImageOptions{})
	layoutJSON, _ := json.Marshal(layout)
	if strings.Contains(string(layoutJSON), evidenceLongSecret) || !layout.Masked {
		t.Fatal("image layout leaked secret or omitted mask indicator")
	}
	for _, query := range []string{"", "?mask=1", "?mask=0"} {
		want := expected
		if query == "?mask=0" {
			want, err = renderEvidenceImage(flow, requestEvidenceImageMeta(*model), evidenceImageOptions{NoMask: true})
			if err != nil {
				t.Fatal(err)
			}
		}
		rec := httptest.NewRecorder()
		f.mux.ServeHTTP(rec, httptest.NewRequest("GET", base+"/image.png"+query, nil))
		if rec.Code != 200 || !bytes.Equal(rec.Body.Bytes(), want) {
			t.Fatalf("image %q status=%d differs from expected pixels", query, rec.Code)
		}
	}
	_, _, again := f.do("GET", base, "")
	if again != original {
		t.Fatal("masking mutated original evidence")
	}
	stored, err := f.capability.store.evidenceByID(t.Context(), "t1", model.ID)
	if err != nil || !bytes.Equal(stored.FlowPB, model.FlowPB) || !bytes.Equal(stored.Preview, model.Preview) {
		t.Fatal("stored evidence changed")
	}

	// 无法读取口令时 fail closed；不影响显式取原件。
	f.capability.secrets = nil
	for _, path := range []string{base + "?mask=1", listBase + "?mask=1", base + "/image.png"} {
		rec := httptest.NewRecorder()
		f.mux.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		if rec.Code != 500 || strings.Contains(rec.Body.String(), evidenceLongSecret) {
			t.Fatalf("missing key %s: %d %s", path, rec.Code, rec.Body.String())
		}
	}
	if status, _, raw := f.do("GET", base+"?mask=0", ""); status != 200 || raw != original {
		t.Fatal("unmasked evidence depends on credential key")
	}
}

func TestEvidenceMaskBinaryAndNoAsset(t *testing.T) {
	f := evidenceMaskFixture(t)
	body := append([]byte{0xff, 0}, []byte(evidenceLongSecret+"|tiny")...)
	flow := &traffic.Flow{Request: &traffic.HttpRequest{Method: "POST", Url: "https://example.test/", Body: body},
		Response: &traffic.HttpResponse{StatusCode: 200, Body: body}}
	preview, _ := responsePreview(body, "application/octet-stream", false)
	insertMaskEvidence(t, f.assetFixture, "t1", "we_binary_mask", "", flow, preview)
	status, data, raw := f.do("GET", "/api/webapi/tasks/t1/evidence/we_binary_mask?mask=1", "")
	if status != 200 || data["masked_secrets"] != float64(6) {
		t.Fatalf("binary: %d %s", status, raw)
	}
	masked, _ := decodeMaskedDetail(t, raw)
	want := append([]byte{0xff, 0}, []byte(credentialPlaceholder("tiny", "secret")+"|"+credentialPlaceholder("short", "secret"))...)
	decodedPreview, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(data["preview"].(string), "base64:"))
	if err != nil || !bytes.Equal(masked.Request.Body, want) || !bytes.Equal(masked.Response.Body, want) || !bytes.Equal(decodedPreview, want) {
		t.Fatal("binary bytes not redacted before base64 encoding")
	}
	if status, _, raw := f.do("PUT", "/api/webapi/tasks/no-asset", validBody("synthetic", "test", `"https://example.test"`)); status != 201 {
		t.Fatalf("task: %d %s", status, raw)
	}
	insertMaskEvidence(t, f.assetFixture, "no-asset", "we_no_asset", "", flow, preview)
	for _, path := range []string{"/api/webapi/tasks/no-asset/evidence?mask=1", "/api/webapi/tasks/no-asset/evidence/we_no_asset?mask=1"} {
		status, data, raw := f.do("GET", path, "")
		if status != 200 || data["masked_secrets"] != float64(0) {
			t.Fatalf("no asset: %d %s", status, raw)
		}
	}
}

func TestCredentialRedactCountProtectsPlaceholders(t *testing.T) {
	resolver := &credentialResolver{redactions: []credentialRedaction{
		{secret: evidenceLongSecret, placeholder: credentialPlaceholder("tiny", "secret")},
		{secret: "tiny", placeholder: credentialPlaceholder("short", "secret")},
	}}
	input := evidenceLongSecret + "|tiny|{{cred:tiny.secret}}"
	want := "{{cred:tiny.secret}}|{{cred:short.secret}}|{{cred:tiny.secret}}"
	if got, count := resolver.redactCount(input); got != want || count != 2 {
		t.Fatalf("redact = %q, %d", got, count)
	}
	if got, count := resolver.redactCount(want); got != want || count != 0 {
		t.Fatalf("second redact = %q, %d", got, count)
	}
}
