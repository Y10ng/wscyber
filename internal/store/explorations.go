package store

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"wscyber/internal/domain"
	"wscyber/internal/modelconfig"
)

const explorationPath = "/api/explorations"

// StartExploration 不调用模型/执行器。auto_run 是持久启动意图，ClaimThread 才创建实际 Attempt。
func (s *Store) StartExploration(ctx context.Context, key string, in domain.ExplorationInput, modelConfigured bool) (domain.ExplorationStart, error) {
	return s.StartExplorationWithModel(ctx, key, in, domain.ExplorationModelOptions{EnvironmentConfigured: modelConfigured})
}

func (s *Store) StartExplorationWithModel(ctx context.Context, key string, in domain.ExplorationInput, options domain.ExplorationModelOptions) (domain.ExplorationStart, error) {
	var zero domain.ExplorationStart
	in.ResearchBrief = in.ResearchBrief.Clone()
	if in.ModelConfigID != "" && !modelconfig.ValidID(in.ModelConfigID) {
		return zero, invalid("模型配置ID必须为32位小写十六进制")
	}
	if !domain.ValidOperationKey(key) || strings.TrimSpace(in.Description) == "" || len(in.Description) > 64000 || strings.TrimSpace(in.Goal) == "" || len(in.Goal) > 32000 || len(in.AllowedOrigins) == 0 || len(in.AllowedOrigins) > 64 {
		return zero, invalid("请提供有效提交键、目标材料、研究目标和明确授权范围")
	}
	if err := domain.ValidateResearchSelection(in); err != nil {
		return zero, err
	}
	in.Materials = slices.Clone(in.Materials)
	slices.SortFunc(in.Materials, func(a, b domain.MaterialSelection) int { return strings.Compare(a.ID, b.ID) })
	// 授权集合顺序不改变提交身份；不修改调用者的切片。
	in.AllowedOrigins = slices.Clone(in.AllowedOrigins)
	slices.Sort(in.AllowedOrigins)
	in.AllowedOrigins = slices.Compact(in.AllowedOrigins)
	encoded, err := json.Marshal(in)
	if err != nil {
		return zero, err
	}
	fingerprint := fmt.Sprintf("%x", sha256.Sum256(encoded))
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	result, err := transaction(ctx, s, pgx.TxOptions{}, func(tx pgx.Tx) (domain.ExplorationStart, error) {
		if _, err := tx.Exec(ctx, `SET LOCAL lock_timeout = '2s'`); err != nil {
			return zero, err
		}
		// 单 bigint 锁域与实例/事件的双 int 锁域分离。碰撞最多串行等待，不混淆完整键。
		// 锁顺序：提交键 → 本事务新业务行 → 事件；主键仍是最终去重约束。
		keyHash := sha256.Sum256([]byte(key))
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1::bigint)`, int64(binary.BigEndian.Uint64(keyHash[:8]))); err != nil {
			return zero, err
		}
		var method, path, hash string
		var original []byte
		err := tx.QueryRow(ctx, `SELECT method, path, request_hash, response FROM operations WHERE key = $1`, key).Scan(&method, &path, &hash, &original)
		if err == nil {
			if method != http.MethodPost || path != explorationPath || hash != fingerprint {
				return zero, conflict("提交键已用于不同请求，请核对原提交")
			}
			var saved domain.ExplorationStart
			err = json.Unmarshal(original, &saved)
			return saved, err
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return zero, err
		}
		if in.RecordModelContent && !options.ContentRecordingAvailable {
			return zero, fmt.Errorf("%w：本机未配置模型原文加密密钥", domain.ErrUnavailable)
		}
		materials, err := selectTaskMaterials(ctx, tx, in.Materials)
		if err != nil {
			return zero, err
		}
		binding, err := selectMissionModel(ctx, tx, in.ModelConfigID, options)
		if err != nil {
			return zero, err
		}
		missionID, err := newID()
		if err != nil {
			return zero, err
		}
		threadID, err := newID()
		if err != nil {
			return zero, err
		}
		// 名称只是目标的短展示；完整输入保存在 origin/goal，不截断用户材料。
		title := []rune(strings.TrimSpace(in.Goal))
		if len(title) > 50 {
			title = append(title[:50], '…')
		}
		origins, err := json.Marshal(in.AllowedOrigins)
		if err != nil {
			return zero, err
		}
		m, err := scanMission(tx.QueryRow(ctx, `INSERT INTO missions(id, title, origin, goal, allowed_origins)
			VALUES ($1, $2, $3, $4, $5) RETURNING `+missionColumns, missionID, string(title), in.Description, in.Goal, origins))
		if err != nil {
			return zero, err
		}
		if in.RecordModelContent {
			if _, err := tx.Exec(ctx, `UPDATE missions SET record_model_content=true WHERE id=$1`, m.ID); err != nil {
				return zero, err
			}
		}
		if err := saveResearchConfiguration(ctx, tx, m.ID, in, materials); err != nil {
			return zero, err
		}
		binding.MissionID = m.ID
		if err := saveMissionModel(ctx, tx, binding); err != nil {
			return zero, err
		}
		t, err := scanThread(tx.QueryRow(ctx, `INSERT INTO threads(id, mission_id, title, question, rationale, next_step, auto_run)
			VALUES ($1, $2, '初始探索', $3, '用户明确选择开始探索；背景材料是待核验输入，不是已证实事实',
			'先读取目标与已知材料，区分观察和假设，在明确授权范围内选择下一步', true) RETURNING `+threadColumns, threadID, m.ID, in.Goal))
		if err != nil {
			return zero, err
		}
		for _, event := range []struct{ threadID, kind, message string }{
			{"", "mission.created", "研究项目已创建"},
			{t.ID, "thread.created", "已自动建立初始研究方向"},
			{t.ID, "exploration.start_requested", "启动意图已保存，等待调度；不表示已经运行"},
		} {
			if err := appendEvent(ctx, tx, m.ID, event.threadID, event.kind, event.message); err != nil {
				return zero, err
			}
		}
		started := domain.ExplorationStart{Mission: m, Thread: t, StartStatus: "accepted"}
		response, err := json.Marshal(started)
		if err != nil {
			return zero, err
		}
		_, err = tx.Exec(ctx, `INSERT INTO operations(key, method, path, request_hash, resource_type, resource_id, response_status, response)
			VALUES ($1, 'POST', $2, $3, 'mission', $4, $5, $6)`, key, explorationPath, fingerprint, m.ID, http.StatusAccepted, response)
		return started, err
	})
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "55P03" {
		return zero, conflict("提交等待超时，请使用原提交键核对；不要换键重建")
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return zero, fmt.Errorf("%w：提交未能及时确认，请使用原提交键核对", domain.ErrUnavailable)
	}
	return result, err
}

func (s *Store) GetOperation(ctx context.Context, key string) (domain.OperationReceipt, error) {
	var receipt domain.OperationReceipt
	if !domain.ValidOperationKey(key) {
		return receipt, invalid("提交键格式无效")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	err := s.pool.QueryRow(ctx, `SELECT key, method, path, resource_type, resource_id, response_status, committed_at
		FROM operations WHERE key = $1`, key).Scan(&receipt.Key, &receipt.Method, &receipt.Path, &receipt.ResourceType, &receipt.ResourceID, &receipt.ResponseStatus, &receipt.CommittedAt)
	return receipt, databaseError(err)
}
