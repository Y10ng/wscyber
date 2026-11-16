package store

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"wscyber/internal/domain"
	"wscyber/internal/modelconfig"
	"wscyber/internal/modelprovider"
)

const modelCallColumns = `id,source,COALESCE(model_config_id,''),config_name,protocol,model,purpose,COALESCE(mission_id,''),COALESCE(thread_id,''),COALESCE(attempt_id,''),COALESCE(test_id,''),started_at,finished_at,duration_ms,state,outcome,attempted,http_status,record_model_content,content_state,usage,cost`

func scanModelCall(row scanner) (domain.ModelCall, error) {
	var c domain.ModelCall
	var usage, cost []byte
	err := row.Scan(&c.ID, &c.Source, &c.ModelConfigID, &c.ConfigName, &c.Protocol, &c.Model, &c.Purpose, &c.MissionID, &c.ThreadID, &c.AttemptID, &c.TestID, &c.StartedAt, &c.FinishedAt, &c.DurationMS, &c.State, &c.Outcome, &c.Attempted, &c.HTTPStatus, &c.RecordModelContent, &c.ContentState, &usage, &cost)
	if err == nil {
		err = json.Unmarshal(usage, &c.Usage)
	}
	if err == nil {
		err = json.Unmarshal(cost, &c.Cost)
	}
	return c, err
}
func (s *Store) GetModelRecording(ctx context.Context, id string) (domain.ModelRecording, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	v := domain.ModelRecording{MissionID: id}
	err := s.pool.QueryRow(ctx, `SELECT record_model_content FROM missions WHERE id=$1`, id).Scan(&v.RecordModelContent)
	return v, databaseError(err)
}
func (s *Store) BeginModelCall(ctx context.Context, in domain.BeginModelCall) (domain.ModelCall, error) {
	var zero domain.ModelCall
	c := in.Call
	if len(in.SkillLoadIDs) > domain.MaxActiveSkills || (c.Purpose != "research" && c.Purpose != "verification" && len(in.SkillLoadIDs) > 0) {
		return zero, domain.ErrInvalid
	}
	seenSkills := map[string]bool{}
	for _, id := range in.SkillLoadIDs {
		if !domain.ValidSkillLoadID(id) || seenSkills[id] {
			return zero, domain.ErrInvalid
		}
		seenSkills[id] = true
	}
	if len(in.SupplementIDs) > domain.MaxSupplements || (c.Purpose != "research" && c.Purpose != "verification" && len(in.SupplementIDs) > 0) {
		return zero, domain.ErrInvalid
	}
	seenSupplements := map[string]bool{}
	for _, id := range in.SupplementIDs {
		if !domain.ValidSkillLoadID(id) || seenSupplements[id] {
			return zero, domain.ErrInvalid
		}
		seenSupplements[id] = true
	}
	if !modelconfig.ValidID(c.ID) {
		return zero, domain.ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return transaction(ctx, s, pgx.TxOptions{}, func(tx pgx.Tx) (domain.ModelCall, error) {
		if c.Purpose == "research" || c.Purpose == "verification" {
			var verification bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM finding_verifications WHERE thread_id=$1)`, c.ThreadID).Scan(&verification); err != nil {
				return zero, err
			}
			if verification != (c.Purpose == "verification") || verification && (in.Assembly == nil || in.Assembly.Policy != domain.VerificationContextPolicy) {
				return zero, domain.ErrInvalid
			}
			var record bool
			var bindingSource, bindingID string
			var revision int64
			err := tx.QueryRow(ctx, `SELECT m.record_model_content,COALESCE(b.source,'environment'),COALESCE(b.config_id,''),COALESCE(b.config_revision,0)
    FROM attempts a JOIN threads t ON t.id=a.thread_id JOIN missions m ON m.id=t.mission_id LEFT JOIN mission_models b ON b.mission_id=m.id
    WHERE a.id=$1 AND t.id=$2 AND m.id=$3 AND a.status='running'`, c.AttemptID, c.ThreadID, c.MissionID).Scan(&record, &bindingSource, &bindingID, &revision)
			if err != nil {
				return zero, err
			}
			if record != c.RecordModelContent || bindingSource != c.Source || bindingID != c.ModelConfigID || revision != in.ConfigRevision {
				return zero, domain.ErrConflict
			}
		} else if c.Purpose == "connectivity_test" {
			var exists bool
			err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM model_tests WHERE id=$1 AND config_id=$2 AND config_revision=$3 AND status='running')`, c.TestID, c.ModelConfigID, in.ConfigRevision).Scan(&exists)
			if err != nil {
				return zero, err
			}
			if !exists || c.Source != "managed" || c.RecordModelContent {
				return zero, domain.ErrConflict
			}
		} else {
			return zero, domain.ErrInvalid
		}
		if c.RecordModelContent != (in.Input != nil) {
			return zero, domain.ErrInvalid
		}
		if in.Input != nil && in.Input.Kind != "input" {
			return zero, domain.ErrInvalid
		}
		c.Cost = domain.ModelCost{Status: "unpriced"}
		var priceID string
		var terms []byte
		err := tx.QueryRow(ctx, `SELECT id,terms FROM model_prices WHERE source=$1 AND model_config_id=$2 AND protocol=$3 AND model=$4 AND enabled`, c.Source, c.ModelConfigID, c.Protocol, c.Model).Scan(&priceID, &terms)
		if err == nil {
			var t domain.PriceTerms
			if err = json.Unmarshal(terms, &t); err != nil {
				return zero, err
			}
			c.Cost = domain.ModelCost{Status: "usage_missing", Currency: &t.Currency, PriceID: &priceID, PriceSnapshot: &t}
		} else if err != pgx.ErrNoRows {
			return zero, err
		}
		c.Usage = modelprovider.MissingUsage()
		usage, _ := json.Marshal(c.Usage)
		cost, _ := json.Marshal(c.Cost)
		c.ContentState = "disabled"
		if c.RecordModelContent {
			c.ContentState = "pending"
		}
		c, err = scanModelCall(tx.QueryRow(ctx, `INSERT INTO model_calls(id,source,model_config_id,config_revision,config_name,protocol,model,purpose,mission_id,thread_id,attempt_id,test_id,state,outcome,record_model_content,content_state,usage,cost)
   VALUES($1,$2,NULLIF($3,''),NULLIF($4,0),$5,$6,$7,$8,NULLIF($9,''),NULLIF($10,''),NULLIF($11,''),NULLIF($12,''),'running','pending',$13,$14,$15,$16) RETURNING `+modelCallColumns,
			c.ID, c.Source, c.ModelConfigID, in.ConfigRevision, c.ConfigName, c.Protocol, c.Model, c.Purpose, c.MissionID, c.ThreadID, c.AttemptID, c.TestID, c.RecordModelContent, c.ContentState, usage, cost))
		if err != nil {
			return zero, err
		}
		for _, loadID := range in.SkillLoadIDs {
			var owner string
			err = tx.QueryRow(ctx, `SELECT load_id FROM skill_loads WHERE load_id=$1 AND mission_id=$2 AND thread_id=$3 AND attempt_id<>$4`, loadID, c.MissionID, c.ThreadID, c.AttemptID).Scan(&owner)
			if err != nil {
				return zero, err
			}
			if _, err = tx.Exec(ctx, `INSERT INTO model_call_skills(call_id,load_id) VALUES($1,$2)`, c.ID, loadID); err != nil {
				return zero, err
			}
		}
		for _, id := range in.SupplementIDs {
			var owner string
			if err = tx.QueryRow(ctx, `SELECT id FROM research_supplements WHERE id=$1 AND mission_id=$2 AND thread_id=$3`, id, c.MissionID, c.ThreadID).Scan(&owner); err != nil {
				return zero, err
			}
			if _, err = tx.Exec(ctx, `INSERT INTO model_call_supplements(call_id,supplement_id) VALUES($1,$2)`, c.ID, id); err != nil {
				return zero, err
			}
		}
		if err = insertContextAssembly(ctx, tx, in); err != nil {
			return zero, err
		}
		if in.Input != nil {
			if err = insertCallContent(ctx, tx, c.ID, *in.Input); err != nil {
				return zero, err
			}
		}
		return c, nil
	})
}
func insertCallContent(ctx context.Context, tx pgx.Tx, id string, c domain.SealedCallContent) error {
	_, err := tx.Exec(ctx, `INSERT INTO model_call_contents(call_id,kind,ciphertext,media_type,complete,redacted) VALUES($1,$2,$3,$4,$5,$6)`, id, c.Kind, c.Ciphertext, c.MediaType, c.Complete, c.Redacted)
	return err
}
func (s *Store) FinishModelCall(ctx context.Context, in domain.FinishModelCall) error {
	if !modelconfig.ValidID(in.ID) || in.FinishedAt.IsZero() || in.DurationMS < 0 {
		return domain.ErrInvalid
	}
	switch in.Outcome {
	case "not_sent", "transport_unknown", "http_error", "invalid_response", "valid_response":
	default:
		return domain.ErrInvalid
	}
	if (in.Outcome == "not_sent") == in.Attempted {
		return domain.ErrInvalid
	}
	if in.Response != nil && in.Response.Kind != "response" {
		return domain.ErrInvalid
	}
	raw, err := json.Marshal(in)
	if err != nil {
		return domain.ErrInvalid
	}
	hash := fmt.Sprintf("%x", sha256.Sum256(raw))
	return s.mutate(ctx, func(tx pgx.Tx) error {
		var state string
		var oldHash *string
		var record bool
		var snapshot []byte
		err := tx.QueryRow(ctx, `SELECT state,finish_hash,record_model_content,cost FROM model_calls WHERE id=$1 FOR UPDATE`, in.ID).Scan(&state, &oldHash, &record, &snapshot)
		if err != nil {
			return err
		}
		if state != "running" {
			if state == "completed" && oldHash != nil && *oldHash == hash {
				return nil
			}
			return domain.ErrConflict
		}
		if !record && in.Response != nil {
			return domain.ErrInvalid
		}
		var original domain.ModelCost
		if json.Unmarshal(snapshot, &original) != nil {
			return domain.ErrInvalid
		}
		a, _ := json.Marshal(original.PriceSnapshot)
		b, _ := json.Marshal(in.Cost.PriceSnapshot)
		if string(a) != string(b) || !sameString(original.PriceID, in.Cost.PriceID) || !sameString(original.Currency, in.Cost.Currency) {
			return domain.ErrConflict
		}
		contentState := "disabled"
		if record {
			contentState = "partial"
			if in.Response != nil && in.Response.Complete {
				contentState = "available"
			}
		}
		if in.Response != nil {
			if err = insertCallContent(ctx, tx, in.ID, *in.Response); err != nil {
				return err
			}
		}
		usage, _ := json.Marshal(in.Usage)
		cost, _ := json.Marshal(in.Cost)
		_, err = tx.Exec(ctx, `UPDATE model_calls SET state='completed',outcome=$2,attempted=$3,http_status=$4,finished_at=$5,duration_ms=$6,usage=$7,cost=$8,content_state=$9,finish_hash=$10 WHERE id=$1 AND state='running'`, in.ID, in.Outcome, in.Attempted, in.HTTPStatus, in.FinishedAt, in.DurationMS, usage, cost, contentState, hash)
		return err
	})
}
func sameString(a, b *string) bool {
	return (a == nil && b == nil) || (a != nil && b != nil && *a == *b)
}
func interruptModelCalls(ctx context.Context, tx pgx.Tx) error {
	_, err := tx.Exec(ctx, `UPDATE model_calls SET state='interrupted',outcome='interrupted',finished_at=clock_timestamp(),content_state=CASE WHEN record_model_content THEN 'partial' ELSE 'disabled' END WHERE state='running'`)
	return err
}
func (s *Store) GetModelCall(ctx context.Context, id, mission string) (domain.ModelCall, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	c, err := scanModelCall(s.pool.QueryRow(ctx, `SELECT `+modelCallColumns+` FROM model_calls WHERE id=$1 AND ($2='' OR mission_id=$2)`, id, mission))
	return c, databaseError(err)
}
func (s *Store) GetSealedCallContent(ctx context.Context, id string) ([]domain.SealedCallContent, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	rows, err := s.pool.Query(ctx, `SELECT kind,ciphertext,media_type,complete,redacted FROM model_call_contents WHERE call_id=$1 ORDER BY kind`, id)
	if err != nil {
		return nil, databaseError(err)
	}
	defer rows.Close()
	out := []domain.SealedCallContent{}
	for rows.Next() {
		var c domain.SealedCallContent
		if err = rows.Scan(&c.Kind, &c.Ciphertext, &c.MediaType, &c.Complete, &c.Redacted); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
