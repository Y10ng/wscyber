package store

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"wscyber/internal/domain"
)

var _ domain.FindingRepository = (*Store)(nil)

const findingRevisionColumns = `r.id,r.finding_id,f.mission_id,f.thread_id,f.number,r.revision,r.created_at,r.attempt_id,r.call_id,COALESCE(e.id::text,''),c.started_at,c.finished_at,r.content_sha256,r.content,r.evidence`
const findingRevisionFrom = ` FROM finding_revisions r JOIN findings f ON f.id=r.finding_id JOIN model_calls c ON c.id=r.call_id LEFT JOIN events e ON e.finding_revision_id=r.id `

func findingNumber(n int64) string { return fmt.Sprintf("WSC-%06d", n) }
func sourceHash(value any) string {
	raw, _ := json.Marshal(value)
	return fmt.Sprintf("%x", sha256.Sum256(raw))
}
func scanFindingRevision(row scanner) (domain.FindingRevision, error) {
	var v domain.FindingRevision
	var n int64
	var body, evidence []byte
	err := row.Scan(&v.ID, &v.FindingID, &v.MissionID, &v.ThreadID, &n, &v.Revision, &v.CreatedAt, &v.AttemptID, &v.CallID, &v.EventID, &v.CallStartedAt, &v.CallFinishedAt, &v.ContentSHA256, &body, &evidence)
	if err == nil {
		err = json.Unmarshal(body, &v.Content)
	}
	if err == nil {
		err = json.Unmarshal(evidence, &v.Evidence)
	}
	v.Number = findingNumber(n)
	v.Status = "candidate"
	return v, err
}
func getFindingRevision(ctx context.Context, tx pgx.Tx, mission, finding, revision string) (domain.FindingRevision, error) {
	return scanFindingRevision(tx.QueryRow(ctx, `SELECT `+findingRevisionColumns+findingRevisionFrom+`WHERE f.mission_id=$1 AND f.id=$2 AND r.id=CASE WHEN $3='' THEN f.current_revision_id ELSE $3 END`, mission, finding, revision))
}

