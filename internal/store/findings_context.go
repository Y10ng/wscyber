package store

import (
	"context"
	"github.com/jackc/pgx/v5"
	"wscyber/internal/domain"
)

// 仅当前方向最新八个候选的固定修订；第九项只用于更早范围标志。
func findingCandidates(ctx context.Context, tx pgx.Tx, thread string) ([]historyCandidate, domain.ContextWindow, error) {
	w := domain.ContextWindow{Kind: "finding", Limit: 8}
	rows, err := collect(ctx, tx, `SELECT r.id,f.updated_at,octet_length(r.content::text)+octet_length(r.evidence::text),r.content_sha256 FROM findings f JOIN finding_revisions r ON r.id=f.current_revision_id WHERE f.thread_id=$1 ORDER BY f.updated_at DESC,f.current_revision_id DESC LIMIT 9`, func(row scanner) (historyCandidate, error) {
		v := historyCandidate{ContextSource: domain.ContextSource{Kind: "finding", State: "candidate"}}
		e := row.Scan(&v.ID, &v.At, &v.OriginalBytes, &v.ContentSHA256)
		return v, e
	}, thread)
	if err != nil {
		return nil, w, err
	}
	if len(rows) > 8 {
		rows = rows[:8]
		w.OlderExists = true
	}
	if len(rows) > 0 {
		rows[0].Required = true
		last := rows[len(rows)-1]
		w.CutoffID = last.ID
		w.CutoffAt = &last.At
	}
	return rows, w, nil
}
