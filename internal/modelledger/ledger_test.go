package modelledger

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"wscyber/internal/domain"
	"wscyber/internal/modelconfig"
	"wscyber/internal/modelprovider"
)

func str(s string) *string { return &s }
func TestPriceExactAndMissingBuckets(t *testing.T) {
	p := domain.PriceTerms{Currency: "USD", InputPerMillion: "2", OutputPerMillion: "8", CacheReadPerMillion: str("0.5"), CacheWrite5mPerMillion: str("2.5"), CacheWrite1hPerMillion: str("4")}
	base := domain.ModelCost{Currency: str("USD"), PriceID: str("price"), PriceSnapshot: &p}
	for _, c := range []struct{ protocol, body, amount string }{
		{"openai_chat", `{"usage":{"prompt_tokens":120,"completion_tokens":20,"prompt_tokens_details":{"cached_tokens":40},"completion_tokens_details":{"reasoning_tokens":5}}}`, "0.00034"},
		{"anthropic_messages", `{"usage":{"input_tokens":100,"output_tokens":30,"cache_read_input_tokens":40,"cache_creation_input_tokens":20,"cache_creation":{"ephemeral_5m_input_tokens":12,"ephemeral_1h_input_tokens":8}}}`, "0.000522"},
	} {
		u, x := modelprovider.ParseUsage(c.protocol, []byte(c.body))
		got := Estimate(c.protocol, u, x, base)
		if got.Status != "estimated" || got.Amount == nil || *got.Amount != c.amount {
			t.Fatal(got)
		}
	}
	u := domain.ModelUsage{Status: "reported", InputTokens: str("1"), OutputTokens: str("0"), CacheReadInputTokens: str("0")}
	p.InputPerMillion = "0.000001"
	got := Estimate("openai_chat", u, false, base)
	if got.Amount == nil || *got.Amount != "0.000000000001" {
		t.Fatal(got)
	}
	p.InputPerMillion = "1000000"
	u.InputTokens = str("1000000000000")
	got = Estimate("openai_chat", u, false, base)
	if got.Amount == nil || *got.Amount != "1000000000000" {
		t.Fatal(got)
	}
	u.CacheReadInputTokens = nil
	got = Estimate("openai_chat", u, false, base)
	if got.Status != "unsupported" || got.Amount != nil {
		t.Fatal(got)
	}
	u.CacheReadInputTokens = str("1")
	p.CacheReadPerMillion = nil
	got = Estimate("openai_chat", u, false, base)
	if got.Status != "unsupported" || got.Amount != nil {
		t.Fatal(got)
	}
	u.CacheReadInputTokens = str("0")
	if Estimate("openai_chat", u, false, base).Status != "estimated" {
		t.Fatal("零桶不要求价格")
	}
	if Estimate("openai_chat", u, true, base).Status != "unsupported" || Estimate("openai_chat", u, false, domain.ModelCost{}).Status != "unpriced" {
		t.Fatal("不能估算额外费用或缺价格")
	}
	u.Status = "invalid"
	got = Estimate("openai_chat", u, false, base)
	if got.Status != "usage_missing" || got.Amount != nil {
		t.Fatal(got)
	}
}
func TestPriceInputStrict(t *testing.T) {
	for _, s := range []string{"-1", "+1", "01", "1e3", "NaN", "1.1234567", "1000000.000001", "1000001", "1.", ".1", " 1", ""} {
		if _, ok := priceUnits(s); ok {
			t.Fatal(s)
		}
	}
	raw := `{"expected_revision":0,"source":"environment","model_config_id":"","protocol":"openai_chat","model":"fixture","enabled":true,"currency":"USD","input_per_million":"2","output_per_million":"8","cache_read_per_million":null,"cache_write_5m_per_million":null,"cache_write_1h_per_million":null}`
	if _, e := DecodePrice([]byte(raw)); e != nil {
		t.Fatal(e)
	}
	for _, bad := range []string{strings.Replace(raw, `"enabled":true`, `"enabled":true,"enabled":false`, 1), strings.Replace(raw, `"enabled":true`, `"Enabled":true`, 1), strings.Replace(raw, `"enabled":true`, `"enabled":null`, 1), strings.Replace(raw, `"input_per_million":"2"`, `"input_per_million":2`, 1), raw + `{}`, strings.Replace(raw, `"cache_read_per_million":null,`, "", 1)} {
		if _, e := DecodePrice([]byte(bad)); e == nil {
			t.Fatal(bad)
		}
	}
}
func TestCalendarFiltersAndDST(t *testing.T) {
	now := time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		from, to string
		hours    int
	}{{"2026-03-08", "2026-03-09", 23}, {"2026-11-01", "2026-11-02", 25}} {
		f, e := ParseFilter(url.Values{"from": {c.from}, "to": {c.to}, "time_zone": {"America/New_York"}}, "", true, now)
		if e != nil || f.To.Sub(f.From) != time.Duration(c.hours)*time.Hour {
			t.Fatal(f, e)
		}
	}
	f, e := ParseFilter(url.Values{}, "mission", true, now)
	if e != nil || !f.AllTime || f.Range.From != "2026-11-06" || f.Range.To != "2026-11-13" {
		t.Fatal(f, e)
	}
	for _, q := range []string{"from=2026-01-01&to=2026-04-05", "from=2026-11-12&to=2026-11-12", "time_zone=Local", "time_zone=missing", "from=2026-02-30", "limit=101", "outcome=unknown", "limit=1&limit=2", "unknown=x", "purpose="} {
		v, _ := url.ParseQuery(q)
		if _, e := ParseFilter(v, "", false, now); e == nil {
			t.Fatal(q)
		}
	}
	if _, e := ParseFilter(url.Values{"cursor": {"x"}}, "", true, now); e == nil {
		t.Fatal("聚合不接受游标")
	}
	if _, e := ParseFilter(url.Values{"mission_id": {"other"}}, "mission", true, now); e == nil {
		t.Fatal("不能覆盖路径归属")
	}
}
func TestRedactAbnormalAndDuplicateJSON(t *testing.T) {
	secrets := domain.ModelSecrets{APIKey: "known-api-key", Headers: map[string]string{"X-Custom": "header-secret"}}
	bs := string(rune(92))
	for _, raw := range []string{
		`{"a":"known-` + bs + `u0061pi-key","` + bs + `u0061":"safe"}`,
		`{"a":"known-` + bs + `u0061pi-key`,
		`bad known-` + bs + `u0061pi-key tail`,
		`{"a":"known-api-key","a":"safe"}`,
		`{"a":"known-api-key","a":"safe"}`,
		`{"known-api-key":"business","echo":"header-secret"}`,
		`{"a":"known-api-key`,
		`invalid known-api-key tail`,
		`invalid known-api-key tail`,
	} {
		got, changed := redact([]byte(raw), secrets)
		if !changed || bytes.Contains(got, []byte("known")) || bytes.Contains(got, []byte("header-secret")) || len(got) > len(raw) {
			t.Fatalf("未移除或膨胀: %q", got)
		}
	}
	raw := []byte("{\"a\":\"business\",\"a\":\"duplicate\"}\n")
	if got, changed := redact(raw, secrets); changed || !bytes.Equal(got, raw) {
		t.Fatal("无凭据原字节不能改", string(got))
	}
	got, changed := redact([]byte(`{"echo":"`+bs+`ud83d`+bs+`ude00"}`), domain.ModelSecrets{APIKey: "😀"})
	if !changed || bytes.Contains(got, []byte("ud83d")) {
		t.Fatal(string(got))
	}
	raw = bytes.Repeat([]byte("a"), 1<<20)
	got, changed = redact(raw, domain.ModelSecrets{APIKey: "a"})
	if !changed || len(got) != 0 {
		t.Fatal("短凭据不能放大原文", len(got))
	}
}

