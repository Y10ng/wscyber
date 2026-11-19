package webapi

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"image/png"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chainreactors/cyber/aop/traffic"
	"google.golang.org/protobuf/proto"
)

// 全部为 example.test 合成 Flow，不含真实目标或载荷。

func evidenceImageJSONFlow() *traffic.Flow {
	return &traffic.Flow{
		Id: "we_fixture_json", Complete: true,
		Request: &traffic.HttpRequest{Method: "POST", Url: "https://api.example.test/v1/orders?page=2", Protocol: "HTTP/1.1",
			Headers: []*traffic.Header{{Name: "Host", Value: "api.example.test"}, {Name: "Content-Type", Value: "application/json"}, {Name: "Authorization", Value: "Bearer demo-token-0123456789"}, {Name: "Cookie", Value: "sid=fixture-session-abcdef"}},
			Body:    []byte(`{"item":"widget","qty":2}`)},
		Response: &traffic.HttpResponse{StatusCode: 200, ReasonPhrase: "OK",
			Headers: []*traffic.Header{{Name: "Content-Type", Value: "application/json; charset=utf-8"}, {Name: "Set-Cookie", Value: "sid=rotated-session-value; HttpOnly"}},
			Body:    []byte("{\"order_id\":\"ord-1001\",\"status\":\"accepted\",\"owner\":\"user-42\"}\n")},
	}
}

func evidenceImageChineseFlow() *traffic.Flow {
	return &traffic.Flow{
		Id: "we_fixture_cn", Complete: true,
		Request: &traffic.HttpRequest{Method: "GET", Url: "https://shop.example.test/api/profile?id=42", Protocol: "HTTP/1.1",
			Headers: []*traffic.Header{{Name: "Host", Value: "shop.example.test"}, {Name: "Accept", Value: "application/json"}}},
		Response: &traffic.HttpResponse{StatusCode: 200, ReasonPhrase: "OK",
			Headers: []*traffic.Header{{Name: "Content-Type", Value: "application/json; charset=utf-8"}},
			Body:    []byte("{\n  \"用户名\": \"测试用户甲\",\n  \"手机号\": \"138****0000\",\n  \"地址\": \"示例省示例市示例路 1 号\",\n  \"备注\": \"这是一段用于验证中文渲染与折行的较长说明文字，应当在栏宽处按字符折行而不重叠，并保持行号只在首行出现。\"\n}\n")},
	}
}

func evidenceImageLongFlow(lines int) *traffic.Flow {
	var body strings.Builder
	for i := 1; i <= lines; i++ {
		fmt.Fprintf(&body, "{\"row\":%d,\"name\":\"item-%03d\",\"note\":\"fixture\"}\n", i, i)
	}
	return &traffic.Flow{
		Id: "we_fixture_long", Complete: false,
		Request: &traffic.HttpRequest{Method: "GET", Url: "https://api.example.test/v1/items", Protocol: "HTTP/1.1",
			Headers: []*traffic.Header{{Name: "Host", Value: "api.example.test"}}},
		Response: &traffic.HttpResponse{StatusCode: 200, ReasonPhrase: "OK",
			Headers: []*traffic.Header{{Name: "Content-Type", Value: "application/x-ndjson"}}, Body: []byte(body.String())},
	}
}

func layoutTexts(column evidenceColumn) []string {
	texts := make([]string, 0, len(column.Rows))
	for _, row := range column.Rows {
		texts = append(texts, row.Text)
	}
	return texts
}

func findRow(t *testing.T, column evidenceColumn, prefix string) evidenceRow {
	t.Helper()
	for _, row := range column.Rows {
		if strings.HasPrefix(row.Text, prefix) {
			return row
		}
	}
	t.Fatalf("%s 栏缺少以 %q 开头的行：%q", column.Title, prefix, layoutTexts(column))
	return evidenceRow{}
}

func testLayout(t *testing.T, flow *traffic.Flow, meta evidenceImageMeta, opts evidenceImageOptions) *evidenceLayout {
	t.Helper()
	fonts, err := loadEvidenceFonts()
	if err != nil {
		t.Fatal(err)
	}
	layout, err := layoutEvidenceLines(flow, meta, opts, evidenceTextCells(fonts.cellWidth))
	if err != nil {
		t.Fatal(err)
	}
	return layout
}

