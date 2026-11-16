package domain

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	MaxActiveSkills     = 4
	MaxActiveSkillBytes = 65536
	MaxSerializedSkills = 98304
)

// SkillRequest 不接受脚本、URL或权限；按Decision种类严格区分字段。
type SkillRequest struct {
	Query       string `json:"query,omitempty"`
	After       string `json:"after,omitempty"`
	Limit       int    `json:"limit,omitempty"`
	ItemID      string `json:"item_id,omitempty"`
	SourceToken string `json:"source_token,omitempty"`
	Purpose     string `json:"purpose,omitempty"`
	LoadID      string `json:"load_id,omitempty"`
}
type SkillDescriptor struct {
	ID            string `json:"id"`
	Token         string `json:"token"`
	Name          string `json:"name"`
	Description   string `json:"description"`
	ByteSize      int    `json:"byte_size"`
	ContentSHA256 string `json:"content_sha256"`
}
type SkillResult struct {
	Status     string             `json:"status"`
	Items      []SkillDescriptor  `json:"items,omitempty"`
	NextCursor string             `json:"next_cursor,omitempty"`
	LoadID     string             `json:"load_id,omitempty"`
	Material   *MaterialReference `json:"material,omitempty"`
}
type SkillActivity struct {
	Sequence  string       `json:"sequence"`
	AttemptID string       `json:"attempt_id"`
	MissionID string       `json:"mission_id"`
	ThreadID  string       `json:"thread_id"`
	Kind      string       `json:"kind"`
	Request   SkillRequest `json:"request"`
	Result    SkillResult  `json:"result"`
	CreatedAt time.Time    `json:"created_at"`
}
type SkillLoad struct {
	LoadID    string    `json:"load_id"`
	MissionID string    `json:"mission_id"`
	ThreadID  string    `json:"thread_id"`
	AttemptID string    `json:"attempt_id"`
	Purpose   string    `json:"purpose"`
	CreatedAt time.Time `json:"created_at"`
	TaskMaterial
}
type SkillContext struct {
	Active       []SkillLoad    `json:"active"`
	LastActivity *SkillActivity `json:"last_activity,omitempty"`
}

func (s *SkillContext) LoadIDs() []string {
	if s == nil {
		return nil
	}
	ids := make([]string, 0, len(s.Active))
	for _, l := range s.Active {
		ids = append(ids, l.LoadID)
	}
	return ids
}

type SkillUsageItem struct {
	ItemID          string `json:"item_id"`
	Name            string `json:"name"`
	SuccessfulLoads string `json:"successful_loads"`
}
type SkillUsagePage struct {
	MissionID            string           `json:"mission_id"`
	TotalSuccessfulLoads string           `json:"total_successful_loads"`
	Items                []SkillUsageItem `json:"items"`
	NextCursor           string           `json:"next_cursor"`
}
type SkillActivityPage struct {
	MissionID  string          `json:"mission_id"`
	Items      []SkillActivity `json:"items"`
	NextCursor string          `json:"next_cursor"`
}
type SkillCallsPage struct {
	MissionID  string      `json:"mission_id"`
	LoadID     string      `json:"load_id"`
	Items      []ModelCall `json:"items"`
	NextCursor string      `json:"next_cursor"`
}
type SkillRepository interface {
	ApplySkillActivity(context.Context, string, string, SkillRequest) (SkillActivity, error)
	GetSkillContext(context.Context, string, string) (SkillContext, error)
	SkillUsage(context.Context, string, string, int) (SkillUsagePage, error)
	ListSkillActivities(context.Context, string, string, int) (SkillActivityPage, error)
	GetSkillLoad(context.Context, string, string) (SkillLoad, error)
	SkillModelCalls(context.Context, string, string, string, int) (SkillCallsPage, error)
}

// ValidSkillLoadID 与现有Attempt的UUID文本身份一致，不同于资料库的32位hex ID。
func ValidSkillLoadID(id string) bool {
	return len(id) == 36 && id[8] == '-' && id[13] == '-' && id[18] == '-' && id[23] == '-' && ValidLibraryID(strings.ReplaceAll(id, "-", ""))
}

func ValidateSkillRequest(kind string, r SkillRequest) error {
	bad := func() error { return fmt.Errorf("%w：Skill活动字段无效", ErrInvalid) }
	validText := func(s string, n int) bool { return utf8.ValidString(s) && len(s) <= n && !strings.ContainsRune(s, 0) }
	switch kind {
	case "skill_discover":
		if !validText(r.Query, 128) || r.After != "" && !ValidLibraryID(r.After) || r.Limit < 0 || r.Limit > 20 || r.ItemID != "" || r.SourceToken != "" || r.Purpose != "" || r.LoadID != "" {
			return bad()
		}
	case "skill_load":
		if !ValidLibraryID(r.ItemID) || !ValidLibraryID(r.SourceToken) || strings.TrimSpace(r.Purpose) == "" || !validText(r.Purpose, 1024) || r.Query != "" || r.After != "" || r.Limit != 0 || r.LoadID != "" {
			return bad()
		}
	case "skill_release":
		if !ValidSkillLoadID(r.LoadID) || r.Query != "" || r.After != "" || r.Limit != 0 || r.ItemID != "" || r.SourceToken != "" || r.Purpose != "" {
			return bad()
		}
	default:
		return bad()
	}
	return nil
}
