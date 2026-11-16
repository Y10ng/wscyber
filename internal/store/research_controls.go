package store

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"wscyber/internal/domain"
)

func researchPath(mission, thread, kind string) string {
	return "/api/missions/" + mission + "/threads/" + thread + "/" + kind
}

// 原键互斥、原响应与业务共事务。重放优先于实时能力/状态检查，不再次派发。
func (s *Store) researchWrite(ctx context.Context, key, mission, thread, kind string, input any, apply func(context.Context, pgx.Tx) (domain.ResearchWriteReceipt, error)) (domain.ResearchWriteReceipt, error) {
	var zero domain.ResearchWriteReceipt
	if !domain.ValidOperationKey(key) || !domain.ValidSkillLoadID(mission) || !domain.ValidSkillLoadID(thread) {
		return zero, domain.ErrInvalid
	}
	raw, err := json.Marshal(input)
	if err != nil {
		return zero, err
	}
	hash := fmt.Sprintf("%x", sha256.Sum256(raw))
	path := researchPath(mission, thread, kind)
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return transaction(ctx, s, pgx.TxOptions{}, func(tx pgx.Tx) (domain.ResearchWriteReceipt, error) {
		if _, err := tx.Exec(ctx, `SET LOCAL lock_timeout='2s'`); err != nil {
			return zero, err
		}
		kh := sha256.Sum256([]byte(key))
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1::bigint)`, int64(binary.BigEndian.Uint64(kh[:8]))); err != nil {
			return zero, err
		}
		var method, oldPath, oldHash string
		var response []byte
		err := tx.QueryRow(ctx, `SELECT method,path,request_hash,response FROM operations WHERE key=$1`, key).Scan(&method, &oldPath, &oldHash, &response)
		if err == nil {
			if method != "POST" || oldPath != path || oldHash != hash {
				return zero, conflict("提交键已用于不同请求，请核对原提交")
			}
			var saved domain.ResearchWriteReceipt
			err = json.Unmarshal(response, &saved)
			return saved, err
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return zero, err
		}
		t, err := lockThread(ctx, tx, thread)
		if err != nil {
			return zero, err
		}
		if t.MissionID != mission {
			return zero, domain.ErrNotFound
		}
		v, err := apply(ctx, tx)
		if err != nil {
			return zero, err
		}
		v.Key = key
		v.MissionID = mission
		v.ThreadID = thread
		resourceType, resourceID, status := "thread", thread, 200
		if v.Supplement != nil {
			resourceType = "research_supplement"
			resourceID = v.Supplement.ID
			status = 201
		}
		response, err = json.Marshal(v)
		if err != nil {
			return zero, err
		}
		_, err = tx.Exec(ctx, `INSERT INTO operations(key,method,path,request_hash,resource_type,resource_id,response_status,response) VALUES($1,'POST',$2,$3,$4,$5,$6,$7)`, key, path, hash, resourceType, resourceID, status, response)
		return v, err
	})
}
func (s *Store) ControlResearch(ctx context.Context, key, mission, thread, command string, validate func(context.Context) error) (domain.ResearchWriteReceipt, error) {
	if command != "pause" && command != "run" && command != "resume" {
		return domain.ResearchWriteReceipt{}, domain.ErrInvalid
	}
	return s.researchWrite(ctx, key, mission, thread, "control", struct {
		Command string `json:"command"`
	}{command}, func(ctx context.Context, tx pgx.Tx) (domain.ResearchWriteReceipt, error) {
		v := domain.ResearchWriteReceipt{Command: command}
		if command == "run" {
			if validate == nil {
				return v, domain.ErrUnavailable
			}
			if err := validate(ctx); err != nil {
				return v, err
			}
		}
		status := "ready"
		if command == "pause" {
			status = "paused"
		}
		t, err := setThreadControl(ctx, tx, thread, status, command == "run")
		v.Thread = &t
		return v, err
	})
}

const supplementColumns = `id,mission_id,thread_id,source,'operator_statement',octet_length(content),content_sha256,created_at`

func scanSupplement(row scanner) (domain.Supplement, error) {
	var v domain.Supplement
	err := row.Scan(&v.ID, &v.MissionID, &v.ThreadID, &v.Source, &v.Kind, &v.ByteSize, &v.ContentSHA256, &v.CreatedAt)
	return v, databaseError(err)
}
func (s *Store) AddSupplement(ctx context.Context, key, mission, thread string, in domain.SupplementInput) (domain.ResearchWriteReceipt, error) {
	if err := domain.ValidateSupplement(in); err != nil {
		return domain.ResearchWriteReceipt{}, err
	}
	return s.researchWrite(ctx, key, mission, thread, "supplements", in, func(ctx context.Context, tx pgx.Tx) (domain.ResearchWriteReceipt, error) {
		var zero domain.ResearchWriteReceipt
		var count, total int
		if err := tx.QueryRow(ctx, `SELECT count(*),COALESCE(sum(octet_length(content)),0) FROM research_supplements WHERE thread_id=$1`, thread).Scan(&count, &total); err != nil {
			return zero, err
		}
		if count >= domain.MaxSupplements || total+len(in.Content) > domain.MaxSupplementTotalBytes {
			return zero, conflict("方向人工补充容量已满；原件保留，不截断或替换")
		}
		id, err := newID()
		if err != nil {
			return zero, err
		}
		v, err := scanSupplement(tx.QueryRow(ctx, `INSERT INTO research_supplements(id,mission_id,thread_id,source,content,content_sha256) VALUES($1,$2,$3,$4,$5,$6) RETURNING `+supplementColumns, id, mission, thread, in.Source, in.Content, fmt.Sprintf("%x", sha256.Sum256([]byte(in.Content)))))
		if err != nil {
			return zero, err
		}
		// JSON转义也有上限；与模型装配采用同一完整集合，不静默截断。
		if _, err = supplementContext(ctx, tx, mission, thread); err != nil {
			return zero, err
		}
		err = appendEvent(ctx, tx, mission, thread, "research.supplement_added", "已保存人工补充："+id+"；用户声明，尚不代表模型已调用或采用")
		return domain.ResearchWriteReceipt{Command: "supplement", Supplement: &v}, err
	})
}
func checkResearchOwner(ctx context.Context, db querier, mission, thread string) error {
	t, err := getThread(ctx, db, thread)
	if err != nil {
		return err
	}
	if t.MissionID != mission {
		return domain.ErrNotFound
	}
	return nil
}
func (s *Store) ListSupplements(ctx context.Context, mission, thread string) (domain.SupplementPage, error) {
	v := domain.SupplementPage{MissionID: mission, ThreadID: thread, Items: []domain.Supplement{}}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := checkResearchOwner(ctx, s.pool, mission, thread); err != nil {
		return v, err
	}
	var err error
	v.Items, err = collect(ctx, s.pool, `SELECT `+supplementColumns+` FROM research_supplements WHERE mission_id=$1 AND thread_id=$2 ORDER BY created_at,id LIMIT 16`, scanSupplement, mission, thread)
	return v, err
}
func (s *Store) GetSupplement(ctx context.Context, mission, thread, id string) (domain.Supplement, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var v domain.Supplement
	err := s.pool.QueryRow(ctx, `SELECT `+supplementColumns+`,content FROM research_supplements WHERE id=$1 AND mission_id=$2 AND thread_id=$3`, id, mission, thread).Scan(&v.ID, &v.MissionID, &v.ThreadID, &v.Source, &v.Kind, &v.ByteSize, &v.ContentSHA256, &v.CreatedAt, &v.Content)
	return v, databaseError(err)
}
func supplementContext(ctx context.Context, db querier, mission, thread string) ([]domain.Supplement, error) {
	if err := checkResearchOwner(ctx, db, mission, thread); err != nil {
		return nil, err
	}
	items, err := collect(ctx, db, `SELECT `+supplementColumns+`,content FROM research_supplements WHERE mission_id=$1 AND thread_id=$2 ORDER BY created_at,id LIMIT 17`, func(row scanner) (domain.Supplement, error) {
		var v domain.Supplement
		err := row.Scan(&v.ID, &v.MissionID, &v.ThreadID, &v.Source, &v.Kind, &v.ByteSize, &v.ContentSHA256, &v.CreatedAt, &v.Content)
		return v, err
	}, mission, thread)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(items)
	if err != nil {
		return nil, err
	}
	if len(items) > domain.MaxSupplements || len(raw) > 98304 {
		return nil, conflict("人工补充上下文超出容量；未发送模型")
	}
	return items, nil
}
func (s *Store) GetSupplementContext(ctx context.Context, mission, thread string) ([]domain.Supplement, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return supplementContext(ctx, s.pool, mission, thread)
}
func (s *Store) SupplementModelCalls(ctx context.Context, mission, thread, id, after string, limit int) (domain.SupplementCalls, error) {
	v := domain.SupplementCalls{MissionID: mission, ThreadID: thread, SupplementID: id, Items: []domain.ModelCall{}}
	if !validSkillPage(after, limit) {
		return v, domain.ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var owner string
	if err := s.pool.QueryRow(ctx, `SELECT id FROM research_supplements WHERE id=$1 AND mission_id=$2 AND thread_id=$3`, id, mission, thread).Scan(&owner); err != nil {
		return v, databaseError(err)
	}
	var err error
	v.Items, err = collect(ctx, s.pool, `SELECT `+modelCallColumns+` FROM model_calls WHERE mission_id=$1 AND thread_id=$2 AND id>$3 AND id IN(SELECT call_id FROM model_call_supplements WHERE supplement_id=$4) ORDER BY id LIMIT $5`, scanModelCall, mission, thread, after, id, limit+1)
	if len(v.Items) > limit {
		v.Items = v.Items[:limit]
		v.NextCursor = v.Items[limit-1].ID
	}
	return v, err
}
