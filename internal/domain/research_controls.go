package domain

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

const MaxSupplements = 16
const MaxSupplementBytes = 16384
const MaxSupplementTotalBytes = 65536

type SupplementInput struct {
	Source         string `json:"source"`
	Content        string `json:"content"`
	ShareWithModel bool   `json:"share_with_model"`
}
type Supplement struct {
	ID            string    `json:"id"`
	MissionID     string    `json:"mission_id"`
	ThreadID      string    `json:"thread_id"`
	Source        string    `json:"source"`
	Kind          string    `json:"kind"`
	ByteSize      int       `json:"byte_size"`
	ContentSHA256 string    `json:"content_sha256"`
	CreatedAt     time.Time `json:"created_at"`
	Content       string    `json:"content,omitempty"`
}
type ResearchWriteReceipt struct {
	Key        string      `json:"key"`
	MissionID  string      `json:"mission_id"`
	ThreadID   string      `json:"thread_id"`
	Command    string      `json:"command"`
	Thread     *Thread     `json:"thread,omitempty"`
	Supplement *Supplement `json:"supplement,omitempty"`
}
type SupplementPage struct {
	MissionID string       `json:"mission_id"`
	ThreadID  string       `json:"thread_id"`
	Items     []Supplement `json:"items"`
}
type SupplementCalls struct {
	MissionID    string      `json:"mission_id"`
	ThreadID     string      `json:"thread_id"`
	SupplementID string      `json:"supplement_id"`
	Items        []ModelCall `json:"items"`
	NextCursor   string      `json:"next_cursor"`
}

func ValidateSupplement(in SupplementInput) error {
	valid := func(s string, n int) bool {
		return utf8.ValidString(s) && strings.TrimSpace(s) != "" && len(s) <= n && !strings.ContainsRune(s, 0)
	}
	if !in.ShareWithModel || !valid(in.Source, 256) || !valid(in.Content, MaxSupplementBytes) {
		return fmt.Errorf("%w：来源须为1–256字节，正文须为1–16384字节的有效文本，并明确同意提供给后续模型研究", ErrInvalid)
	}
	return nil
}
func (s Snapshot) SupplementIDs() []string {
	ids := make([]string, 0, len(s.Supplements))
	for _, v := range s.Supplements {
		ids = append(ids, v.ID)
	}
	return ids
}

type ResearchControlRepository interface {
	ControlResearch(context.Context, string, string, string, string, func(context.Context) error) (ResearchWriteReceipt, error)
	AddSupplement(context.Context, string, string, string, SupplementInput) (ResearchWriteReceipt, error)
	ListSupplements(context.Context, string, string) (SupplementPage, error)
	GetSupplement(context.Context, string, string, string) (Supplement, error)
	SupplementModelCalls(context.Context, string, string, string, string, int) (SupplementCalls, error)
	GetSupplementContext(context.Context, string, string) ([]Supplement, error)
}
