package domain

import (
	"fmt"
	"strings"
	"time"
)

// HTTPRecordSummary 只描述已保存的平台 HTTP 动作，不代表请求已经发出。
// URL 保留原值，可能含敏感 query；其余请求/响应正文仅按需从详情读取。
type HTTPRecordSummary struct {
	ID            string     `json:"id"`
	MissionID     string     `json:"mission_id"`
	ThreadID      string     `json:"thread_id"`
	ThreadTitle   string     `json:"thread_title"`
	Method        string     `json:"method"`
	URL           string     `json:"url"`
	Status        string     `json:"status"`
	HasResult     bool       `json:"has_result"`
	StatusCode    int        `json:"status_code,omitempty"`
	Truncated     bool       `json:"truncated"`
	EffectUnknown bool       `json:"effect_unknown"`
	ResolvedAt    *time.Time `json:"resolved_at,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

type HTTPRecordPage struct {
	Items      []HTTPRecordSummary `json:"items"`
	NextCursor string              `json:"next_cursor"`
}

// HTTPRecordDetail 复用原 Thread/Action，不生成新的请求身份或证据副本。
type HTTPRecordDetail struct {
	MissionID string `json:"mission_id"`
	Thread    Thread `json:"thread"`
	Action    Action `json:"action"`
}

type HTTPRecordQuery struct {
	Limit    int
	Cursor   string
	ThreadID string
	Status   string
	Method   string
}

// NormalizeHTTPRecordQuery 同时供 API 和持久层使用；游标内容及归属由持久层验证。
func NormalizeHTTPRecordQuery(q HTTPRecordQuery) (HTTPRecordQuery, error) {
	if q.Limit == 0 {
		q.Limit = 50
	}
	if q.Limit < 1 || q.Limit > 100 || len(q.Cursor) > 2048 || len(q.ThreadID) > 128 || strings.ContainsRune(q.ThreadID, '\x00') {
		return q, fmt.Errorf("%w：HTTP 记录分页或方向参数无效", ErrInvalid)
	}
	switch q.Status {
	case "", "pending", "approved", "running", "succeeded", "failed", "rejected", "outcome_unknown":
	default:
		return q, fmt.Errorf("%w：HTTP 记录状态无效", ErrInvalid)
	}
	if len(q.Method) > 32 {
		return q, fmt.Errorf("%w：HTTP 方法必须为 1–32 字节的 token", ErrInvalid)
	}
	for _, c := range q.Method {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || strings.ContainsRune("!#$%&'*+-.^_`|~", c)) {
			return q, fmt.Errorf("%w：HTTP 方法必须为 1–32 字节的 token", ErrInvalid)
		}
	}
	q.Method = strings.ToUpper(q.Method)
	return q, nil
}