func renderAndCheck(t *testing.T, flow *traffic.Flow, meta evidenceImageMeta, opts evidenceImageOptions) []byte {
	t.Helper()
	first, err := renderEvidenceImage(flow, meta, opts)
	if err != nil {
		t.Fatal(err)
	}
	second, err := renderEvidenceImage(flow, meta, opts)
	if err != nil {
		t.Fatal(err)
	}
	if sha256.Sum256(first) != sha256.Sum256(second) {
		t.Fatal("同一 Flow 同一参数两次渲染字节不同")
	}
	img, err := png.Decode(bytes.NewReader(first))
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds().Dx() != evidenceImageWidth || img.Bounds().Dy() < 100 {
		t.Fatalf("尺寸=%v", img.Bounds())
	}
	return first
}

func TestEvidenceImageLayoutJSONMaskAndHighlight(t *testing.T) {
	flow := evidenceImageJSONFlow()
	original := proto.Clone(flow).(*traffic.Flow)
	meta := evidenceImageMeta{ID: "we_fixture_json", CreatedAt: "2026-11-19T05:00:00.123Z", Status: requestCompleted}
	layout := testLayout(t, flow, meta, evidenceImageOptions{Marks: []string{"ord-1001"}})

	if layout.Header != "POST https://api.example.test/v1/orders?page=2" || layout.HeaderRight != "证据 we_fixture_json" {
		t.Fatalf("header=%q right=%q", layout.Header, layout.HeaderRight)
	}
	first := layout.Request.Rows[0]
	if first.Text != "POST /v1/orders?page=2 HTTP/1.1" || !first.Highlight || first.Number != "1" {
		t.Fatalf("请求行=%+v", first)
	}
	if status := layout.Response.Rows[0]; status.Text != "HTTP/1.1 200 OK" || !status.Highlight {
		t.Fatalf("状态行=%+v", status)
	}
	if row := findRow(t, layout.Request, "Authorization:"); row.Text != "Authorization: Bearer"+evidenceMaskSuffix || row.Highlight {
		t.Fatalf("Authorization 遮盖=%q", row.Text)
	}
	if row := findRow(t, layout.Request, "Cookie:"); row.Text != "Cookie: sid=fi"+evidenceMaskSuffix {
		t.Fatalf("Cookie 遮盖=%q", row.Text)
	}
	if row := findRow(t, layout.Response, "Set-Cookie:"); row.Text != "Set-Cookie: sid=ro"+evidenceMaskSuffix {
		t.Fatalf("Set-Cookie 遮盖=%q", row.Text)
	}
	// JSON 不重新格式化。
	findRow(t, layout.Request, `{"item":"widget","qty":2}`)
	body := findRow(t, layout.Response, `{"order_id":"ord-1001"`)
	if !body.Highlight || len(body.Marks) != 1 || []rune(body.Text)[body.Marks[0].Start] != 'o' || body.Marks[0].End-body.Marks[0].Start != len("ord-1001") {
		t.Fatalf("mark 高亮=%+v", body)
	}
	if row := findRow(t, layout.Response, "Content-Type:"); row.Highlight {
		t.Fatal("未命中 mark 的行不应高亮")
	}
	footer := strings.Join(layout.Footer, " · ")
	if footer != "wscyber · 证据 we_fixture_json · 2026-11-19 05:00:00 UTC · 已遮盖敏感头" {
		t.Fatalf("footer=%q", footer)
	}
	if !proto.Equal(flow, original) {
		t.Fatal("遮盖改动了原件")
	}

	unmasked := testLayout(t, flow, meta, evidenceImageOptions{NoMask: true})
	if row := findRow(t, unmasked.Request, "Authorization:"); row.Text != "Authorization: Bearer demo-token-0123456789" {
		t.Fatalf("mask=0 仍遮盖：%q", row.Text)
	}
	if unmasked.Masked || !strings.Contains(strings.Join(unmasked.Footer, " "), "敏感头未遮盖") {
		t.Fatalf("mask=0 footer=%q", unmasked.Footer)
	}
	if maskEvidenceValue("abc") != "a"+evidenceMaskSuffix || maskEvidenceValue("") != evidenceMaskSuffix {
		t.Fatal("短值遮盖不应暴露全文")
	}
	renderAndCheck(t, flow, meta, evidenceImageOptions{Marks: []string{"ord-1001"}})
}

