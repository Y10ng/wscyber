package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"wscyber/internal/domain"
)

// 未覆写的 Repository 方法和 nil Executor 会在意外读全量/写入/执行时直接失败。
type httpRecordsRepoStub struct {
	domain.Repository
	calls     int
	missionID string
	actionID  string
	query     domain.HTTPRecordQuery
	page      domain.HTTPRecordPage
	detail    domain.HTTPRecordDetail
	err       error
}

func (r *httpRecordsRepoStub) ListHTTPRecords(_ context.Context, missionID string, q domain.HTTPRecordQuery) (domain.HTTPRecordPage, error) {
	r.calls++
	r.missionID, r.query = missionID, q
	return r.page, r.err
}

func (r *httpRecordsRepoStub) GetHTTPRecord(_ context.Context, missionID, actionID string) (domain.HTTPRecordDetail, error) {
	r.calls++
	r.missionID, r.actionID = missionID, actionID
	return r.detail, r.err
}

func TestHTTPRecordQueryValidation(t *testing.T) {
	for _, raw := range []string{
		"limit=", "limit=0", "limit=101", "limit=-1", "limit=%2B1", "limit=1.5", "limit=%201", "limit=999999999999999999999999",
		"limit=25&limit=25", "method=GET&method=POST", "status=&status=", "thread_id=&thread_id=", "cursor=&cursor=",
		"unknown=", "mission_id=other", "status=unknown", "status=SUCCEEDED", "status=%20pending",
		"method=G%20ET", "method=GET%0A", "method=G%3AET", "method=%E4%B8%AD", "method=" + strings.Repeat("A", 33),
		"method=%", "method=GET;status=failed", "thread_id=%00", "thread_id=" + strings.Repeat("a", 129),
		"cursor=" + strings.Repeat("A", 2049),
	} {
		t.Run(raw, func(t *testing.T) {
			r := &httpRecordsRepoStub{}
			h := New(r, nil, Config{Token: testToken})
			w := request(h, "GET", "/api/missions/mission/http-records?"+raw, "", testToken)
			if w.Code != 400 || r.calls != 0 || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("非法查询应在访问 Repository 前拒绝：%d calls=%d %s", w.Code, r.calls, w.Body.String())
			}
		})
	}
	for _, tc := range []struct {
		raw  string
		want domain.HTTPRecordQuery
	}{
		{"", domain.HTTPRecordQuery{Limit: 50}},
		{"thread_id=&method=&status=&cursor=", domain.HTTPRecordQuery{Limit: 50}},
		{"limit=1", domain.HTTPRecordQuery{Limit: 1}},
		{"limit=100&method=patch&status=failed&thread_id=thread&cursor=opaque", domain.HTTPRecordQuery{Limit: 100, Method: "PATCH", Status: "failed", ThreadID: "thread", Cursor: "opaque"}},
		{"method=" + url.QueryEscape("a!#$%&'*+-.^_`|~09"), domain.HTTPRecordQuery{Limit: 50, Method: "A!#$%&'*+-.^_`|~09"}},
	} {
		got, err := parseHTTPRecordQuery(tc.raw)
		if err != nil || got != tc.want {
			t.Fatalf("查询 %q：%+v %v，期望 %+v", tc.raw, got, err, tc.want)
		}
	}
	for _, status := range []string{"pending", "approved", "running", "succeeded", "failed", "rejected", "outcome_unknown"} {
		if q, err := parseHTTPRecordQuery("status=" + status); err != nil || q.Status != status {
			t.Fatal(status, q, err)
		}
	}
}

