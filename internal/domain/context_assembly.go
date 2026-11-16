package domain

import (
	"context"
	"encoding/json"
	"time"
)

const ContextPolicy = "bounded-history-1"
const MaxContextSources = 160
const MaxContextAssemblyBytes = 96 << 10
const MaxContextItemBytes = 64 << 10
const MaxContextHistoryBytes = 96 << 10

type ContextSource struct {
	Kind          string    `json:"kind"`
	ID            string    `json:"id"`
	State         string    `json:"state,omitempty"`
	At            time.Time `json:"at"`
	OriginalBytes int       `json:"original_bytes"`
	IncludedBytes int       `json:"included_bytes"`
	Included      bool      `json:"included"`
	Required      bool      `json:"required"`
	Reason        string    `json:"reason"`
	ContentSHA256 string    `json:"content_sha256,omitempty"`
}
type ContextWindow struct {
	Kind        string     `json:"kind"`
	Limit       int        `json:"limit"`
	OlderExists bool       `json:"older_exists"`
	CutoffAt    *time.Time `json:"cutoff_at,omitempty"`
	CutoffID    string     `json:"cutoff_id,omitempty"`
}
type ContextSelection struct {
	Policy  string          `json:"policy"`
	Windows []ContextWindow `json:"windows"`
	Sources []ContextSource `json:"sources"`
}
type ContextAssembly struct {
	ContextSelection
	MissionID     string    `json:"mission_id"`
	ThreadID      string    `json:"thread_id"`
	AttemptID     string    `json:"attempt_id"`
	AssembledAt   time.Time `json:"assembled_at"`
	InputBytes    int       `json:"input_bytes"`
	MaxInputBytes int       `json:"max_input_bytes"`
}
type ModelCallContext struct {
	CallID    string           `json:"call_id"`
	MissionID string           `json:"mission_id"`
	ThreadID  string           `json:"thread_id"`
	Recorded  bool             `json:"recorded"`
	Assembly  *ContextAssembly `json:"assembly"`
}

// 正式存储实现专用：初步作用域不加载历史，路由边界再读取同一事务中的有界材料。
type ResearchContextRepository interface {
	ResearchScope(context.Context, string) (Snapshot, error)
	AssembleResearchSnapshot(context.Context, string, string, string) (Snapshot, error)
	GetModelCallContext(context.Context, string, string) (ModelCallContext, error)
}