// ApplyFinding从可信Attempt定位唯一有效call；不接受模型提供归属或调用身份。
func (s *Store) ApplyFinding(ctx context.Context, attempt string, p domain.FindingProposal) (domain.FindingRevision, error) {
	var zero domain.FindingRevision
	if !domain.ValidSkillLoadID(attempt) || domain.ValidateFindingProposal(p) != nil {
		return zero, domain.ErrInvalid
	}
	hash := sourceHash(p)
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return transaction(ctx, s, pgx.TxOptions{}, func(tx pgx.Tx) (domain.FindingRevision, error) {
		if _, err := tx.Exec(ctx, `SET LOCAL lock_timeout='2s'`); err != nil {
			return zero, err
		}
		var thread string
		if err := tx.QueryRow(ctx, `SELECT thread_id FROM attempts WHERE id=$1`, attempt).Scan(&thread); err != nil {
			return zero, err
		}
		t, err := lockThread(ctx, tx, thread)
		if err != nil {
			return zero, err
		}
		var oldID, oldFinding, oldHash string
		err = tx.QueryRow(ctx, `SELECT id,finding_id,proposal_hash FROM finding_revisions WHERE attempt_id=$1`, attempt).Scan(&oldID, &oldFinding, &oldHash)
		if err == nil {
			if hash != oldHash {
				return zero, domain.ErrConflict
			}
			return getFindingRevision(ctx, tx, t.MissionID, oldFinding, oldID)
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return zero, err
		}
		var callID string
		var assemblyRaw []byte
		err = tx.QueryRow(ctx, `SELECT c.id,x.assembly FROM attempts a JOIN model_calls c ON c.attempt_id=a.id JOIN model_call_contexts x ON x.call_id=c.id WHERE a.id=$1 AND a.thread_id=$2 AND a.status='running' AND c.thread_id=$2 AND c.mission_id=$3 AND c.purpose='research' AND c.state='completed' AND c.outcome='valid_response' AND c.attempted=true`, attempt, t.ID, t.MissionID).Scan(&callID, &assemblyRaw)
		if err != nil {
			return zero, err
		}
		var assembly domain.ContextAssembly
		if json.Unmarshal(assemblyRaw, &assembly) != nil || domain.ValidateContextAssembly(&assembly) != nil || assembly.Policy != domain.FindingsContextPolicy || assembly.AttemptID != attempt || assembly.ThreadID != t.ID || assembly.MissionID != t.MissionID {
			return zero, domain.ErrInvalid
		}
		direct := map[string]domain.ContextSource{}
		inherited := map[string]domain.FindingEvidence{}
		selected := map[string]domain.FindingRevision{}
		for _, v := range assembly.Sources {
			if !v.Included {
				continue
			}
			if v.Kind == "action" || v.Kind == "observation" {
				direct[v.Kind+":"+v.ID] = v
			}
			if v.Kind == "finding" {
				r, e := scanFindingRevision(tx.QueryRow(ctx, `SELECT `+findingRevisionColumns+findingRevisionFrom+`WHERE r.id=$1 AND f.thread_id=$2 AND f.mission_id=$3`, v.ID, t.ID, t.MissionID))
				if e != nil {
					return zero, e
				}
				if r.ContentSHA256 != v.ContentSHA256 {
					return zero, domain.ErrConflict
				}
				selected[r.FindingID] = r
				for _, ref := range r.Evidence {
					key := ref.Kind + ":" + ref.ID
					old, ok := inherited[key]
					if !ok || ref.CapturedAt.After(old.CapturedAt) {
						inherited[key] = ref
					}
				}
			}
		}
		evidence := make([]domain.FindingEvidence, 0, len(p.Content.Evidence))
		for _, ref := range p.Content.Evidence {
			key := ref.Kind + ":" + ref.ID
			if src, ok := direct[key]; ok {
				fact, e := captureFindingEvidence(ctx, tx, ref, t.ID, assembly.AssembledAt, src.ContentSHA256)
				if e != nil {
					return zero, e
				}
				evidence = append(evidence, fact)
			} else if fact, ok := inherited[key]; ok {
				table := map[string]string{"action": "actions", "observation": "observations"}[ref.Kind]
				var exists bool
				if e := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM `+table+` WHERE id=$1 AND thread_id=$2)`, ref.ID, t.ID).Scan(&exists); e != nil {
					return zero, e
				}
				if !exists {
					return zero, domain.ErrInvalid
				}
				fact.FindingReference = ref
				evidence = append(evidence, fact)
			} else {
				return zero, domain.ErrInvalid
			}
		}
		revisionID, err := newID()
		if err != nil {
			return zero, err
		}
		now := time.Now().UTC().Truncate(time.Microsecond)
		finding := p.FindingID
		revision := int64(1)
		if finding == "" {
			finding, err = newID()
			if err != nil {
				return zero, err
			}
			_, err = tx.Exec(ctx, `INSERT INTO findings(id,mission_id,thread_id,current_revision,current_revision_id,created_at,updated_at) VALUES($1,$2,$3,1,$4,$5,$5)`, finding, t.MissionID, t.ID, revisionID, now)
		} else {
			seen, ok := selected[finding]
			if !ok || seen.Revision != p.ExpectedRevision {
				return zero, domain.ErrConflict
			}
			var current int64
			if err = tx.QueryRow(ctx, `SELECT current_revision FROM findings WHERE id=$1 AND mission_id=$2 AND thread_id=$3 FOR UPDATE`, finding, t.MissionID, t.ID).Scan(&current); err != nil {
				return zero, err
			}
			if current != p.ExpectedRevision {
				return zero, domain.ErrConflict
			}
			revision = current + 1
			_, err = tx.Exec(ctx, `UPDATE findings SET current_revision=$2,current_revision_id=$3,updated_at=$4 WHERE id=$1`, finding, revision, revisionID, now)
		}
		if err != nil {
			return zero, err
		}
		body, _ := json.Marshal(p.Content)
		refs, _ := json.Marshal(evidence)
		contentHash := sourceHash(struct {
			Content  domain.FindingContent
			Evidence []domain.FindingEvidence
		}{p.Content, evidence})
		if _, err = tx.Exec(ctx, `INSERT INTO finding_revisions(id,finding_id,revision,attempt_id,call_id,proposal_hash,content_sha256,content,evidence,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, revisionID, finding, revision, attempt, callID, hash, contentHash, body, refs, now); err != nil {
			return zero, err
		}
		// 与既有事件相同的提交顺序锁；所有业务行锁均已取得。
		if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1,$2)`, lockNamespace, eventLock); err != nil {
			return zero, err
		}
		kind, message := "finding.created", "已保存候选发现"
		if revision > 1 {
			kind, message = "finding.updated", "已更新候选发现，历史保留"
		}
		if _, err = tx.Exec(ctx, `INSERT INTO events(mission_id,thread_id,kind,message,finding_id,finding_revision_id) VALUES($1,$2,$3,$4,$5,$6)`, t.MissionID, t.ID, kind, message, finding, revisionID); err != nil {
			return zero, err
		}
		return getFindingRevision(ctx, tx, t.MissionID, finding, revisionID)
	})
}
func captureFindingEvidence(ctx context.Context, tx pgx.Tx, ref domain.FindingReference, thread string, at time.Time, expectedHash string) (domain.FindingEvidence, error) {
	v := domain.FindingEvidence{FindingReference: ref, ThreadID: thread, CapturedAt: at}
	if len(expectedHash) != 64 {
		return v, domain.ErrInvalid
	}
	value, err := readFindingOriginal(ctx, tx, ref, thread)
	if err != nil {
		return v, err
	}
	v.ContentSHA256 = sourceHash(value)
	if v.ContentSHA256 != expectedHash {
		return v, domain.ErrConflict
	}
	switch x := value.(type) {
	case domain.Action:
		v.AttemptID = x.AttemptID
		v.ActionID = x.ID
		v.CreatedAt = x.CreatedAt
		v.State = x.Status
		v.ActionKind = x.Input.Kind
		v.RequestHash = x.RequestHash
		v.HasResult = x.Result != nil
		if x.Result != nil {
			v.Truncated = x.Result.Truncated
			v.EffectUnknown = x.Result.EffectUnknown
		}
		if x.Status == "outcome_unknown" {
			v.EffectUnknown = true
		}
	case domain.Observation:
		v.AttemptID = x.AttemptID
		v.ActionID = x.ActionID
		v.CreatedAt = x.CreatedAt
		switch x.Kind {
		case "counterevidence", "contradiction", "refutation", "action_result", "model_observation", "operator_note":
			v.State = x.Kind
		default:
			v.State = "other"
		}
	}
	return v, nil
}

// 先元数据字节检查，防止显式原件读取成为无界正文通道。
func readFindingOriginal(ctx context.Context, tx pgx.Tx, ref domain.FindingReference, thread string) (any, error) {
	var size int
	if ref.Kind == "action" {
		err := tx.QueryRow(ctx, `SELECT octet_length(input::text)+COALESCE(octet_length(result::text),0)+octet_length(error)+octet_length(decision_reason)+octet_length(resolution_note) FROM actions WHERE id=$1 AND thread_id=$2`, ref.ID, thread).Scan(&size)
		if err != nil {
			return nil, err
		}
		if size > 128<<10 {
			return nil, domain.ErrUnavailable
		}
		return scanAction(tx.QueryRow(ctx, `SELECT `+actionColumns+` FROM actions WHERE id=$1 AND thread_id=$2`, ref.ID, thread))
	}
	if ref.Kind != "observation" {
		return nil, domain.ErrInvalid
	}
	err := tx.QueryRow(ctx, `SELECT octet_length(kind)+octet_length(content) FROM observations WHERE id=$1 AND thread_id=$2`, ref.ID, thread).Scan(&size)
	if err != nil {
		return nil, err
	}
	if size > 128<<10 {
		return nil, domain.ErrUnavailable
	}
	return scanObservation(tx.QueryRow(ctx, `SELECT `+observationColumns+` FROM observations WHERE id=$1 AND thread_id=$2`, ref.ID, thread))
}
func (s *Store) GetFinding(ctx context.Context, mission, finding, revision string) (domain.FindingRevision, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return transaction(ctx, s, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) (domain.FindingRevision, error) {
		return getFindingRevision(ctx, tx, mission, finding, revision)
	})
}

// 游标使用严格规范JSON+base64url；重编码比较拒绝重复/未知/别名/null/尾随字段。
type findingCursor struct {
	Version int    `json:"version"`
	Mission string `json:"mission"`
	Thread  string `json:"thread"`
	Finding string `json:"finding"`
	Before  string `json:"before"`
}

func decodeFindingCursor(raw, mission, thread, finding string) (int64, error) {
	if raw == "" {
		return 0, nil
	}
	if len(raw) > 1024 {
		return 0, domain.ErrInvalid
	}
	b, e := base64.RawURLEncoding.Strict().DecodeString(raw)
	if e != nil {
		return 0, domain.ErrInvalid
	}
	var c findingCursor
	if json.Unmarshal(b, &c) != nil {
		return 0, domain.ErrInvalid
	}
	canonical, _ := json.Marshal(c)
	if base64.RawURLEncoding.EncodeToString(canonical) != raw || c.Version != 1 || c.Mission != mission || c.Thread != thread || c.Finding != finding {
		return 0, domain.ErrInvalid
	}
	n, e := strconv.ParseInt(c.Before, 10, 64)
	if e != nil || n < 1 || strconv.FormatInt(n, 10) != c.Before {
		return 0, domain.ErrInvalid
	}
	return n, nil
}
func encodeFindingCursor(mission, thread, finding string, n int64) string {
	b, _ := json.Marshal(findingCursor{1, mission, thread, finding, strconv.FormatInt(n, 10)})
	return base64.RawURLEncoding.EncodeToString(b)
}
func (s *Store) ListFindings(ctx context.Context, mission string, q domain.FindingQuery) (domain.FindingsPage, error) {
	zero := domain.FindingsPage{MissionID: mission, Items: []domain.FindingSummary{}}
	q, err := domain.NormalizeFindingQuery(q)
	if err != nil {
		return zero, err
	}
	before, err := decodeFindingCursor(q.Cursor, mission, q.ThreadID, "")
	if err != nil {
		return zero, err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return transaction(ctx, s, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) (domain.FindingsPage, error) {
		if _, err := getMission(ctx, tx, mission); err != nil {
			return zero, err
		}
		if q.ThreadID != "" {
			var exists bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM threads WHERE id=$1 AND mission_id=$2)`, q.ThreadID, mission).Scan(&exists); err != nil {
				return zero, err
			}
			if !exists {
				return zero, domain.ErrNotFound
			}
		}
		nums := []int64{}
		items, err := collect(ctx, tx, `SELECT f.id,f.mission_id,f.thread_id,f.number,r.content->>'title',f.current_revision,f.current_revision_id,f.created_at,f.updated_at FROM findings f JOIN finding_revisions r ON r.id=f.current_revision_id WHERE f.mission_id=$1 AND ($2='' OR f.thread_id=$2) AND ($3::bigint=0 OR f.number<$3) ORDER BY f.number DESC LIMIT $4`, func(row scanner) (domain.FindingSummary, error) {
			var v domain.FindingSummary
			var n int64
			e := row.Scan(&v.ID, &v.MissionID, &v.ThreadID, &n, &v.Title, &v.Revision, &v.RevisionID, &v.CreatedAt, &v.UpdatedAt)
			v.Number = findingNumber(n)
			v.Status = "candidate"
			nums = append(nums, n)
			return v, e
		}, mission, q.ThreadID, before, q.Limit+1)
		if err != nil {
			return zero, err
		}
		zero.Items = items
		if len(items) > q.Limit {
			zero.Items = items[:q.Limit]
			zero.NextCursor = encodeFindingCursor(mission, q.ThreadID, "", nums[q.Limit-1])
		}
		return zero, nil
	})
}
func (s *Store) FindingHistory(ctx context.Context, mission, finding string, q domain.FindingQuery) (domain.FindingHistoryPage, error) {
	out := domain.FindingHistoryPage{MissionID: mission, FindingID: finding, Items: []domain.FindingHistoryRow{}}
	q, err := domain.NormalizeFindingQuery(q)
	if err != nil || q.ThreadID != "" {
		return out, domain.ErrInvalid
	}
	before, err := decodeFindingCursor(q.Cursor, mission, "", finding)
	if err != nil {
		return out, err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return transaction(ctx, s, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) (domain.FindingHistoryPage, error) {
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM findings WHERE id=$1 AND mission_id=$2)`, finding, mission).Scan(&exists); err != nil {
			return out, err
		}
		if !exists {
			return out, domain.ErrNotFound
		}
		items, err := collect(ctx, tx, `SELECT r.id,r.finding_id,f.mission_id,f.thread_id,r.revision,r.content->>'title',r.created_at,r.attempt_id,r.call_id,e.id::text FROM finding_revisions r JOIN findings f ON f.id=r.finding_id JOIN events e ON e.finding_revision_id=r.id WHERE f.id=$1 AND f.mission_id=$2 AND ($3::bigint=0 OR r.revision<$3) ORDER BY r.revision DESC LIMIT $4`, func(row scanner) (domain.FindingHistoryRow, error) {
			var v domain.FindingHistoryRow
			e := row.Scan(&v.ID, &v.FindingID, &v.MissionID, &v.ThreadID, &v.Revision, &v.Title, &v.CreatedAt, &v.AttemptID, &v.CallID, &v.EventID)
			return v, e
		}, finding, mission, before, q.Limit+1)
		if err != nil {
			return out, err
		}
		out.Items = items
		if len(items) > q.Limit {
			out.Items = items[:q.Limit]
			out.NextCursor = encodeFindingCursor(mission, "", finding, out.Items[q.Limit-1].Revision)
		}
		return out, nil
	})
}
func (s *Store) FindingEvidenceContent(ctx context.Context, mission, finding, revision string, index int) (domain.FindingEvidenceContent, error) {
	out := domain.FindingEvidenceContent{MissionID: mission, FindingID: finding, RevisionID: revision, Index: index}
	if index < 0 || index >= 16 || revision == "" {
		return out, domain.ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return transaction(ctx, s, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) (domain.FindingEvidenceContent, error) {
		r, err := getFindingRevision(ctx, tx, mission, finding, revision)
		if err != nil {
			return out, err
		}
		if index >= len(r.Evidence) {
			return out, domain.ErrNotFound
		}
		ref := r.Evidence[index]
		out.Reference = ref
		out.Thread, err = getThread(ctx, tx, r.ThreadID)
		if err != nil {
			return out, err
		}
		if out.Thread.MissionID != mission || ref.ThreadID != r.ThreadID {
			return out, domain.ErrInvalid
		}
		original, err := readFindingOriginal(ctx, tx, ref.FindingReference, r.ThreadID)
		if err != nil {
			return out, err
		}
		out.MatchesSnapshot = sourceHash(original) == ref.ContentSHA256
		switch v := original.(type) {
		case domain.Action:
			out.Action = &v
		case domain.Observation:
			out.Observation = &v
		}
		raw, err := json.Marshal(out)
		if err != nil || len(raw) > domain.MaxFindingEvidenceBytes {
			return out, domain.ErrUnavailable
		}
		return out, nil
	})
}
