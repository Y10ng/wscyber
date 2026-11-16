package domain

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// 仅在提交键锁内确认尚无回执、选材检查失败时返回；不用于锁超时或提交身份冲突。
var ErrMaterialSelectionStale = fmt.Errorf("%w：选用资料已变更或删除，请刷新选择", ErrConflict)

const (
	MaxLibraryItems      = 2000
	MaxLibraryFolders    = 256
	MaxLibraryDepth      = 8
	MaxMaterialBytes     = 32768
	MaxLibraryBytes      = 16 * 1024 * 1024
	MaxTaskMaterials     = 16
	MaxTaskMaterialBytes = 65536
	MaxSafetyPromptBytes = 8000
)

type LibraryItem struct {
	ID             string    `json:"id"`
	ParentID       string    `json:"parent_id"`
	Kind           string    `json:"kind"`
	Classification string    `json:"classification"`
	Name           string    `json:"name"`
	Description    string    `json:"description"`
	Format         string    `json:"format"`
	ContentSHA256  string    `json:"content_sha256"`
	ByteSize       int       `json:"byte_size"`
	Token          string    `json:"token"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}
type LibraryDetail struct {
	LibraryItem
	Content string `json:"content"`
}
type LibraryWrite struct {
	ExpectedToken  string `json:"expected_token,omitempty"`
	ParentID       string `json:"parent_id"`
	Kind           string `json:"kind"`
	Name           string `json:"name"`
	Description    string `json:"description"`
	Content        string `json:"content"`
	Classification string `json:"classification,omitempty"` // 省略创建为普通，修改保留；不改变旧指纹。
}
type LibraryDelete struct {
	ExpectedToken string `json:"expected_token"`
}
type LibraryMutation struct {
	Key         string       `json:"key"`
	ItemID      string       `json:"item_id"`
	Deleted     bool         `json:"deleted"`
	Item        *LibraryItem `json:"item,omitempty"`
	CommittedAt time.Time    `json:"committed_at"`
}
type LibraryQuery struct {
	ParentID string
	All      bool
	Kind     string
	Search   string
	After    string
	Limit    int
}
type LibraryPage struct {
	Items      []LibraryItem `json:"items"`
	NextCursor string        `json:"next_cursor"`
}
type MaterialSelection struct {
	ID    string `json:"id"`
	Token string `json:"token"`
}
type MaterialReference struct {
	ID            string `json:"id"`
	SourceToken   string `json:"source_token"`
	Name          string `json:"name"`
	Path          string `json:"path"`
	Description   string `json:"description"`
	Format        string `json:"format"`
	ContentSHA256 string `json:"content_sha256"`
	ByteSize      int    `json:"byte_size"`
}
type TaskMaterial struct {
	MaterialReference
	Content string `json:"content"`
}
type ResearchConfiguration struct {
	MissionID           string              `json:"mission_id"`
	Materials           []MaterialReference `json:"materials"`
	SafetyPromptEnabled bool                `json:"safety_prompt_enabled"`
	SafetyPrompt        string              `json:"safety_prompt"`
	ResearchBrief       *ResearchBrief      `json:"research_brief,omitempty"`
}

// ResearchContext 仅在研究调用边界装配；普通线程/列表读取不加载材料正文。
type ResearchContext struct {
	Materials           []TaskMaterial `json:"materials"`
	SafetyPromptEnabled bool           `json:"safety_prompt_enabled"`
	SafetyPrompt        string         `json:"safety_prompt,omitempty"`
	ResearchBrief       *ResearchBrief `json:"research_brief,omitempty"`
}
type ResearchMaterialRepository interface {
	ListLibraryFolders(context.Context) ([]LibraryItem, error)
	ListLibraryItems(context.Context, LibraryQuery) (LibraryPage, error)
	GetLibraryItem(context.Context, string) (LibraryDetail, error)
	SaveLibraryItem(context.Context, string, string, LibraryWrite) (LibraryMutation, error)
	DeleteLibraryItem(context.Context, string, string, LibraryDelete) (LibraryMutation, error)
	GetLibraryOperation(context.Context, string) (LibraryMutation, error)
	GetResearchConfiguration(context.Context, string) (ResearchConfiguration, error)
	GetTaskMaterial(context.Context, string, string) (TaskMaterial, error)
	GetResearchContext(context.Context, string) (ResearchContext, error)
}

func ValidLibraryID(id string) bool {
	if len(id) != 32 {
		return false
	}
	for _, c := range id {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
func ValidateLibraryWrite(in LibraryWrite) error {
	bad := func(s string) error { return fmt.Errorf("%w：%s", ErrInvalid, s) }
	if in.ExpectedToken != "" && !ValidLibraryID(in.ExpectedToken) || in.ParentID != "" && !ValidLibraryID(in.ParentID) {
		return bad("资料身份无效")
	}
	if in.Name != strings.TrimSpace(in.Name) || in.Name == "" || len(in.Name) > 256 || !utf8.ValidString(in.Name) || in.Name == "." || in.Name == ".." || strings.ContainsAny(in.Name, "/\\") {
		return bad("名称须为1–256字节，不含路径或控制字符")
	}
	for _, c := range in.Name {
		if unicode.IsControl(c) {
			return bad("名称不能含控制字符")
		}
	}
	if !utf8.ValidString(in.Description) || len(in.Description) > 1024 || strings.ContainsRune(in.Description, 0) {
		return bad("说明须为不含NUL的UTF-8文本，最多1024字节")
	}
	if in.Classification != "" && in.Classification != "material" && in.Classification != "skill" {
		return bad("分类须为material或skill")
	}
	if in.Classification == "skill" && (in.Kind != "file" || strings.TrimSpace(in.Description) == "") {
		return bad("Skill须为文件并填写适用说明")
	}
	switch in.Kind {
	case "folder":
		if in.Content != "" {
			return bad("文件夹不能包含正文")
		}
	case "file":
		if MaterialFormat(in.Name) == "" {
			return bad("仅支持.md或.txt文本资料")
		}
		if !utf8.ValidString(in.Content) || len(in.Content) == 0 || len(in.Content) > MaxMaterialBytes || strings.ContainsRune(in.Content, 0) {
			return bad("正文须为非空UTF-8文本且不含NUL，最多32768字节")
		}
	default:
		return bad("资料类型须为folder或file")
	}
	return nil
}
func MaterialFormat(name string) string {
	switch {
	case strings.HasSuffix(strings.ToLower(name), ".md"):
		return "markdown"
	case strings.HasSuffix(strings.ToLower(name), ".txt"):
		return "text"
	}
	return ""
}
func ValidateResearchSelection(in ExplorationInput) error {
	bad := func(s string) error { return fmt.Errorf("%w：%s", ErrInvalid, s) }
	if len(in.Materials) > MaxTaskMaterials {
		return bad("最多选用16份资料")
	}
	seen := map[string]bool{}
	for _, m := range in.Materials {
		if !ValidLibraryID(m.ID) || !ValidLibraryID(m.Token) || seen[m.ID] {
			return bad("选用资料身份无效或重复")
		}
		seen[m.ID] = true
	}
	if !utf8.ValidString(in.SafetyPrompt) || len(in.SafetyPrompt) > MaxSafetyPromptBytes || strings.ContainsRune(in.SafetyPrompt, 0) {
		return bad("附加禁止行为须为不含NUL的UTF-8文本，最多8000字节")
	}
	if in.SafetyPromptEnabled && strings.TrimSpace(in.SafetyPrompt) == "" {
		return bad("开启附加禁止行为后须填写清单")
	}
	if !in.SafetyPromptEnabled && in.SafetyPrompt != "" {
		return bad("关闭附加禁止行为时不能提交清单")
	}
	return ValidateResearchBrief(in.ResearchBrief)
}