func TestEvidenceImageChineseWrapAndLongLines(t *testing.T) {
	flow := evidenceImageChineseFlow()
	layout := testLayout(t, flow, evidenceImageMeta{ID: "we_fixture_cn"}, evidenceImageOptions{})
	findRow(t, layout.Response, `  "用户名": "测试用户甲",`)
	remark := -1
	for index, row := range layout.Response.Rows {
		if strings.HasPrefix(row.Text, `  "备注"`) {
			remark = index
		}
		if cells := runeCellsOf(row.Text); cells > layout.TextCells {
			t.Fatalf("行超出栏宽 %d>%d：%q", cells, layout.TextCells, row.Text)
		}
	}
	if remark < 0 || remark+1 >= len(layout.Response.Rows) || layout.Response.Rows[remark].Number == "" || layout.Response.Rows[remark+1].Number != "" {
		t.Fatalf("超宽中文行未按字符折行/续行行号不应显示：%q", layoutTexts(layout.Response))
	}

	long := strings.Repeat("abcdefghij", 30)
	flow.Request.Body = []byte(long)
	wrapped := testLayout(t, flow, evidenceImageMeta{ID: "we_fixture_cn"}, evidenceImageOptions{Marks: []string{"jabc"}})
	var joined strings.Builder
	continuation := 0
	for _, row := range wrapped.Request.Rows {
		if strings.HasPrefix(row.Text, "abcdefghij") || (row.Number == "" && !row.Note && strings.ContainsAny(row.Text, "abcdefghij")) {
			joined.WriteString(row.Text)
			if row.Number == "" {
				continuation++
			}
			if !row.Highlight || len(row.Marks) == 0 {
				t.Fatalf("折行后的各显示行应整体高亮且带 mark：%+v", row)
			}
		}
	}
	if joined.String() != long || continuation < 3 {
		t.Fatalf("超长行折行丢字或未折行：续行=%d", continuation)
	}
	if fonts, _ := loadEvidenceFonts(); fonts.fallback == nil {
		t.Log("本机无 CJK 回退字体：中文将画方框并在页脚说明")
	}
	renderAndCheck(t, evidenceImageChineseFlow(), evidenceImageMeta{ID: "we_fixture_cn"}, evidenceImageOptions{})
}

