package domain

import (
	"context"
	"encoding/json"
	"time"
)

const VerificationContextPolicy = "bounded-verification-1"
const MaxVerificationSteps = 8
const MaxFindingVerifications = 32

type VerificationInput struct {
	SourceRevisionID string `json:"source_revision_id"`
	MaxSteps         int    `json:"max_steps"`
}
type VerificationReceipt struct {
	Key              string `json:"key"`
	MissionID        string `json:"mission_id"`
	FindingID        string `json:"finding_id"`
	VerificationID   string `json:"verification_id"`
	ThreadID         string `json:"thread_id"`
	SourceRevisionID string `json:"source_revision_id"`
	StartStatus      string `json:"start_status"`
}
type VerificationSummary struct {
	ID               string    `json:"id"`
	MissionID        string    `json:"mission_id"`
	FindingID        string    `json:"finding_id"`
	SourceRevisionID string    `json:"source_revision_id"`
	SourceCreatedAt  time.Time `json:"source_created_at"`
	ThreadID         string    `json:"thread_id"`
	MaxSteps         int       `json:"max_steps"`
	CreatedAt        time.Time `json:"created_at"`
	Status           string    `json:"status"`
	PauseRequested   bool      `json:"pause_requested"`
	AutoRun          bool      `json:"auto_run"`
	StepCount        int       `json:"step_count"`
	Verdict          string    `json:"verdict"`
	HasResult        bool      `json:"has_result"`
	EndReason        string    `json:"end_reason"`
}
type VerificationConclusion struct {
	Verdict       string             `json:"verdict"`
	Method        string             `json:"method"`
	Preconditions string             `json:"preconditions"`
	Summary       string             `json:"summary"`
	Limitations   string             `json:"limitations"`
	Evidence      []FindingReference `json:"evidence"`
}
type VerificationResult struct {
	AttemptID  string                 `json:"attempt_id"`
	CallID     string                 `json:"call_id"`
	CreatedAt  time.Time              `json:"created_at"`
	Conclusion VerificationConclusion `json:"conclusion"`
	Evidence   []FindingEvidence      `json:"evidence"`
}
type VerificationDetail struct {
	VerificationSummary
	Thread         Thread              `json:"thread"`
	Result         *VerificationResult `json:"result"`
	ExecutionError string              `json:"execution_error"`
}
type VerificationPage struct {
	MissionID  string                `json:"mission_id"`
	FindingID  string                `json:"finding_id"`
	Items      []VerificationSummary `json:"items"`
	NextCursor string                `json:"next_cursor"`
}
type VerificationContext struct {
	ID       string          `json:"id"`
	MaxSteps int             `json:"max_steps"`
	Source   FindingRevision `json:"source"`
}
type VerificationEvidenceContent struct {
	MissionID       string          `json:"mission_id"`
	FindingID       string          `json:"finding_id"`
	VerificationID  string          `json:"verification_id"`
	Index           int             `json:"index"`
	Reference       FindingEvidence `json:"reference"`
	MatchesSnapshot bool            `json:"matches_snapshot"`
	Thread          Thread          `json:"thread"`
	Action          *Action         `json:"action,omitempty"`
	Observation     *Observation    `json:"observation,omitempty"`
}
type VerificationRepository interface {
	StartVerification(context.Context, string, string, string, VerificationInput, func(context.Context) error) (VerificationReceipt, error)
	ListVerifications(context.Context, string, string, FindingQuery) (VerificationPage, error)
	GetVerification(context.Context, string, string, string) (VerificationDetail, error)
	ApplyVerification(context.Context, string, VerificationConclusion) (VerificationResult, error)
	VerificationEvidence(context.Context, string, string, string, int) (VerificationEvidenceContent, error)
}

func ValidateVerificationInput(in VerificationInput) error {
	if !ValidSkillLoadID(in.SourceRevisionID) || in.MaxSteps < 1 || in.MaxSteps > MaxVerificationSteps {
		return ErrInvalid
	}
	return nil
}
func ValidateVerificationConclusion(c VerificationConclusion) error {
	if c.Verdict != "supported" && c.Verdict != "counterevidence" && c.Verdict != "inconclusive" {
		return ErrInvalid
	}
	if !ValidFindingText(c.Method, 4096, true) || !ValidFindingText(c.Summary, 4096, true) || !ValidFindingText(c.Preconditions, 4096, false) || !ValidFindingText(c.Limitations, 4096, false) || c.Evidence == nil || len(c.Evidence) > 16 {
		return ErrInvalid
	}
	seen := map[string]bool{}
	for _, e := range c.Evidence {
		k := e.Kind + ":" + e.ID
		if (e.Kind != "action" && e.Kind != "observation") || !ValidSkillLoadID(e.ID) || seen[k] || (e.Relation != "supporting" && e.Relation != "counterevidence" && e.Relation != "limitation") {
			return ErrInvalid
		}
		seen[k] = true
	}
	raw, err := json.Marshal(c)
	if err != nil || len(raw) > MaxFindingProposalBytes {
		return ErrInvalid
	}
	return nil
}

// 仅程序可确定的最低证据条件，不替代业务语义复核或升级confirmed。
func QualifyingVerificationAction(a Action) bool {
	if a.Input.Kind != "http" || a.Status != "succeeded" || a.Result == nil || a.Result.Truncated || a.Result.EffectUnknown {
		return false
	}
	// 状态码只证明捕获的是HTTP响应：500可含错误证据，302可含重定向证据；200也不证明漏洞。
	return a.Result.StatusCode >= 100 && a.Result.StatusCode <= 599
}
