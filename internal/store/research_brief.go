package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"wscyber/internal/domain"
)

func saveResearchBrief(ctx context.Context, tx pgx.Tx, missionID string, brief *domain.ResearchBrief) error {
	if brief == nil {
		return nil
	}
	if err := domain.ValidateResearchBrief(brief); err != nil {
		return err
	}
	raw, err := json.Marshal(brief)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO mission_research_briefs(mission_id,content,content_sha256) VALUES($1,$2,$3)`, missionID, string(raw), fmt.Sprintf("%x", sha256.Sum256(raw)))
	return err
}

// 即使分享关闭也核验原件。按任务读取，无正文回显；历史无行保持nil。
func readResearchBrief(ctx context.Context, q querier, missionID string) (*domain.ResearchBrief, error) {
	var content, hash string
	err := q.QueryRow(ctx, `SELECT content,content_sha256 FROM mission_research_briefs WHERE mission_id=$1`, missionID).Scan(&content, &hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	bad := fmt.Errorf("%w：身份说明与业务前提完整性检查失败", domain.ErrUnavailable)
	if len(content) > domain.MaxBriefJSONBytes || hash != fmt.Sprintf("%x", sha256.Sum256([]byte(content))) {
		return nil, bad
	}
	var brief domain.ResearchBrief
	if json.Unmarshal([]byte(content), &brief) != nil || domain.ValidateResearchBrief(&brief) != nil {
		return nil, bad
	}
	canonical, err := json.Marshal(&brief)
	// 写入只使用此规范编码。精确比较也拒绝重复/未知键、null、无效Unicode和尾随JSON。
	if err != nil || !bytes.Equal(canonical, []byte(content)) {
		return nil, bad
	}
	return &brief, nil
}