func TestHTTPRecordRoutesReadOnlyAndEnvelope(t *testing.T) {
	r := &httpRecordsRepoStub{page: domain.HTTPRecordPage{Items: []domain.HTTPRecordSummary{}}}
	h := New(r, nil, Config{Token: testToken})
	w := request(h, "GET", "/api/missions/project/http-records", "", testToken)
	if w.Code != 200 || strings.TrimSpace(w.Body.String()) != `{"items":[],"next_cursor":""}` || r.missionID != "project" || r.query.Limit != 50 || r.calls != 1 {
		t.Fatal(w.Code, w.Body.String(), r)
	}
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	r.page = domain.HTTPRecordPage{Items: []domain.HTTPRecordSummary{{
		ID: "action", MissionID: "project", ThreadID: "thread", ThreadTitle: "方向", Method: "GET",
		URL: "https://fixture.invalid/?sensitive=saved-query", Status: "pending", CreatedAt: now, UpdatedAt: now,
	}}, NextCursor: "opaque"}
	w = request(h, "GET", "/api/missions/project/http-records?limit=25&method=get", "", testToken)
	if w.Code != 200 || r.query.Method != "GET" || r.query.Limit != 25 {
		t.Fatal(w.Code, w.Body.String(), r.query)
	}
	var page map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil || len(page) != 2 {
		t.Fatal(page, err)
	}
	var items []map[string]json.RawMessage
	if err := json.Unmarshal(page["items"], &items); err != nil || len(items) != 1 {
		t.Fatal(items, err)
	}
	for _, key := range []string{"input", "result", "headers", "body", "body_base64", "error", "reason", "decision_reason", "request_hash", "status_code", "resolved_at", "total"} {
		if _, exists := items[0][key]; exists {
			t.Fatalf("摘要不应包含 %s", key)
		}
	}
	for _, key := range []string{"has_result", "truncated", "effect_unknown"} {
		if string(items[0][key]) != "false" {
			t.Fatal("布尔字段不能因 false 而省略", key, items[0])
		}
	}
	if !strings.Contains(w.Body.String(), "saved-query") {
		t.Fatal("列表 URL 应保留保存值，不假称通用脱敏")
	}
	r.detail = domain.HTTPRecordDetail{
		MissionID: "project", Thread: domain.Thread{ID: "thread", MissionID: "project"},
		Action: domain.Action{ID: "action", ThreadID: "thread", Status: "outcome_unknown",
			Input:  domain.ActionInput{Kind: "http", Method: "POST", URL: "https://fixture.invalid/", Body: "请求原文", Headers: map[string]string{"Authorization": "fixture-secret"}},
			Result: &domain.ActionResult{Body: []byte{0, 255, 128, 10}, EffectUnknown: true, Truncated: true}},
	}
	w = request(h, "GET", "/api/missions/project/http-records/action", "", testToken)
	var detail domain.HTTPRecordDetail
	if err := json.Unmarshal(w.Body.Bytes(), &detail); err != nil || !reflect.DeepEqual(detail, r.detail) || w.Code != 200 || r.actionID != "action" || r.missionID != "project" {
		t.Fatal(w.Code, detail, err)
	}
	calls := r.calls
	for _, method := range []string{"POST", "PUT", "DELETE"} {
		for _, path := range []string{"/api/missions/project/http-records", "/api/missions/project/http-records/action"} {
			w = request(h, method, path, `{}`, testToken)
			if w.Code < 400 || r.calls != calls {
				t.Fatal("记录端点不得提供执行或写入", method, path, w.Code)
			}
		}
	}
	w = request(h, "GET", "/api/missions/project/http-records/action?unknown=1", "", testToken)
	if w.Code != 400 || r.calls != calls {
		t.Fatal("详情不能默默忽略查询参数", w.Code)
	}
}

func TestHTTPRecordsAuthOriginAndErrors(t *testing.T) {
	for _, path := range []string{"/api/missions/project/http-records", "/api/missions/project/http-records/action"} {
		t.Run(path, func(t *testing.T) {
			r := &httpRecordsRepoStub{page: domain.HTTPRecordPage{Items: []domain.HTTPRecordSummary{}}}
			h := New(r, nil, Config{Token: testToken})
			for _, token := range []string{"", "wrong"} {
				w := request(h, "GET", path, "", token)
				if w.Code != 401 || r.calls != 0 || w.Header().Get("Cache-Control") != "no-store" {
					t.Fatal(w.Code, r.calls)
				}
			}
			for _, header := range []string{"Origin", "Sec-Fetch-Site"} {
				req := httptest.NewRequest(http.MethodGet, "http://localhost"+path, nil)
				req.Header.Set("Authorization", "Bearer "+testToken)
				if header == "Origin" {
					req.Header.Set(header, "https://other.invalid")
				} else {
					req.Header.Set(header, "cross-site")
				}
				w := httptest.NewRecorder()
				h.ServeHTTP(w, req)
				if w.Code != 403 || r.calls != 0 || w.Header().Get("Access-Control-Allow-Origin") != "" || w.Header().Get("Cache-Control") != "no-store" {
					t.Fatal(w.Code, r.calls, w.Header())
				}
			}
			req := httptest.NewRequest(http.MethodGet, "http://localhost"+path, nil)
			req.Header.Set("Authorization", "Bearer "+testToken)
			req.Header.Set("Origin", "http://localhost")
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)
			if w.Code != 200 || r.calls != 1 || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("同源只读请求应正常返回并禁止缓存", w.Code, r.calls, w.Header())
			}
			for _, tc := range []struct {
				err  error
				code int
			}{{domain.ErrNotFound, 404}, {domain.ErrInvalid, 400}, {errors.New("数据库秘密 fixture-secret"), 500}} {
				r.err = tc.err
				w := request(h, "GET", path, "", testToken)
				if w.Code != tc.code || w.Header().Get("Cache-Control") != "no-store" || strings.Contains(w.Body.String(), "fixture-secret") {
					t.Fatal(w.Code, w.Body.String(), w.Header())
				}
				if strings.Contains(w.Body.String(), `"items"`) || strings.Contains(w.Body.String(), `"action"`) {
					t.Fatal("错误不应冒充空列表或详情", w.Body.String())
				}
			}
		})
	}
}
