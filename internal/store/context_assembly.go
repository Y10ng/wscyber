package store

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"wscyber/internal/domain"
)

var _ domain.ResearchContextRepository = (*Store)(nil)

func researchScope(ctx context.Context, tx pgx.Tx, id string) (domain.Snapshot, error) {
	var s domain.Snapshot
	var size int
	if err := tx.QueryRow(ctx, `SELECT octet_length(t.title)+octet_length(t.question)+octet_length(t.rationale)+octet_length(t.next_step)+octet_length(m.title)+octet_length(m.origin)+octet_length(m.goal)+octet_length(m.allowed_origins::text) FROM threads t JOIN missions m ON m.id=t.mission_id WHERE t.id=$1`, id).Scan(&size); err != nil {
		return s, err
	}
	if size > domain.MaxContextItemBytes {
		return s, domain.ErrUnavailable
	}
	var err error
	if s.Thread, err = getThread(ctx, tx, id); err != nil {
		return s, err
	}
	s.Mission, err = getMission(ctx, tx, s.Thread.MissionID)
	if err == nil {
		s.Verification, err = verificationForThread(ctx, tx, id)
	}
	return s, err
}
func (s *Store) ResearchScope(ctx context.Context, id string) (domain.Snapshot, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return transaction(ctx, s, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) (domain.Snapshot, error) { return researchScope(ctx, tx, id) })
}

type historyCandidate struct {
	domain.ContextSource
	value any
}
type historySpec struct {
	kind, table, clock, bytes, state, pinned string
	limit                                    int
}

var historySpecs = []historySpec{
	{"observation", "observations", "created_at", "octet_length(kind)+octet_length(content)", "CASE WHEN kind IN ('counterevidence','contradiction','refutation','action_result','model_observation') THEN kind ELSE 'other' END", "kind IN ('counterevidence','contradiction','refutation')", 32},
	{"action", "actions", "created_at", "octet_length(input::text)+COALESCE(octet_length(result::text),0)+octet_length(error)+octet_length(decision_reason)+octet_length(resolution_note)", "status", "status IN ('pending','approved','running','outcome_unknown')", 8},
	{"attempt", "attempts", "started_at", "octet_length(summary)+octet_length(error)", "status", "", 8},
}