func TestEvidenceImageMaxLinesAndCaptureTruncated(t *testing.T) {
	flow := evidenceImageLongFlow(100)
	meta := evidenceImageMeta{ID: "we_fixture_long", ResponseTruncated: &evidenceTruncation{Kept: 65536}}
	layout := testLayout(t, flow, meta, evidenceImageOptions{})
	content, notes := 0, []string{}
	for _, row := range layout.Response.Rows {
		if row.Note {
			notes = append(notes, row.Text)
		} else {
			content++
		}
	}
	// 响应：状态行+1头+空行+100行 body = 103 行；显示 60 行，其余 43 行。
	if content != 60 || len(notes) != 2 || notes[0] != "… 其余 43 行未显示" || notes[1] != "原件已截断（仅保存前 65536 字节）" {
		t.Fatalf("content=%d notes=%q", content, notes)
	}
	if len(layout.Request.Rows) != 2 {
		t.Fatalf("请求栏不应带截断说明：%q", layoutTexts(layout.Request))
	}
	if footer := strings.Join(layout.Footer, " · "); !strings.Contains(footer, "每栏最多显示 60 行") || !strings.Contains(footer, "原件已截断") {
		t.Fatalf("footer=%q", footer)
	}
	short := testLayout(t, flow, meta, evidenceImageOptions{MaxLines: 10})
	if got := short.Response.Rows[10].Text; got != "… 其余 93 行未显示" {
		t.Fatalf("max_lines=10 说明=%q", got)
	}
	// 单个超长逻辑行跨越上限：标出部分显示。
	flow.Response.Body = []byte(strings.Repeat("x", 4000))
	partial := testLayout(t, flow, evidenceImageMeta{ID: "we_fixture_long"}, evidenceImageOptions{MaxLines: 10})
	if got := partial.Response.Rows[10].Text; got != "… 其余 1 行未显示（含第 4 行后半部分）" {
		t.Fatalf("部分显示说明=%q", got)
	}
	egress := testLayout(t, flow, evidenceImageMeta{ID: "wx_1", Label: "出口记录", ResponseTruncated: &evidenceTruncation{Kept: 4096, Observed: 9000}}, evidenceImageOptions{})
	if got := egress.Response.Rows[len(egress.Response.Rows)-1].Text; got != "原件已截断（仅保存前 4096 字节，共 9000 字节）" || egress.HeaderRight != "出口记录 wx_1" {
		t.Fatalf("出口截断说明=%q right=%q", got, egress.HeaderRight)
	}
	renderAndCheck(t, evidenceImageLongFlow(100), meta, evidenceImageOptions{})
}

func TestEvidenceImageBinaryNoResponseAndUnsent(t *testing.T) {
	flow := evidenceImageJSONFlow()
	flow.Response.Body = []byte{0x89, 'P', 'N', 'G', 0x00, 0x01, 0x02}
	layout := testLayout(t, flow, evidenceImageMeta{ID: "we_bin"}, evidenceImageOptions{})
	if row := layout.Response.Rows[len(layout.Response.Rows)-1]; row.Text != "（二进制内容 7 字节，未渲染）" || !row.Note {
		t.Fatalf("二进制=%+v", row)
	}
	if !evidenceBodyIsBinary(bytes.Repeat([]byte{0xff, 'a', 'b', 'c', 'd'}, 10)) || evidenceBodyIsBinary([]byte("普通文本 plain")) {
		t.Fatal("非 UTF-8 比例判定错误")
	}
	renderAndCheck(t, flow, evidenceImageMeta{ID: "we_bin"}, evidenceImageOptions{})

	flow.Response = nil
	flow.Error = "read tcp: i/o timeout"
	layout = testLayout(t, flow, evidenceImageMeta{ID: "we_unknown", Status: requestUnknown, Reason: "发送后未取得响应"}, evidenceImageOptions{})
	if len(layout.Response.Rows) != 1 || layout.Response.Rows[0].Text != "（无响应：unknown · 发送后未取得响应）" {
		t.Fatalf("无响应=%q", layoutTexts(layout.Response))
	}
	sent := false
	layout = testLayout(t, flow, evidenceImageMeta{ID: "we_denied", Status: "denied", Reason: "目标不在授权 origins 内", Sent: &sent}, evidenceImageOptions{})
	if layout.Response.Rows[0].Text != "（未发送：目标不在授权 origins 内）" {
		t.Fatalf("未发送=%q", layoutTexts(layout.Response))
	}
	renderAndCheck(t, flow, evidenceImageMeta{ID: "we_denied", Sent: &sent, Reason: "目标不在授权 origins 内"}, evidenceImageOptions{})
}

