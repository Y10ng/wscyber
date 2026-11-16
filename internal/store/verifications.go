package store

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"wscyber/internal/domain"
)

var _ domain.VerificationRepository = (*Store)(nil)

func verificationPath(mission, finding string) string {
	return "/api/missions/" + mission + "/findings/" + finding + "/verifications"
}
func (s *Store) StartVerification(ctx context.Context, key, mission, finding string, in domain.VerificationInput, validate func(context.Context) error) (domain.VerificationReceipt, error) {
	var out domain.VerificationReceipt
	if !domain.ValidOperationKey(key) || !domain.ValidSkillLoadID(mission) || !domain.ValidSkillLoadID(finding) || domain.ValidateVerificationInput(in) != nil {
		return out, domain.ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	path, hash := verificationPath(mission, finding), sourceHash(in)
	return transaction(ctx, s, pgx.TxOptions{}, func(tx pgx.Tx) (domain.VerificationReceipt, error) {
		if _, err := tx.Exec(ctx, `SET LOCAL lock_timeout='2s'`); err != nil {
			return out, err
		}
		kh := sha256.Sum256([]byte(key))
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1::bigint)`, int64(binary.BigEndian.Uint64(kh[:8]))); err != nil {
			return out, err
		}
		var method, oldPath, oldHash string
		var response []byte
		err := tx.QueryRow(ctx, `SELECT method,path,request_hash,response FROM operations WHERE key=$1`, key).Scan(&method, &oldPath, &oldHash, &response)
		if err == nil {
			if method != "POST" || oldPath != path || oldHash != hash {
				return out, domain.ErrConflict
			}
			err = json.Unmarshal(response, &out)
			return out, err
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return out, err
		}
		var current string
		if err = tx.QueryRow(ctx, `SELECT current_revision_id FROM findings WHERE id=$1 AND mission_id=$2 FOR UPDATE`, finding, mission).Scan(&current); err != nil {
			return out, err
		}
		if current != in.SourceRevisionID {
			return out, conflict("候选已更新，请重读后明确发起验证")
		}
		// 先锁定已有独立方向，启动与原控制/派发/收尾不能交错逃逸。
		if _, err = collect(ctx, tx, `SELECT t.id FROM threads t JOIN finding_verifications v ON v.thread_id=t.id WHERE v.finding_id=$1 ORDER BY t.id FOR UPDATE OF t`, func(row scanner) (string, error) { var id string; err := row.Scan(&id); return id, err }, finding); err != nil {
			return out, err
		}
		var unsettled bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM finding_verifications v WHERE v.finding_id=$1 AND (EXISTS(SELECT 1 FROM attempts WHERE thread_id=v.thread_id AND status='running') OR EXISTS(SELECT 1 FROM actions WHERE thread_id=v.thread_id AND `+unsettledPredicate+`)))`, finding).Scan(&unsettled); err != nil {
			return out, err
		}
		if unsettled {
			return out, conflict("原验证仍有在途或未决动作，不能另起验证绕过")
		}
		var total, active int
		if err = tx.QueryRow(ctx, `SELECT count(*),count(*) FILTER(WHERE NOT EXISTS(SELECT 1 FROM verification_results r WHERE r.verification_id=v.id) AND NOT EXISTS(SELECT 1 FROM verification_ends e WHERE e.verification_id=v.id)) FROM finding_verifications v WHERE finding_id=$1`, finding).Scan(&total, &active); err != nil {
			return out, err
		}
		if total >= domain.MaxFindingVerifications || active > 0 {
			return out, conflict("已有未结束的独立验证或达到记录容量；请查看原验证")
		}
		if validate == nil {
			return out, domain.ErrUnavailable
		}
		if err = validate(ctx); err != nil {
			return out, err
		}
		m, err := getMission(ctx, tx, mission)
		if err != nil {
			return out, err
		}
		if len(m.AllowedOrigins) == 0 {
			return out, domain.ErrInvalid
		}
		src, err := getFindingRevision(ctx, tx, mission, finding, current)
		if err != nil {
			return out, err
		}
		id, err := newID()
		if err != nil {
			return out, err
		}
		thread, err := newID()
		if err != nil {
			return out, err
		}
		// 来源由结构化关联固定，不在自然语言中伪造父子关系。
		_, err = tx.Exec(ctx, `INSERT INTO threads(id,mission_id,title,question,rationale,auto_run) VALUES($1,$2,$3,$4,$5,true)`, thread, mission, "独立验证 · "+src.Number, "使用新证据独立检验固定候选的主张、前提与反证", "有限验证；不扩大授权，不将模型判读升级为确认")
		if err != nil {
			return out, err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO finding_verifications(id,mission_id,finding_id,source_revision_id,thread_id,max_steps) VALUES($1,$2,$3,$4,$5,$6)`, id, mission, finding, current, thread, in.MaxSteps); err != nil {
			return out, err
		}
		if err = appendEvent(ctx, tx, mission, thread, "verification.started", "已接收固定候选的独立验证："+id); err != nil {
			return out, err
		}
		out = domain.VerificationReceipt{Key: key, MissionID: mission, FindingID: finding, VerificationID: id, ThreadID: thread, SourceRevisionID: current, StartStatus: "accepted"}
		response, err = json.Marshal(out)
		if err != nil {
			return out, err
		}
		_, err = tx.Exec(ctx, `INSERT INTO operations(key,method,path,request_hash,resource_type,resource_id,response_status,response) VALUES($1,'POST',$2,$3,'verification',$4,202,$5)`, key, path, hash, id, response)
		return out, err
	})
}

const verificationColumns = `v.id,v.mission_id,v.finding_id,v.source_revision_id,fr.created_at,v.thread_id,v.max_steps,v.created_at,t.status,t.pause_requested,t.auto_run,t.step_count,COALESCE(r.verdict,''),(r.verification_id IS NOT NULL),COALESCE(ve.reason,'')`
const verificationFrom = ` FROM finding_verifications v JOIN threads t ON t.id=v.thread_id JOIN finding_revisions fr ON fr.id=v.source_revision_id LEFT JOIN verification_results r ON r.verification_id=v.id LEFT JOIN verification_ends ve ON ve.verification_id=v.id `

func scanVerification(row scanner) (domain.VerificationSummary, error) {
	var v domain.VerificationSummary
	err := row.Scan(&v.ID, &v.MissionID, &v.FindingID, &v.SourceRevisionID, &v.SourceCreatedAt, &v.ThreadID, &v.MaxSteps, &v.CreatedAt, &v.Status, &v.PauseRequested, &v.AutoRun, &v.StepCount, &v.Verdict, &v.HasResult, &v.EndReason)
	return v, err
}
func verificationResult(ctx context.Context, tx pgx.Tx, id string) (*domain.VerificationResult, error) {
	var v domain.VerificationResult
	var c, e []byte
	err := tx.QueryRow(ctx, `SELECT attempt_id,call_id,created_at,conclusion,evidence FROM verification_results WHERE verification_id=$1`, id).Scan(&v.AttemptID, &v.CallID, &v.CreatedAt, &c, &e)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(c, &v.Conclusion); err != nil {
		return nil, err
	}
	if err = json.Unmarshal(e, &v.Evidence); err != nil {
		return nil, err
	}
	return &v, nil
}
func getVerification(ctx context.Context, tx pgx.Tx, mission, finding, id string) (domain.VerificationDetail, error) {
	var out domain.VerificationDetail
	var err error
	out.VerificationSummary, err = scanVerification(tx.QueryRow(ctx, `SELECT `+verificationColumns+verificationFrom+`WHERE v.id=$1 AND v.mission_id=$2 AND v.finding_id=$3`, id, mission, finding))
	if err != nil {
		return out, err
	}
	out.Thread, err = getThread(ctx, tx, out.ThreadID)
	if err != nil {
		return out, err
	}
	out.Result, err = verificationResult(ctx, tx, id)
	if err != nil {
		return out, err
	}
	err = tx.QueryRow(ctx, `SELECT COALESCE((SELECT error FROM attempts WHERE thread_id=$1 ORDER BY started_at DESC,id DESC LIMIT 1),'')`, out.ThreadID).Scan(&out.ExecutionError)
	return out, err
}
func (s *Store) GetVerification(ctx context.Context, mission, finding, id string) (domain.VerificationDetail, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return transaction(ctx, s, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) (domain.VerificationDetail, error) {
		return getVerification(ctx, tx, mission, finding, id)
	})
}
func (s *Store) ListVerifications(ctx context.Context, mission, finding string, q domain.FindingQuery) (domain.VerificationPage, error) {
	out := domain.VerificationPage{MissionID: mission, FindingID: finding, Items: []domain.VerificationSummary{}}
	q, err := domain.NormalizeFindingQuery(q)
	if err != nil || q.ThreadID != "" {
		return out, domain.ErrInvalid
	}
	before, err := decodeFindingCursor(q.Cursor, mission, "verification", finding)
	if err != nil {
		return out, err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return transaction(ctx, s, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) (domain.VerificationPage, error) {
		var owner string
		if err := tx.QueryRow(ctx, `SELECT id FROM findings WHERE id=$1 AND mission_id=$2`, finding, mission).Scan(&owner); err != nil {
			return out, err
		}
		// 仅元数据投影，不读取结果正文或候选全文。seq由固定身份保存。
		rows, err := tx.Query(ctx, `SELECT `+verificationColumns+`,v.seq`+verificationFrom+`WHERE v.mission_id=$1 AND v.finding_id=$2 AND ($3::bigint=0 OR v.seq<$3) ORDER BY v.seq DESC LIMIT $4`, mission, finding, before, q.Limit+1)
		if err != nil {
			return out, err
		}
		defer rows.Close()
		var seqs []int64
		for rows.Next() {
			var v domain.VerificationSummary
			var seq int64
			err = rows.Scan(&v.ID, &v.MissionID, &v.FindingID, &v.SourceRevisionID, &v.SourceCreatedAt, &v.ThreadID, &v.MaxSteps, &v.CreatedAt, &v.Status, &v.PauseRequested, &v.AutoRun, &v.StepCount, &v.Verdict, &v.HasResult, &v.EndReason, &seq)
			if err != nil {
				return out, err
			}
			out.Items = append(out.Items, v)
			seqs = append(seqs, seq)
		}
		if err = rows.Err(); err != nil {
			return out, err
		}
		if len(out.Items) > q.Limit {
			out.Items = out.Items[:q.Limit]
			out.NextCursor = encodeFindingCursor(mission, "verification", finding, seqs[q.Limit-1])
		}
		return out, nil
	})
}
func verificationForThread(ctx context.Context, tx pgx.Tx, thread string) (*domain.VerificationContext, error) {
	var out domain.VerificationContext
	var mission, finding, revision string
	err := tx.QueryRow(ctx, `SELECT id,mission_id,finding_id,source_revision_id,max_steps FROM finding_verifications WHERE thread_id=$1`, thread).Scan(&out.ID, &mission, &finding, &revision, &out.MaxSteps)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	out.Source, err = getFindingRevision(ctx, tx, mission, finding, revision)
	if err != nil {
		return nil, err
	}
	return &out, nil
}
func verificationFinished(ctx context.Context, tx pgx.Tx, thread string) (bool, error) {
	var done bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM finding_verifications v WHERE v.thread_id=$1 AND (EXISTS(SELECT 1 FROM verification_results r WHERE r.verification_id=v.id) OR EXISTS(SELECT 1 FROM verification_ends e WHERE e.verification_id=v.id)))`, thread).Scan(&done)
	return done, err
}

func (s *Store) ApplyVerification(ctx context.Context, attempt string, c domain.VerificationConclusion) (domain.VerificationResult, error) {
	var out domain.VerificationResult
	if !domain.ValidSkillLoadID(attempt) || domain.ValidateVerificationConclusion(c) != nil {
		return out, domain.ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	hash := sourceHash(c)
	return transaction(ctx, s, pgx.TxOptions{}, func(tx pgx.Tx) (domain.VerificationResult, error) {
		t, err := lockAttemptThread(ctx, tx, attempt)
		if err != nil {
			return out, err
		}
		var id string
		if err = tx.QueryRow(ctx, `SELECT id FROM finding_verifications WHERE thread_id=$1`, t.ID).Scan(&id); err != nil {
			return out, err
		}
		saved, err := verificationResult(ctx, tx, id)
		if err != nil {
			return out, err
		}
		if saved != nil {
			var oldHash string
			if err = tx.QueryRow(ctx, `SELECT proposal_hash FROM verification_results WHERE verification_id=$1`, id).Scan(&oldHash); err != nil {
				return out, err
			}
			if saved.AttemptID != attempt || oldHash != hash {
				return out, domain.ErrConflict
			}
			return *saved, nil
		}
		var call string
		var raw []byte
		err = tx.QueryRow(ctx, `SELECT c.id,x.assembly FROM attempts a JOIN model_calls c ON c.attempt_id=a.id JOIN model_call_contexts x ON x.call_id=c.id WHERE a.id=$1 AND a.thread_id=$2 AND a.status='running' AND c.mission_id=$3 AND c.thread_id=$2 AND c.purpose='verification' AND c.state='completed' AND c.outcome='valid_response' AND c.attempted=true`, attempt, t.ID, t.MissionID).Scan(&call, &raw)
		if err != nil {
			return out, err
		}
		var assembly domain.ContextAssembly
		if json.Unmarshal(raw, &assembly) != nil || domain.ValidateContextAssembly(&assembly) != nil || assembly.Policy != domain.VerificationContextPolicy || assembly.AttemptID != attempt || assembly.ThreadID != t.ID || assembly.MissionID != t.MissionID {
			return out, domain.ErrInvalid
		}
		pending, err := unsettledAction(ctx, tx, t.ID)
		if err != nil {
			return out, err
		}
		if pending != nil {
			return out, domain.ErrConflict
		}
		selected := map[string]domain.ContextSource{}
		for _, src := range assembly.Sources {
			if src.Included && (src.Kind == "action" || src.Kind == "observation") {
				selected[src.Kind+":"+src.ID] = src
			}
		}
		refs := []domain.FindingEvidence{}
		qualifying := false
		for _, ref := range c.Evidence {
			src, ok := selected[ref.Kind+":"+ref.ID]
			if !ok {
				return out, domain.ErrInvalid
			}
			fact, e := captureFindingEvidence(ctx, tx, ref, t.ID, assembly.AssembledAt, src.ContentSHA256)
			if e != nil {
				return out, e
			}
			if ref.Kind == "action" {
				value, e := readFindingOriginal(ctx, tx, ref, t.ID)
				if e != nil {
					return out, e
				}
				a := value.(domain.Action)
				relation := "supporting"
				if c.Verdict == "counterevidence" {
					relation = "counterevidence"
				}
				if ref.Relation == relation && domain.QualifyingVerificationAction(a) {
					qualifying = true
				}
			}
			refs = append(refs, fact)
		}
		if c.Verdict != "inconclusive" && !qualifying {
			return out, domain.ErrInvalid
		}
		body, _ := json.Marshal(c)
		evidence, _ := json.Marshal(refs)
		if _, err = tx.Exec(ctx, `INSERT INTO verification_results(verification_id,attempt_id,call_id,proposal_hash,verdict,conclusion,evidence) VALUES($1,$2,$3,$4,$5,$6,$7)`, id, attempt, call, hash, c.Verdict, body, evidence); err != nil {
			return out, err
		}
		// 结果保存即关闭后续模型意图；收尾失败/重启不会重新执行已完成验证。
		if _, err = tx.Exec(ctx, `UPDATE threads SET auto_run=false WHERE id=$1`, t.ID); err != nil {
			return out, err
		}
		if err = appendEvent(ctx, tx, t.MissionID, t.ID, "verification.result_saved", "独立验证结果已保存："+id+"；"+c.Verdict); err != nil {
			return out, err
		}
		saved, err = verificationResult(ctx, tx, id)
		if err != nil {
			return out, err
		}
		return *saved, nil
	})
}
func (s *Store) VerificationEvidence(ctx context.Context, mission, finding, id string, index int) (domain.VerificationEvidenceContent, error) {
	out := domain.VerificationEvidenceContent{MissionID: mission, FindingID: finding, VerificationID: id, Index: index}
	if index < 0 || index >= 16 {
		return out, domain.ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return transaction(ctx, s, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) (domain.VerificationEvidenceContent, error) {
		v, err := getVerification(ctx, tx, mission, finding, id)
		if err != nil {
			return out, err
		}
		if v.Result == nil || index >= len(v.Result.Evidence) {
			return out, domain.ErrNotFound
		}
		ref := v.Result.Evidence[index]
		if ref.ThreadID != v.ThreadID || v.Thread.MissionID != mission {
			return out, domain.ErrInvalid
		}
		out.Reference = ref
		out.Thread = v.Thread
		value, err := readFindingOriginal(ctx, tx, ref.FindingReference, v.ThreadID)
		if err != nil {
			return out, err
		}
		out.MatchesSnapshot = sourceHash(value) == ref.ContentSHA256
		switch x := value.(type) {
		case domain.Action:
			out.Action = &x
		case domain.Observation:
			out.Observation = &x
		}
		raw, err := json.Marshal(out)
		if err != nil || len(raw)+1 > domain.MaxFindingEvidenceBytes {
			return out, domain.ErrUnavailable
		}
		return out, nil
	})
}
