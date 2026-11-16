package store

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5"
	"wscyber/internal/domain"
)

// 仅核对固定身份/归属，不用当前活跃或后续动作状态重构过去的输入。
func insertContextAssembly(ctx context.Context, tx pgx.Tx, in domain.BeginModelCall) error {
	a := in.Assembly
	if a == nil {
		return nil
	} // 旧调用/独立适配器没有记录；读取不得补造。
	c := in.Call
	if (c.Purpose != "research" && c.Purpose != "verification") || (c.Purpose == "verification") != (a.Policy == domain.VerificationContextPolicy) || a.MissionID != c.MissionID || a.ThreadID != c.ThreadID || a.AttemptID != c.AttemptID {
		return domain.ErrInvalid
	}
	if err := domain.ValidateContextAssembly(a); err != nil {
		return err
	}
	skills, supplements := map[string]bool{}, map[string]bool{}
	for _, v := range a.Sources {
		var query string
		args := []any{v.ID, a.MissionID, a.ThreadID}
		switch v.Kind {
		case "mission", "research_brief", "safety_prompt":
			if v.ID != a.MissionID {
				return domain.ErrInvalid
			}
			if v.Kind == "mission" {
				continue
			}
			if v.Kind == "research_brief" {
				query = `SELECT EXISTS(SELECT 1 FROM mission_research_briefs WHERE mission_id=$1 AND (content::jsonb->>'share_with_model')='true' AND content_sha256=$2)`
			} else {
				query = `SELECT EXISTS(SELECT 1 FROM mission_research_configs WHERE mission_id=$1 AND safety_prompt_enabled)`
			}
			args = args[:1]
			if v.Kind == "research_brief" {
				args = append(args, v.ContentSHA256)
			}
		case "thread":
			if v.ID != a.ThreadID {
				return domain.ErrInvalid
			}
			continue
		case "observation", "action", "attempt":
			table := map[string]string{"observation": "observations", "action": "actions", "attempt": "attempts"}[v.Kind]
			query = `SELECT EXISTS(SELECT 1 FROM ` + table + ` x JOIN threads t ON t.id=x.thread_id WHERE x.id=$1 AND t.mission_id=$2 AND t.id=$3)`
		case "verification_source":
			query = `SELECT EXISTS(SELECT 1 FROM finding_verifications v JOIN finding_revisions r ON r.id=v.source_revision_id WHERE r.id=$1 AND v.mission_id=$2 AND v.thread_id=$3 AND r.content_sha256=$4)`
			args = append(args, v.ContentSHA256)
		case "finding":
			query = `SELECT EXISTS(SELECT 1 FROM finding_revisions r JOIN findings f ON f.id=r.finding_id WHERE r.id=$1 AND f.mission_id=$2 AND f.thread_id=$3 AND r.content_sha256=$4)`
			args = append(args, v.ContentSHA256)
		case "material":
			query = `SELECT EXISTS(SELECT 1 FROM mission_materials WHERE item_id=$1 AND mission_id=$2 AND content_sha256=$3)`
			args[2] = v.ContentSHA256
		case "skill":
			skills[v.ID] = true
			query = `SELECT EXISTS(SELECT 1 FROM skill_loads WHERE load_id=$1 AND mission_id=$2 AND thread_id=$3 AND content_sha256=$4)`
			args = append(args, v.ContentSHA256)
		case "skill_activity":
			query = `SELECT EXISTS(SELECT 1 FROM skill_activities WHERE attempt_id=$1 AND mission_id=$2 AND thread_id=$3)`
		case "supplement":
			supplements[v.ID] = true
			query = `SELECT EXISTS(SELECT 1 FROM research_supplements WHERE id=$1 AND mission_id=$2 AND thread_id=$3 AND content_sha256=$4)`
			args = append(args, v.ContentSHA256)
		default:
			return domain.ErrInvalid
		}
		var exists bool
		if err := tx.QueryRow(ctx, query, args...).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return domain.ErrInvalid
		}
	}
	if len(skills) != len(in.SkillLoadIDs) || len(supplements) != len(in.SupplementIDs) {
		return domain.ErrInvalid
	}
	for _, id := range in.SkillLoadIDs {
		if !skills[id] {
			return domain.ErrInvalid
		}
	}
	for _, id := range in.SupplementIDs {
		if !supplements[id] {
			return domain.ErrInvalid
		}
	}
	raw, err := json.Marshal(a)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO model_call_contexts(call_id,assembly) VALUES($1,$2)`, c.ID, raw)
	return err
}