func TestEvidenceImageOptionsAndMissingGlyph(t *testing.T) {
	bad := []url.Values{
		{"max_lines": {"9"}}, {"max_lines": {"201"}}, {"max_lines": {"abc"}}, {"mask": {"2"}},
		{"mark": {"a", "b", "c", "d", "e", "f"}}, {"mark": {strings.Repeat("长", 101)}}, {"mark": {"a\nb"}},
	}
	for _, query := range bad {
		if _, err := evidenceImageOptionsFromQuery(query); err == nil {
			t.Fatalf("应拒绝 %v", query)
		}
	}
	opts, err := evidenceImageOptionsFromQuery(url.Values{"mark": {"", "token"}, "max_lines": {"10"}, "mask": {"0"}})
	if err != nil || len(opts.Marks) != 1 || opts.MaxLines != 10 || !opts.NoMask {
		t.Fatalf("opts=%+v err=%v", opts, err)
	}
	if _, err := renderEvidenceImage(evidenceImageJSONFlow(), evidenceImageMeta{}, evidenceImageOptions{MaxLines: 5}); err == nil {
		t.Fatal("渲染函数应校验 max_lines")
	}

	fonts, err := loadEvidenceFonts()
	if err != nil {
		t.Fatal(err)
	}
	withoutFallback := *fonts
	withoutFallback.fallback = nil
	flow := evidenceImageChineseFlow()
	layout := testLayout(t, flow, evidenceImageMeta{ID: "we_cn"}, evidenceImageOptions{})
	if _, missing, err := drawEvidenceLayout(layout, &withoutFallback); err != nil || !missing {
		t.Fatalf("无回退字体时应标缺字：missing=%v err=%v", missing, err)
	}
	flow.Response.Body = []byte("plain ascii only")
	layout = testLayout(t, flow, evidenceImageMeta{ID: "we_ascii"}, evidenceImageOptions{})
	if _, missing, err := drawEvidenceLayout(layout, &withoutFallback); err != nil || missing != hasNonASCII(strings.Join(layout.Footer, "")+layout.HeaderRight) {
		t.Fatalf("缺字判定异常：missing=%v err=%v", missing, err)
	}
}

func hasNonASCII(text string) bool {
	for _, r := range text {
		if r > 0x7f {
			return true
		}
	}
	return false
}

