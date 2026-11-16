package store

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"wscyber/internal/domain"
)

var _ domain.SkillRepository = (*Store)(nil)

const skillActivityColumns = `seq::text,attempt_id,mission_id,thread_id,kind,request,result,created_at`
const skillLoadColumns = `load_id,mission_id,thread_id,attempt_id,purpose,created_at,item_id,source_token,name,path,description,format,content_sha256,byte_size,content`

func scanSkillActivity(row scanner) (domain.SkillActivity, error) {
	var a domain.SkillActivity
	var request, result []byte
	err := row.Scan(&a.Sequence, &a.AttemptID, &a.MissionID, &a.ThreadID, &a.Kind, &request, &result, &a.CreatedAt)
	if err == nil {
		err = json.Unmarshal(request, &a.Request)
	}
	if err == nil {
		err = json.Unmarshal(result, &a.Result)
	}
	return a, databaseError(err)
}
func scanSkillLoad(row scanner) (domain.SkillLoad, error) {
	var l domain.SkillLoad
	err := row.Scan(&l.LoadID, &l.MissionID, &l.ThreadID, &l.AttemptID, &l.Purpose, &l.CreatedAt, &l.ID, &l.SourceToken, &l.Name, &l.Path, &l.Description, &l.Format, &l.ContentSHA256, &l.ByteSize, &l.Content)
	if err != nil {
		return l, databaseError(err)
	}
	if l.ByteSize != len(l.Content) || l.ByteSize < 1 || l.ByteSize > domain.MaxMaterialBytes || l.ContentSHA256 != fmt.Sprintf("%x", sha256.Sum256([]byte(l.Content))) {
		return domain.SkillLoad{}, fmt.Errorf("%w：Skill固定原件完整性失败", domain.ErrUnavailable)
	}
	return l, nil
}
func readActiveSkills(ctx context.Context, q querier, mission, thread string) ([]domain.SkillLoad, error) {
	active, err := collect(ctx, q, `SELECT `+skillLoadColumns+` FROM skill_loads WHERE mission_id=$1 AND thread_id=$2 AND load_id IN (SELECT load_id FROM thread_skill_context WHERE thread_id=$2) ORDER BY load_id LIMIT 5`, scanSkillLoad, mission, thread)
	if err != nil {
		return nil, err
	}
	if !skillsFit(active) {
		return nil, fmt.Errorf("%w：活跃Skill上下文超过上限", domain.ErrUnavailable)
	}
	return active, nil
}
func skillsFit(active []domain.SkillLoad) bool {
	total := 0
	for _, l := range active {
		total += l.ByteSize
	}
	raw, err := json.Marshal(active)
	return err == nil && len(active) <= domain.MaxActiveSkills && total <= domain.MaxActiveSkillBytes && len(raw) <= domain.MaxSerializedSkills
}

