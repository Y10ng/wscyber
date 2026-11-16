package store

import (
	"context"
	"time"
	"wscyber/internal/domain"
)

// 打开的详情只轮询当前修订身份，不反复加载正文或覆盖阅读内容。
func (s *Store) GetFindingSummary(ctx context.Context, mission, id string) (domain.FindingSummary, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var v domain.FindingSummary
	var n int64
	err := s.pool.QueryRow(ctx, `SELECT f.id,f.mission_id,f.thread_id,f.number,r.content->>'title',f.current_revision,f.current_revision_id,f.created_at,f.updated_at FROM findings f JOIN finding_revisions r ON r.id=f.current_revision_id WHERE f.id=$1 AND f.mission_id=$2`, id, mission).Scan(&v.ID, &v.MissionID, &v.ThreadID, &n, &v.Title, &v.Revision, &v.RevisionID, &v.CreatedAt, &v.UpdatedAt)
	v.Number = findingNumber(n)
	v.Status = "candidate"
	return v, databaseError(err)
}
