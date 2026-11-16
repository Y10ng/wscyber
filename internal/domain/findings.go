package domain

import (
	"context"
	"encoding/json"
	"strings"
	"time"
	"unicode/utf8"
)

const MaxFindingProposalBytes = 24 << 10
const MaxFindingEvidenceBytes = 256 << 10
const FindingsContextPolicy = "bounded-history-findings-1"

type FindingReference struct {
	Kind     string `json:"kind"`
	ID       string `json:"id"`
	Relation string `json:"relation"`
}
type FindingStep struct {
	Text            string `json:"text"`
	EvidenceIndices []int  `json:"evidence_indices"`
}
type FindingContent struct {
	Title       string             `json:"title"`
	Description string             `json:"description"`
	Steps       []FindingStep      `json:"steps"`
	Remediation string             `json:"remediation"`
	Limitations string             `json:"limitations"`
	Evidence    []FindingReference `json:"evidence"`
}
type FindingProposal struct {
	FindingID        string         `json:"finding_id,omitempty"`
	ExpectedRevision int64          `json:"expected_revision,omitempty"`
	Content          FindingContent `json:"content"`
}

// FindingEvidence只保留被引用原件的时点事实，不复制正文或将模型关系判断当验证结论。
type FindingEvidence struct {
	FindingReference
	ThreadID      string    `json:"thread_id"`
	AttemptID     string    `json:"attempt_id"`
	ActionID      string    `json:"action_id"`
	CreatedAt     time.Time `json:"created_at"`
	CapturedAt    time.Time `json:"captured_at"`
	ContentSHA256 string    `json:"content_sha256"`
	State         string    `json:"state"`
	ActionKind    string    `json:"action_kind"`
	RequestHash   string    `json:"request_hash"`
	HasResult     bool      `json:"has_result"`
	Truncated     bool      `json:"truncated"`
	EffectUnknown bool      `json:"effect_unknown"`
}
type FindingSummary struct {
	ID         string    `json:"id"`
	MissionID  string    `json:"mission_id"`
	ThreadID   string    `json:"thread_id"`
	Number     string    `json:"number"`
	Title      string    `json:"title"`
	Status     string    `json:"status"`
	Revision   int64     `json:"revision"`
	RevisionID string    `json:"revision_id"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}
type FindingRevision struct {
	ID             string            `json:"id"`
	FindingID      string            `json:"finding_id"`
	MissionID      string            `json:"mission_id"`
	ThreadID       string            `json:"thread_id"`
	Number         string            `json:"number"`
	Revision       int64             `json:"revision"`
	Status         string            `json:"status"`
	CreatedAt      time.Time         `json:"created_at"`
	AttemptID      string            `json:"attempt_id"`
	CallID         string            `json:"call_id"`
	EventID        string            `json:"event_id"`
	CallStartedAt  time.Time         `json:"call_started_at"`
	CallFinishedAt time.Time         `json:"call_finished_at"`
	ContentSHA256  string            `json:"content_sha256"`
	Content        FindingContent    `json:"content"`
	Evidence       []FindingEvidence `json:"evidence"`
}
type FindingHistoryRow struct {
	ID        string    `json:"id"`
	FindingID string    `json:"finding_id"`
	MissionID string    `json:"mission_id"`
	ThreadID  string    `json:"thread_id"`
	Revision  int64     `json:"revision"`
	Title     string    `json:"title"`
	CreatedAt time.Time `json:"created_at"`
	AttemptID string    `json:"attempt_id"`
	CallID    string    `json:"call_id"`
	EventID   string    `json:"event_id"`
}
type FindingQuery struct {
	ThreadID, Cursor string
	Limit            int
}
type FindingsPage struct {
	MissionID  string           `json:"mission_id"`
	Items      []FindingSummary `json:"items"`
	NextCursor string           `json:"next_cursor"`
}
type FindingHistoryPage struct {
	MissionID  string              `json:"mission_id"`
	FindingID  string              `json:"finding_id"`
	Items      []FindingHistoryRow `json:"items"`
	NextCursor string              `json:"next_cursor"`
}
type FindingEvidenceContent struct {
	MissionID       string          `json:"mission_id"`
	FindingID       string          `json:"finding_id"`
	RevisionID      string          `json:"revision_id"`
	Index           int             `json:"index"`
	Reference       FindingEvidence `json:"reference"`
	MatchesSnapshot bool            `json:"matches_snapshot"`
	Thread          Thread          `json:"thread"`
	Action          *Action         `json:"action,omitempty"`
	Observation     *Observation    `json:"observation,omitempty"`
}
type FindingRepository interface {
	ApplyFinding(context.Context, string, FindingProposal) (FindingRevision, error)
	ListFindings(context.Context, string, FindingQuery) (FindingsPage, error)
	GetFinding(context.Context, string, string, string) (FindingRevision, error)
	GetFindingSummary(context.Context, string, string) (FindingSummary, error)
	FindingHistory(context.Context, string, string, FindingQuery) (FindingHistoryPage, error)
	FindingEvidenceContent(context.Context, string, string, string, int) (FindingEvidenceContent, error)
}

func ValidFindingText(s string, max int, required bool) bool {
	return utf8.ValidString(s) && !strings.ContainsRune(s, 0) && len(s) <= max && (!required || strings.TrimSpace(s) != "")
}
func ValidateFindingProposal(p FindingProposal) error {
	if p.FindingID == "" && p.ExpectedRevision != 0 || p.FindingID != "" && (!ValidSkillLoadID(p.FindingID) || p.ExpectedRevision < 1 || p.ExpectedRevision >= 2147483647) {
		return ErrInvalid
	}
	c := p.Content
	if !ValidFindingText(c.Title, 256, true) || !ValidFindingText(c.Description, 8192, true) || !ValidFindingText(c.Remediation, 4096, false) || !ValidFindingText(c.Limitations, 4096, false) || len(c.Steps) < 1 || len(c.Steps) > 16 || len(c.Evidence) < 1 || len(c.Evidence) > 16 {
		return ErrInvalid
	}
	seen := map[string]bool{}
	for _, e := range c.Evidence {
		key := e.Kind + ":" + e.ID
		if (e.Kind != "action" && e.Kind != "observation") || !ValidSkillLoadID(e.ID) || seen[key] {
			return ErrInvalid
		}
		seen[key] = true
		if e.Relation != "supporting" && e.Relation != "counterevidence" && e.Relation != "limitation" {
			return ErrInvalid
		}
	}
	for _, step := range c.Steps {
		if !ValidFindingText(step.Text, 1024, true) || step.EvidenceIndices == nil || len(step.EvidenceIndices) > 16 {
			return ErrInvalid
		}
		used := map[int]bool{}
		for _, i := range step.EvidenceIndices {
			if i < 0 || i >= len(c.Evidence) || used[i] {
				return ErrInvalid
			}
			used[i] = true
		}
	}
	raw, err := json.Marshal(p)
	if err != nil || len(raw) > MaxFindingProposalBytes {
		return ErrInvalid
	}
	return nil
}
func NormalizeFindingQuery(q FindingQuery) (FindingQuery, error) {
	if q.Limit == 0 {
		q.Limit = 25
	}
	if q.Limit < 1 || q.Limit > 50 || len(q.Cursor) > 1024 || q.ThreadID != "" && !ValidSkillLoadID(q.ThreadID) {
		return q, ErrInvalid
	}
	return q, nil
}