func TestEvidenceImageAPI(t *testing.T) {
	capability, _, task, inv, _, _ := newHTTPToolFixture(t, http.NotFoundHandler())
	flowPB, err := proto.Marshal(evidenceImageJSONFlow())
	if err != nil {
		t.Fatal(err)
	}
	now := "2026-11-19T05:00:00Z"
	insert := func(id string, flow []byte) {
		model := &requestModel{ID: id, TaskID: task.ID, SessionID: inv.SessionID, TurnID: "turn-" + id, CallID: "call-" + id, Fingerprint: "fp-" + id,
			Method: "POST", URL: "https://api.example.test/v1/orders", Status: requestCompleted, Sent: sql.NullBool{Bool: true, Valid: true},
			StatusCode: sql.NullInt64{Int64: 200, Valid: true}, Preview: []byte("{}"), FlowPB: flow, CreatedAt: now, CompletedAt: sql.NullString{String: now, Valid: true}}
		if _, err := capability.store.orm.NewInsert().Model(model).Exec(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	insert("we_image_ok", flowPB)
	insert("we_image_broken", []byte{0xff, 0xff, 0xff})
	denied := &traffic.Flow{Error: "未发送：目标不在授权 origins 内", Request: &traffic.HttpRequest{Method: "GET", Url: "http://other.example.test:8081/", Protocol: "HTTP/1.1", Headers: []*traffic.Header{{Name: "Host", Value: "other.example.test:8081"}}}}
	egress := &egressFlowModel{TaskID: task.ID, SessionID: inv.SessionID, Tool: egressToolName, Method: "GET", URL: "http://other.example.test:8081/", Host: "other.example.test",
		Decision: egressDenied, Error: "目标不在授权 origins 内", Sent: sql.NullBool{Valid: true}, CreatedAt: now, CompletedAt: sql.NullString{String: now, Valid: true}}
	if err := capability.store.insertEgressFlow(t.Context(), egress, denied); err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.Handle("GET /api/webapi/tasks/{id}/evidence/{evidence_id}/image.png", http.HandlerFunc(capability.handleEvidenceImage))
	mux.Handle("GET /api/webapi/tasks/{id}/egress/{flow_id}/image.png", http.HandlerFunc(capability.handleEgressImage))
	srv := httptest.NewServer(mux)
	defer srv.Close()
	get := func(path string) (*http.Response, []byte) {
		response, err := srv.Client().Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		var body bytes.Buffer
		_, _ = body.ReadFrom(response.Body)
		return response, body.Bytes()
	}
	base := "/api/webapi/tasks/" + task.ID
	response, body := get(base + "/evidence/we_image_ok/image.png?mark=ord-1001&mark=widget&max_lines=40&mask=1")
	if response.StatusCode != 200 || response.Header.Get("Content-Type") != "image/png" || response.Header.Get("Cache-Control") != "private, max-age=300" {
		t.Fatalf("status=%d headers=%v body=%s", response.StatusCode, response.Header, body)
	}
	if img, err := png.Decode(bytes.NewReader(body)); err != nil || img.Bounds().Dx() != evidenceImageWidth {
		t.Fatalf("png err=%v", err)
	}
	_, again := get(base + "/evidence/we_image_ok/image.png?mark=ord-1001&mark=widget&max_lines=40&mask=1")
	if !bytes.Equal(body, again) {
		t.Fatal("API 两次返回字节不同")
	}
	direct, err := renderEvidenceImage(evidenceImageJSONFlow(), requestEvidenceImageMeta(requestModel{ID: "we_image_ok", CreatedAt: now, Status: requestCompleted, Sent: sql.NullBool{Bool: true, Valid: true}}), evidenceImageOptions{Marks: []string{"ord-1001", "widget"}, MaxLines: 40})
	if err != nil || !bytes.Equal(direct, body) {
		t.Fatalf("API 与包内渲染函数结果不同：err=%v", err)
	}
	response, body = get(base + "/egress/" + egress.ID + "/image.png")
	if response.StatusCode != 200 || response.Header.Get("Content-Type") != "image/png" {
		t.Fatalf("egress status=%d body=%s", response.StatusCode, body)
	}
	cases := map[string]int{
		base + "/evidence/we_missing/image.png":                        404,
		"/api/webapi/tasks/other-task/evidence/we_image_ok/image.png":  404,
		base + "/evidence/we_image_ok/image.png?max_lines=5":           400,
		base + "/evidence/we_image_ok/image.png?mask=yes":              400,
		base + "/evidence/we_image_broken/image.png":                   422,
		base + "/egress/wx_missing/image.png":                          404,
		"/api/webapi/tasks/missing/egress/" + egress.ID + "/image.png": 404,
		base + "/egress/" + egress.ID + "/image.png?max_lines=500":     400,
	}
	for path, want := range cases {
		response, body := get(path)
		var decoded map[string]string
		if response.StatusCode != want || response.Header.Get("Content-Type") != "application/json" || json.Unmarshal(body, &decoded) != nil || decoded["error"] == "" {
			t.Fatalf("%s status=%d want=%d body=%s", path, response.StatusCode, want, body)
		}
	}
}

// TestEvidenceImageWriteSamples 只在设置 WSCYBER_EVIDENCE_SAMPLES_DIR 时写出样例图供人工查看。
func TestEvidenceImageWriteSamples(t *testing.T) {
	dir := os.Getenv("WSCYBER_EVIDENCE_SAMPLES_DIR")
	if dir == "" {
		t.Skip("未设置 WSCYBER_EVIDENCE_SAMPLES_DIR")
	}
	created := "2026-11-19T05:00:00Z"
	samples := map[string][]byte{
		"1-chinese-json.png": renderAndCheck(t, evidenceImageChineseFlow(), evidenceImageMeta{ID: "we_fixture_cn", CreatedAt: created, Status: requestCompleted}, evidenceImageOptions{Marks: []string{"测试用户甲"}}),
		"2-truncated-long.png": renderAndCheck(t, evidenceImageLongFlow(100), evidenceImageMeta{ID: "we_fixture_long", CreatedAt: created, Status: requestCompleted,
			ResponseTruncated: &evidenceTruncation{Kept: 65536}}, evidenceImageOptions{MaxLines: 30}),
		"3-masked-headers.png": renderAndCheck(t, evidenceImageJSONFlow(), evidenceImageMeta{ID: "we_fixture_json", CreatedAt: created, Status: requestCompleted}, evidenceImageOptions{Marks: []string{"ord-1001"}}),
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, data := range samples {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}