func ValidateContextAssembly(a *ContextAssembly) error {
	if a == nil {
		return ErrInvalid
	}
	withVerification := a.Policy == VerificationContextPolicy
	withFindings := a.Policy == FindingsContextPolicy || withVerification
	windowsCount := 3
	if withFindings {
		windowsCount = 4
	}
	if (a.Policy != ContextPolicy && !withFindings) || !ValidSkillLoadID(a.MissionID) || !ValidSkillLoadID(a.ThreadID) || !ValidSkillLoadID(a.AttemptID) || a.AssembledAt.IsZero() || a.InputBytes < 1 || a.MaxInputBytes != 256<<10 || a.InputBytes > a.MaxInputBytes || len(a.Sources) > MaxContextSources || len(a.Windows) != windowsCount {
		return ErrInvalid
	}
	windows := map[string]bool{}
	for _, w := range a.Windows {
		limit := 8
		if w.Kind == "observation" {
			limit = 32
		} else if w.Kind != "action" && w.Kind != "attempt" && !(withFindings && w.Kind == "finding") {
			return ErrInvalid
		}
		if windows[w.Kind] || w.Limit != limit || (w.CutoffAt == nil) != (w.CutoffID == "") || (w.OlderExists && w.CutoffID == "") || (w.CutoffID != "" && !ValidSkillLoadID(w.CutoffID)) {
			return ErrInvalid
		}
		windows[w.Kind] = true
	}
	seen := map[string]bool{}
	required := map[string]bool{}
	historyBytes := 0
	verificationSources := 0
	for _, s := range a.Sources {
		key := s.Kind + ":" + s.ID
		if seen[key] || s.At.IsZero() || s.OriginalBytes < 0 || s.OriginalBytes > 1<<30 || s.IncludedBytes < 0 || s.IncludedBytes > 256<<10 {
			return ErrInvalid
		}
		seen[key] = true
		if s.Kind == "material" {
			if !ValidLibraryID(s.ID) {
				return ErrInvalid
			}
		} else if !ValidSkillLoadID(s.ID) {
			return ErrInvalid
		}
		history := false
		switch s.Kind {
		case "mission", "material":
			if s.State != "" {
				return ErrInvalid
			}
		case "thread":
			switch s.State {
			case "ready", "running", "waiting_approval", "blocked", "paused", "concluded":
			default:
				return ErrInvalid
			}
		case "observation":
			history = true
			switch s.State {
			case "counterevidence", "contradiction", "refutation", "action_result", "model_observation", "other":
			default:
				return ErrInvalid
			}
		case "action":
			history = true
			switch s.State {
			case "pending", "approved", "running", "succeeded", "failed", "rejected", "outcome_unknown":
			default:
				return ErrInvalid
			}
		case "attempt":
			history = true
			switch s.State {
			case "running", "succeeded", "failed", "interrupted", "waiting_approval":
			default:
				return ErrInvalid
			}
		case "finding":
			history = true
			if !withFindings || s.State != "candidate" || len(s.ContentSHA256) != 64 {
				return ErrInvalid
			}
		case "verification_source":
			verificationSources++
			if !withVerification || s.State != "candidate" || len(s.ContentSHA256) != 64 || !s.Included || !s.Required || s.OriginalBytes > MaxContextItemBytes {
				return ErrInvalid
			}
			history = true
		case "research_brief":
			if s.State != "shared" {
				return ErrInvalid
			}
		case "safety_prompt":
			if s.State != "enabled" {
				return ErrInvalid
			}
		case "skill":
			if s.State != "active" {
				return ErrInvalid
			}
		case "skill_activity":
			switch s.State {
			case "discovered", "loaded", "released", "unavailable", "stale", "capacity", "not_active":
			default:
				return ErrInvalid
			}
		case "supplement":
			if s.State != "user_statement" {
				return ErrInvalid
			}
		default:
			return ErrInvalid
		}
		if withFindings && s.Included && (s.Kind == "action" || s.Kind == "observation") && len(s.ContentSHA256) != 64 {
			return ErrInvalid
		}
		if s.ContentSHA256 != "" {
			if len(s.ContentSHA256) != 64 {
				return ErrInvalid
			}
			for _, c := range s.ContentSHA256 {
				if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
					return ErrInvalid
				}
			}
		}
		if s.Included {
			if s.Reason != "selected" || s.IncludedBytes < 1 {
				return ErrInvalid
			}
			if s.Required {
				required[key] = true
			}
		} else if !history || s.Required || s.IncludedBytes != 0 || (s.Reason != "item_limit" && s.Reason != "history_budget") {
			return ErrInvalid
		}
		if history {
			historyBytes += s.IncludedBytes
			if s.Included && s.OriginalBytes > MaxContextItemBytes {
				return ErrInvalid
			}
		} else if !s.Required {
			return ErrInvalid
		}
	}
	for _, w := range a.Windows {
		if w.CutoffID == "" {
			continue
		}
		found := false
		for _, source := range a.Sources {
			if source.Kind == w.Kind && source.ID == w.CutoffID && source.At.Equal(*w.CutoffAt) {
				found = true
				break
			}
		}
		if !found {
			return ErrInvalid
		}
	}
	if (withVerification && verificationSources != 1) || (!withVerification && verificationSources != 0) {
		return ErrInvalid
	}
	if historyBytes > MaxContextHistoryBytes || !required["mission:"+a.MissionID] || !required["thread:"+a.ThreadID] || !required["attempt:"+a.AttemptID] {
		return ErrInvalid
	}
	raw, err := json.Marshal(a)
	if err != nil || len(raw) > MaxContextAssemblyBytes {
		return ErrInvalid
	}
	return nil
}