// 方向锁→库锁；先回放同一逻辑活动，再验证在途身份。正文与成功结果/活跃引用同事务。
func (s *Store) ApplySkillActivity(ctx context.Context, attemptID, kind string, request domain.SkillRequest) (domain.SkillActivity, error) {
	var zero domain.SkillActivity
	if !domain.ValidSkillLoadID(attemptID) {
		return zero, domain.ErrInvalid
	}
	if err := domain.ValidateSkillRequest(kind, request); err != nil {
		return zero, err
	}
	raw, _ := json.Marshal(struct {
		Kind    string
		Request domain.SkillRequest
	}{kind, request})
	hash := fmt.Sprintf("%x", sha256.Sum256(raw))
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return transaction(ctx, s, pgx.TxOptions{}, func(tx pgx.Tx) (domain.SkillActivity, error) {
		if _, err := tx.Exec(ctx, `SET LOCAL lock_timeout='2s'`); err != nil {
			return zero, err
		}
		t, err := lockAttemptThread(ctx, tx, attemptID)
		if err != nil {
			return zero, err
		}
		var oldHash string
		err = tx.QueryRow(ctx, `SELECT request_hash FROM skill_activities WHERE attempt_id=$1`, attemptID).Scan(&oldHash)
		if err == nil {
			if oldHash != hash {
				return zero, domain.ErrConflict
			}
			return scanSkillActivity(tx.QueryRow(ctx, `SELECT `+skillActivityColumns+` FROM skill_activities WHERE attempt_id=$1`, attemptID))
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return zero, err
		}
		var status string
		if err = tx.QueryRow(ctx, `SELECT status FROM attempts WHERE id=$1`, attemptID).Scan(&status); err != nil {
			return zero, err
		}
		if status != "running" || t.Status != "running" {
			return zero, domain.ErrConflict
		}
		activity := domain.SkillActivity{AttemptID: attemptID, MissionID: t.MissionID, ThreadID: t.ID, Kind: kind, Request: request}
		var loaded *domain.SkillLoad
		switch kind {
		case "skill_discover":
			limit := request.Limit
			if limit == 0 {
				limit = 10
			}
			items, err := collect(ctx, tx, `SELECT id,token,name,description,byte_size,content_sha256 FROM library_items WHERE classification='skill' AND kind='file' AND deleted_at IS NULL AND id>$1 AND ($2='' OR position(lower($2) in lower(name||' '||description))>0) ORDER BY id LIMIT $3`, func(row scanner) (domain.SkillDescriptor, error) {
				var d domain.SkillDescriptor
				err := row.Scan(&d.ID, &d.Token, &d.Name, &d.Description, &d.ByteSize, &d.ContentSHA256)
				return d, err
			}, request.After, request.Query, limit+1)
			if err != nil {
				return zero, err
			}
			activity.Result = domain.SkillResult{Status: "discovered", Items: items}
			if len(items) > limit {
				activity.Result.Items = items[:limit]
				activity.Result.NextCursor = items[limit-1].ID
			}
		case "skill_load":
			if err := lockLibrary(ctx, tx); err != nil {
				return zero, err
			}
			// 先查显式可用范围；普通/删除/不存在统一结果，绝不读取它们正文。
			var token string
			err = tx.QueryRow(ctx, `SELECT token FROM library_items WHERE id=$1 AND kind='file' AND classification='skill' AND deleted_at IS NULL`, request.ItemID).Scan(&token)
			switch {
			case errors.Is(err, pgx.ErrNoRows):
				activity.Result.Status = "unavailable"
			case err != nil:
				return zero, err
			case token != request.SourceToken:
				activity.Result.Status = "stale"
			default:
				active, err := readActiveSkills(ctx, tx, t.MissionID, t.ID)
				if err != nil {
					return zero, err
				}
				selected, err := selectTaskMaterials(ctx, tx, []domain.MaterialSelection{{ID: request.ItemID, Token: token}})
				if err != nil {
					return zero, err
				}
				m := selected[0]
				if m.ByteSize != len(m.Content) || m.ByteSize < 1 || m.ByteSize > domain.MaxMaterialBytes || m.ContentSHA256 != fmt.Sprintf("%x", sha256.Sum256([]byte(m.Content))) {
					return zero, domain.ErrUnavailable
				}
				l := domain.SkillLoad{LoadID: attemptID, MissionID: t.MissionID, ThreadID: t.ID, AttemptID: attemptID, Purpose: request.Purpose, TaskMaterial: m}
				if err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&l.CreatedAt); err != nil {
					return zero, err
				}
				if !skillsFit(append(active, l)) {
					activity.Result.Status = "capacity"
				} else {
					loaded = &l
					activity.Result = domain.SkillResult{Status: "loaded", LoadID: l.LoadID, Material: &l.MaterialReference}
				}
			}
		case "skill_release":
			tag, err := tx.Exec(ctx, `DELETE FROM thread_skill_context WHERE thread_id=$1 AND load_id=$2`, t.ID, request.LoadID)
			if err != nil {
				return zero, err
			}
			activity.Result = domain.SkillResult{Status: "not_active", LoadID: request.LoadID}
			if tag.RowsAffected() == 1 {
				activity.Result.Status = "released"
			}
		}
		req, _ := json.Marshal(request)
		result, _ := json.Marshal(activity.Result)
		activity, err = scanSkillActivity(tx.QueryRow(ctx, `INSERT INTO skill_activities(attempt_id,mission_id,thread_id,kind,request_hash,request,result) VALUES($1,$2,$3,$4,$5,$6,$7) RETURNING `+skillActivityColumns, attemptID, t.MissionID, t.ID, kind, hash, req, result))
		if err != nil {
			return zero, err
		}
		if loaded != nil {
			l := loaded
			_, err = tx.Exec(ctx, `INSERT INTO skill_loads(`+skillLoadColumns+`) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`, l.LoadID, l.MissionID, l.ThreadID, l.AttemptID, l.Purpose, l.CreatedAt, l.ID, l.SourceToken, l.Name, l.Path, l.Description, l.Format, l.ContentSHA256, l.ByteSize, l.Content)
			if err != nil {
				return zero, err
			}
			if _, err = tx.Exec(ctx, `INSERT INTO thread_skill_context(load_id,thread_id) VALUES($1,$2)`, l.LoadID, l.ThreadID); err != nil {
				return zero, err
			}
		}
		if err = appendEvent(ctx, tx, t.MissionID, t.ID, "skill.activity", kind+"："+activity.Result.Status+"；"+attemptID); err != nil {
			return zero, err
		}
		return activity, nil
	})
}
func (s *Store) GetSkillContext(ctx context.Context, mission, thread string) (domain.SkillContext, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return transaction(ctx, s, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) (domain.SkillContext, error) {
		var out domain.SkillContext
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM threads WHERE id=$1 AND mission_id=$2)`, thread, mission).Scan(&exists); err != nil {
			return out, err
		}
		if !exists {
			return out, domain.ErrNotFound
		}
		var err error
		out.Active, err = readActiveSkills(ctx, tx, mission, thread)
		if err != nil {
			return out, err
		}
		a, err := scanSkillActivity(tx.QueryRow(ctx, `SELECT `+skillActivityColumns+` FROM skill_activities WHERE mission_id=$1 AND thread_id=$2 ORDER BY skill_activities.seq DESC LIMIT 1`, mission, thread))
		if errors.Is(err, domain.ErrNotFound) {
			return out, nil
		}
		if err != nil {
			return out, err
		}
		out.LastActivity = &a
		return out, nil
	})
}
func validSkillPage(after string, limit int) bool {
	return (after == "" || domain.ValidLibraryID(after)) && limit >= 1 && limit <= 100
}
func ensureSkillMission(ctx context.Context, q querier, mission string) error {
	var id string
	return databaseError(q.QueryRow(ctx, `SELECT id FROM missions WHERE id=$1`, mission).Scan(&id))
}
func (s *Store) SkillUsage(ctx context.Context, mission, after string, limit int) (domain.SkillUsagePage, error) {
	zero := domain.SkillUsagePage{MissionID: mission, Items: []domain.SkillUsageItem{}}
	if !validSkillPage(after, limit) {
		return zero, domain.ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return transaction(ctx, s, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) (domain.SkillUsagePage, error) {
		out := zero
		if err := ensureSkillMission(ctx, tx, mission); err != nil {
			return out, err
		}
		if err := tx.QueryRow(ctx, `SELECT count(*)::text FROM skill_loads WHERE mission_id=$1`, mission).Scan(&out.TotalSuccessfulLoads); err != nil {
			return out, err
		}
		items, err := collect(ctx, tx, `SELECT DISTINCT ON (item_id) item_id,name,(count(*) OVER (PARTITION BY item_id))::text FROM skill_loads WHERE mission_id=$1 AND item_id>$2 ORDER BY item_id,created_at DESC,load_id DESC LIMIT $3`, func(row scanner) (domain.SkillUsageItem, error) {
			var v domain.SkillUsageItem
			err := row.Scan(&v.ItemID, &v.Name, &v.SuccessfulLoads)
			return v, err
		}, mission, after, limit+1)
		if err != nil {
			return out, err
		}
		out.Items = items
		if len(items) > limit {
			out.Items = items[:limit]
			out.NextCursor = out.Items[limit-1].ItemID
		}
		return out, nil
	})
}
func (s *Store) ListSkillActivities(ctx context.Context, mission, after string, limit int) (domain.SkillActivityPage, error) {
	out := domain.SkillActivityPage{MissionID: mission, Items: []domain.SkillActivity{}}
	seq := int64(0)
	var err error
	if after != "" {
		seq, err = strconv.ParseInt(after, 10, 64)
	}
	if err != nil || seq < 0 || limit < 1 || limit > 100 || len(after) > 19 {
		return out, domain.ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err = ensureSkillMission(ctx, s.pool, mission); err != nil {
		return out, err
	}
	out.Items, err = collect(ctx, s.pool, `SELECT `+skillActivityColumns+` FROM skill_activities WHERE mission_id=$1 AND seq>$2 ORDER BY skill_activities.seq LIMIT $3`, scanSkillActivity, mission, seq, limit+1)
	if err == nil && len(out.Items) > limit {
		out.Items = out.Items[:limit]
		out.NextCursor = out.Items[limit-1].Sequence
	}
	return out, err
}
func (s *Store) GetSkillLoad(ctx context.Context, mission, id string) (domain.SkillLoad, error) {
	if !domain.ValidSkillLoadID(id) {
		return domain.SkillLoad{}, domain.ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return scanSkillLoad(s.pool.QueryRow(ctx, `SELECT `+skillLoadColumns+` FROM skill_loads WHERE mission_id=$1 AND load_id=$2`, mission, id))
}
func (s *Store) SkillModelCalls(ctx context.Context, mission, load, after string, limit int) (domain.SkillCallsPage, error) {
	zero := domain.SkillCallsPage{MissionID: mission, LoadID: load, Items: []domain.ModelCall{}}
	if !domain.ValidSkillLoadID(load) || !validSkillPage(after, limit) {
		return zero, domain.ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return transaction(ctx, s, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) (domain.SkillCallsPage, error) {
		out := zero
		var id string
		if err := tx.QueryRow(ctx, `SELECT load_id FROM skill_loads WHERE load_id=$1 AND mission_id=$2`, load, mission).Scan(&id); err != nil {
			return out, err
		}
		var err error
		out.Items, err = collect(ctx, tx, `SELECT `+modelCallColumns+` FROM model_calls WHERE mission_id=$1 AND id>$2 AND id IN (SELECT call_id FROM model_call_skills WHERE load_id=$3) ORDER BY id LIMIT $4`, scanModelCall, mission, after, load, limit+1)
		if err == nil && len(out.Items) > limit {
			out.Items = out.Items[:limit]
			out.NextCursor = out.Items[limit-1].ID
		}
		return out, err
	})
}