func candidates(ctx context.Context, tx pgx.Tx, thread string, spec historySpec) ([]historyCandidate, domain.ContextWindow, error) {
	query := `SELECT id,` + spec.clock + `,` + spec.bytes + `,` + spec.state + ` FROM ` + spec.table + ` WHERE thread_id=$1`
	scan := func(row scanner) (historyCandidate, error) {
		v := historyCandidate{ContextSource: domain.ContextSource{Kind: spec.kind}}
		err := row.Scan(&v.ID, &v.At, &v.OriginalBytes, &v.State)
		return v, err
	}
	recent, err := collect(ctx, tx, query+` ORDER BY `+spec.clock+` DESC,id DESC LIMIT $2`, scan, thread, spec.limit+1)
	w := domain.ContextWindow{Kind: spec.kind, Limit: spec.limit}
	if err != nil {
		return nil, w, err
	}
	if len(recent) > spec.limit {
		w.OlderExists = true
		recent = recent[:spec.limit]
	}
	if len(recent) > 0 {
		last := recent[len(recent)-1]
		w.CutoffAt = &last.At
		w.CutoffID = last.ID
		recent[0].Required = true
	}
	seen := map[string]int{}
	for i, v := range recent {
		seen[v.ID] = i
	}
	if spec.pinned != "" {
		pinned, e := collect(ctx, tx, query+` AND (`+spec.pinned+`) ORDER BY `+spec.clock+` DESC,id DESC LIMIT 17`, scan, thread)
		if e != nil {
			return nil, w, e
		}
		if len(pinned) > 16 {
			return nil, w, domain.ErrUnavailable
		}
		for _, v := range pinned {
			if i, ok := seen[v.ID]; ok {
				recent[i].Required = true
			} else {
				v.Required = true
				seen[v.ID] = len(recent)
				recent = append(recent, v)
			}
		}
	}
	return recent, w, nil
}
func loadHistory(ctx context.Context, tx pgx.Tx, c historyCandidate) (any, error) {
	switch c.Kind {
	case "observation":
		return scanObservation(tx.QueryRow(ctx, `SELECT `+observationColumns+` FROM observations WHERE id=$1`, c.ID))
	case "action":
		return scanAction(tx.QueryRow(ctx, `SELECT `+actionColumns+` FROM actions WHERE id=$1`, c.ID))
	case "attempt":
		return scanAttempt(tx.QueryRow(ctx, `SELECT `+attemptColumns+` FROM attempts WHERE id=$1`, c.ID))
	case "finding":
		return scanFindingRevision(tx.QueryRow(ctx, `SELECT `+findingRevisionColumns+findingRevisionFrom+`WHERE r.id=$1`, c.ID))
	}
	return nil, domain.ErrInvalid
}
func addAssemblySource(a *domain.ContextAssembly, kind, id, state, hash string, at time.Time, value any) {
	raw, _ := json.Marshal(value)
	a.Sources = append(a.Sources, domain.ContextSource{Kind: kind, ID: id, State: state, At: at, OriginalBytes: len(raw), IncludedBytes: len(raw), Included: true, Required: true, Reason: "selected", ContentSHA256: hash})
}
func (s *Store) AssembleResearchSnapshot(ctx context.Context, mission, thread, attempt string) (domain.Snapshot, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return transaction(ctx, s, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) (domain.Snapshot, error) {
		out, err := researchScope(ctx, tx, thread)
		if err != nil {
			return out, err
		}
		if out.Mission.ID != mission {
			return out, domain.ErrNotFound
		}
		var running bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM attempts WHERE id=$1 AND thread_id=$2 AND status='running')`, attempt, thread).Scan(&running); err != nil {
			return out, err
		}
		if !running {
			return out, domain.ErrConflict
		}
		a := &domain.ContextAssembly{ContextSelection: domain.ContextSelection{Policy: domain.FindingsContextPolicy, Windows: []domain.ContextWindow{}, Sources: []domain.ContextSource{}}, MissionID: mission, ThreadID: thread, AttemptID: attempt, AssembledAt: time.Now().UTC(), MaxInputBytes: 256 << 10}
		fixedBytes := 0
		if out.Verification != nil {
			a.Policy = domain.VerificationContextPolicy
			raw, err := json.Marshal(out.Verification)
			if err != nil || len(raw) > domain.MaxContextItemBytes {
				return out, domain.ErrUnavailable
			}
			fixedBytes = len(raw)
			addAssemblySource(a, "verification_source", out.Verification.Source.ID, "candidate", out.Verification.Source.ContentSHA256, out.Verification.Source.CreatedAt, out.Verification)
		}
		addAssemblySource(a, "mission", mission, "", "", out.Mission.CreatedAt, out.Mission)
		addAssemblySource(a, "thread", thread, out.Thread.Status, "", out.Thread.UpdatedAt, out.Thread)
		all := []historyCandidate{}
		for _, spec := range historySpecs {
			rows, w, e := candidates(ctx, tx, thread, spec)
			if e != nil {
				return out, e
			}
			all = append(all, rows...)
			a.Windows = append(a.Windows, w)
		}
		findings, findingWindow, err := findingCandidates(ctx, tx, thread)
		if err != nil {
			return out, err
		}
		all = append(all, findings...)
		a.Windows = append(a.Windows, findingWindow)
		found := false
		for i := range all {
			if all[i].Kind == "attempt" && all[i].ID == attempt {
				all[i].Required = true
				found = true
			}
		}
		if !found {
			c := historyCandidate{ContextSource: domain.ContextSource{Kind: "attempt", ID: attempt, Required: true}}
			if err = tx.QueryRow(ctx, `SELECT started_at,octet_length(summary)+octet_length(error),status FROM attempts WHERE id=$1 AND thread_id=$2`, attempt, thread).Scan(&c.At, &c.OriginalBytes, &c.State); err != nil {
				return out, err
			}
			all = append(all, c)
		}
		// 必需项先占预算；其余严格从新到旧整条选择，无语义摘要和条内截断。
		sort.Slice(all, func(i, j int) bool {
			if all[i].Required != all[j].Required {
				return all[i].Required
			}
			if !all[i].At.Equal(all[j].At) {
				return all[i].At.After(all[j].At)
			}
			return all[i].ID > all[j].ID
		})
		used := fixedBytes
		for i := range all {
			c := &all[i]
			if c.OriginalBytes > domain.MaxContextItemBytes {
				if c.Required {
					return out, domain.ErrUnavailable
				}
				c.Reason = "item_limit"
				continue
			}
			value, e := loadHistory(ctx, tx, *c)
			if e != nil {
				return out, e
			}
			raw, e := json.Marshal(value)
			if e != nil {
				return out, e
			}
			if len(raw)+used > domain.MaxContextHistoryBytes {
				if c.Required {
					return out, domain.ErrUnavailable
				}
				c.Reason = "history_budget"
				continue
			}
			c.Included = true
			c.IncludedBytes = len(raw)
			c.Reason = "selected"
			c.value = value
			if c.Kind == "action" || c.Kind == "observation" {
				c.ContentSHA256 = sourceHash(value)
			}
			used += len(raw)
		}
		sort.Slice(all, func(i, j int) bool {
			if !all[i].At.Equal(all[j].At) {
				return all[i].At.Before(all[j].At)
			}
			return all[i].ID < all[j].ID
		})
		out.Observations = []domain.Observation{}
		out.Actions = []domain.Action{}
		out.Attempts = []domain.Attempt{}
		for _, c := range all {
			a.Sources = append(a.Sources, c.ContextSource)
			if !c.Included {
				continue
			}
			switch v := c.value.(type) {
			case domain.Observation:
				out.Observations = append(out.Observations, v)
			case domain.Action:
				out.Actions = append(out.Actions, v)
			case domain.Attempt:
				out.Attempts = append(out.Attempts, v)
			case domain.FindingRevision:
				out.Findings = append(out.Findings, v)
			}
		}
		cfg, err := readResearchContext(ctx, tx, mission)
		if err != nil {
			return out, err
		}
		if len(cfg.Materials) > 0 || cfg.SafetyPromptEnabled || cfg.ResearchBrief != nil {
			out.ResearchContext = &cfg
		}
		for _, m := range cfg.Materials {
			addAssemblySource(a, "material", m.ID, "", m.ContentSHA256, a.AssembledAt, m)
		}
		if cfg.ResearchBrief != nil {
			var hash string
			if err = tx.QueryRow(ctx, `SELECT content_sha256 FROM mission_research_briefs WHERE mission_id=$1`, mission).Scan(&hash); err != nil {
				return out, err
			}
			addAssemblySource(a, "research_brief", mission, "shared", hash, a.AssembledAt, cfg.ResearchBrief)
		}
		if cfg.SafetyPromptEnabled {
			addAssemblySource(a, "safety_prompt", mission, "enabled", "", a.AssembledAt, cfg.SafetyPrompt)
		}
		active, err := readActiveSkills(ctx, tx, mission, thread)
		if err != nil {
			return out, err
		}
		skill := domain.SkillContext{Active: active}
		last, err := scanSkillActivity(tx.QueryRow(ctx, `SELECT `+skillActivityColumns+` FROM skill_activities WHERE mission_id=$1 AND thread_id=$2 ORDER BY skill_activities.seq DESC LIMIT 1`, mission, thread))
		if err != nil && !errors.Is(err, domain.ErrNotFound) {
			return out, err
		}
		if err == nil {
			skill.LastActivity = &last
			addAssemblySource(a, "skill_activity", last.AttemptID, last.Result.Status, "", last.CreatedAt, last)
		}
		if len(active) > 0 || skill.LastActivity != nil {
			out.SkillContext = &skill
		}
		for _, l := range active {
			addAssemblySource(a, "skill", l.LoadID, "active", l.ContentSHA256, l.CreatedAt, l)
		}
		out.Supplements, err = supplementContext(ctx, tx, mission, thread)
		if err != nil {
			return out, err
		}
		for _, v := range out.Supplements {
			addAssemblySource(a, "supplement", v.ID, "user_statement", v.ContentSHA256, v.CreatedAt, v)
		}
		out.ContextSelection = &a.ContextSelection
		out.Assembly = a
		return out, nil
	})
}
func (s *Store) GetModelCallContext(ctx context.Context, id, mission string) (domain.ModelCallContext, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return transaction(ctx, s, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) (domain.ModelCallContext, error) {
		out := domain.ModelCallContext{CallID: id}
		var raw []byte
		err := tx.QueryRow(ctx, `SELECT COALESCE(c.mission_id,''),COALESCE(c.thread_id,''),a.assembly FROM model_calls c LEFT JOIN model_call_contexts a ON a.call_id=c.id WHERE c.id=$1 AND ($2='' OR c.mission_id=$2)`, id, mission).Scan(&out.MissionID, &out.ThreadID, &raw)
		if err != nil {
			return out, err
		}
		if len(raw) == 0 {
			return out, nil
		}
		if len(raw) > domain.MaxContextAssemblyBytes {
			return out, domain.ErrUnavailable
		}
		var a domain.ContextAssembly
		if err = json.Unmarshal(raw, &a); err != nil {
			return out, err
		}
		if err = domain.ValidateContextAssembly(&a); err != nil || a.MissionID != out.MissionID || a.ThreadID != out.ThreadID {
			return out, domain.ErrUnavailable
		}
		out.Recorded = true
		out.Assembly = &a
		return out, nil
	})
}