type callRepo struct {
	domain.ModelCallRepository
	recording           bool
	beginErr, finishErr error
	starts, finishes    int
	begin               domain.BeginModelCall
	finish              domain.FinishModelCall
	cancel              context.CancelFunc
}

func (r *callRepo) GetModelRecording(context.Context, string) (domain.ModelRecording, error) {
	return domain.ModelRecording{RecordModelContent: r.recording}, nil
}
func (r *callRepo) BeginModelCall(_ context.Context, in domain.BeginModelCall) (domain.ModelCall, error) {
	r.starts++
	r.begin = in
	in.Call.StartedAt = time.Now()
	return in.Call, r.beginErr
}
func (r *callRepo) FinishModelCall(ctx context.Context, in domain.FinishModelCall) error {
	r.finishes++
	r.finish = in
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return r.finishErr
}
func TestLedgerFailStopNoReplayAndIndependentFinish(t *testing.T) {
	var hits atomic.Int32
	var cancel context.CancelFunc
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"object":"chat.completion","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"OK"}}],"usage":{"prompt_tokens":2,"completion_tokens":1,"prompt_tokens_details":{"cached_tokens":0}}}`)
	}))
	defer provider.Close()
	doc := domain.ModelDocument{ModelConnection: domain.ModelConnection{Name: "fixture", Protocol: "openai_chat", BaseURL: provider.URL, Model: "fake", Auth: "none", TimeoutSeconds: 2, MaxOutputTokens: 32}}
	id := Identity{Source: "environment", MissionID: "mission", ThreadID: "thread", AttemptID: "attempt", SkillLoadIDs: []string{"11111111-1111-1111-1111-111111111111"}}
	input := modelprovider.Input{User: "business", MaxOutputTokens: 32}
	for _, stage := range []string{"begin", "finish", "ok"} {
		t.Run(stage, func(t *testing.T) {
			repo := &callRepo{}
			if stage == "begin" {
				repo.beginErr = errors.New("SQL-secret")
			}
			if stage == "finish" {
				repo.finishErr = errors.New("SQL-secret")
			}
			service := New(repo, nil, nil)
			before := hits.Load()
			ctx, c := context.WithCancel(context.Background())
			cancel = c
			defer c()
			result, err := service.Complete(ctx, id, doc, input, func(string) bool { cancel(); return false })
			if len(repo.begin.SkillLoadIDs) != 1 || repo.begin.SkillLoadIDs[0] != id.SkillLoadIDs[0] {
				t.Fatal("Begin未携带实际Skill加载身份", repo.begin.SkillLoadIDs)
			}
			if stage == "ok" {
				if err != nil || result.Outcome != "invalid_response" || repo.finish.Usage.Status != "reported" || repo.finishes != 1 {
					t.Fatal(result, err)
				}
				return
			}
			if !errors.Is(err, domain.ErrUnavailable) || strings.Contains(err.Error(), "SQL-secret") || result.Text != "" {
				t.Fatal(result, err)
			}
			want := int32(1)
			if stage == "begin" {
				want = 0
			}
			if hits.Load()-before != want {
				t.Fatal(hits.Load(), before)
			}
			select {
			case <-service.Failed():
			default:
				t.Fatal("未fail-stop")
			}
			if _, e := service.Complete(context.Background(), id, doc, input, nil); !errors.Is(e, domain.ErrUnavailable) || hits.Load()-before != want {
				t.Fatal("停止后外发或重放", e)
			}
		})
	}
	repo := &callRepo{}
	service := New(repo, nil, nil)
	for _, input := range []modelprovider.Input{{User: "x", MaxOutputTokens: 0}, {User: string([]byte{255}), MaxOutputTokens: 1}, {User: "x", MaxOutputTokens: 33}} {
		if _, err := service.Complete(context.Background(), id, doc, input, nil); !errors.Is(err, domain.ErrInvalid) || repo.starts != 0 {
			t.Fatal("校验失败制造账本", err)
		}
	}
}
func TestContentVaultAADAndWrongKey(t *testing.T) {
	v, err := modelconfig.NewVault(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32)))
	if err != nil {
		t.Fatal(err)
	}
	other, err := modelconfig.NewVault(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{2}, 32)))
	if err != nil {
		t.Fatal(err)
	}
	id := strings.Repeat("1", 32)
	sealed, err := v.SealContent(id, "input", []byte("business"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := v.OpenContent(id, "input", sealed)
	if err != nil || string(got) != "business" {
		t.Fatal(string(got), err)
	}
	for _, c := range []struct {
		vault    *modelconfig.Vault
		id, kind string
	}{{other, id, "input"}, {v, id, "response"}, {v, strings.Repeat("2", 32), "input"}} {
		if _, err := c.vault.OpenContent(c.id, c.kind, sealed); !errors.Is(err, domain.ErrUnavailable) {
			t.Fatal("错钥或AAD未拒绝", err)
		}
	}
}
