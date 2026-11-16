package store

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"wscyber/internal/domain"
)

type callCursor struct {
	Filter  string    `json:"filter"`
	Upper   time.Time `json:"upper"`
	Started time.Time `json:"started"`
	ID      string    `json:"id"`
}

func filterHash(f domain.ModelCallFilter) string {
	f.Cursor = ""
	b, _ := json.Marshal(f)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func (s *Store) encodeCallCursor(c callCursor) string {
	b, _ := json.Marshal(c)
	mac := hmac.New(sha256.New, s.cursorKey[:])
	mac.Write(b)
	return base64.RawURLEncoding.EncodeToString(b) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
func (s *Store) decodeCallCursor(raw string, f domain.ModelCallFilter) (callCursor, error) {
	var c callCursor
	parts := strings.Split(raw, ".")
	if len(parts) != 2 {
		return c, domain.ErrInvalid
	}
	b, e := base64.RawURLEncoding.Strict().DecodeString(parts[0])
	if e != nil {
		return c, domain.ErrInvalid
	}
	sig, e := base64.RawURLEncoding.Strict().DecodeString(parts[1])
	if e != nil {
		return c, domain.ErrInvalid
	}
	mac := hmac.New(sha256.New, s.cursorKey[:])
	mac.Write(b)
	if !hmac.Equal(sig, mac.Sum(nil)) {
		return c, domain.ErrInvalid
	}
	if json.Unmarshal(b, &c) != nil || c.Filter != filterHash(f) || c.ID == "" || c.Upper.IsZero() || c.Started.IsZero() || c.Started.After(c.Upper) {
		return c, domain.ErrInvalid
	}
	return c, nil
}
func ledgerFilter(f domain.ModelCallFilter, dates bool) (string, []any) {
	args := []any{}
	where := []string{"true"}
	add := func(column string, v any) {
		args = append(args, v)
		where = append(where, fmt.Sprintf("%s=$%d", column, len(args)))
	}
	if f.MissionID != "" {
		add("mission_id", f.MissionID)
	}
	if f.ModelConfigID != "" {
		add("model_config_id", f.ModelConfigID)
	}
	if f.Purpose != "" {
		add("purpose", f.Purpose)
	}
	if f.Outcome != "" {
		add("outcome", f.Outcome)
	}
	if dates {
		args = append(args, f.From, f.To)
		where = append(where, fmt.Sprintf("started_at >= $%d AND started_at < $%d", len(args)-1, len(args)))
	}
	return strings.Join(where, " AND "), args
}
func checkLedgerMission(ctx context.Context, q querier, mission string) error {
	if mission == "" {
		return nil
	}
	var found bool
	if err := q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM missions WHERE id=$1)`, mission).Scan(&found); err != nil {
		return err
	}
	if !found {
		return domain.ErrNotFound
	}
	return nil
}
func ledgerCoverage(ctx context.Context, q querier) (time.Time, error) {
	var t time.Time
	err := q.QueryRow(ctx, `SELECT coverage_started_at FROM model_ledger_settings WHERE singleton`).Scan(&t)
	return t, err
}
func (s *Store) ListModelCalls(ctx context.Context, f domain.ModelCallFilter) (domain.ModelCallsPage, error) {
	var zero domain.ModelCallsPage
	if f.Limit < 1 || f.Limit > 100 || f.From.After(f.To) {
		return zero, domain.ErrInvalid
	}
	cursor := callCursor{Filter: filterHash(f), Upper: time.Now().UTC()}
	if f.Cursor != "" {
		var err error
		cursor, err = s.decodeCallCursor(f.Cursor, f)
		if err != nil {
			return zero, err
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return transaction(ctx, s, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) (domain.ModelCallsPage, error) {
		out := domain.ModelCallsPage{Range: f.Range, Items: []domain.ModelCall{}}
		if err := checkLedgerMission(ctx, tx, f.MissionID); err != nil {
			return zero, err
		}
		var err error
		out.CoverageStartedAt, err = ledgerCoverage(ctx, tx)
		if err != nil {
			return zero, err
		}
		where, args := ledgerFilter(f, true)
		args = append(args, cursor.Upper)
		where += fmt.Sprintf(" AND started_at <= $%d", len(args))
		if f.Cursor != "" {
			args = append(args, cursor.Started, cursor.ID)
			where += fmt.Sprintf(" AND (started_at,id) < ($%d,$%d)", len(args)-1, len(args))
		}
		args = append(args, f.Limit+1)
		items, err := collect(ctx, tx, `SELECT `+modelCallColumns+` FROM model_calls WHERE `+where+fmt.Sprintf(` ORDER BY started_at DESC,id DESC LIMIT $%d`, len(args)), scanModelCall, args...)
		if err != nil {
			return zero, err
		}
		if len(items) > f.Limit {
			items = items[:f.Limit]
			last := items[len(items)-1]
			cursor.Started = last.StartedAt
			cursor.ID = last.ID
			next := s.encodeCallCursor(cursor)
			out.NextCursor = &next
		}
		out.Items = items
		return out, nil
	})
}

func emptyUsageSummary() domain.ModelUsageSummary {
	return domain.ModelUsageSummary{Calls: "0", RunningCalls: "0", InterruptedCalls: "0", InputTokens: "0", OutputTokens: "0", TotalTokens: "0", MissingUsageCalls: "0", InvalidUsageCalls: "0", UnpricedCalls: "0", EstimatedCosts: []domain.EstimatedCostTotal{}}
}

// PG numeric聚合，不把调用逐条载入内存；最多返回一个summary及93个日期。
const usageSums = `count(*)::text,
 count(*) FILTER(WHERE state='running')::text,
 count(*) FILTER(WHERE state='interrupted')::text,
 COALESCE(sum((usage->>'input_tokens')::numeric) FILTER(WHERE usage->>'status'<>'invalid'),0)::text,
 COALESCE(sum((usage->>'output_tokens')::numeric) FILTER(WHERE usage->>'status'<>'invalid'),0)::text,
 COALESCE(sum((usage->>'total_tokens')::numeric) FILTER(WHERE usage->>'status'<>'invalid'),0)::text,
 count(*) FILTER(WHERE usage->>'status'<>'reported')::text,
 count(*) FILTER(WHERE usage->>'status'='invalid')::text,
 count(*) FILTER(WHERE cost->>'status'<>'estimated')::text`

func costSums() string {
	out := ""
	for _, c := range []string{"USD", "CNY", "EUR"} {
		out += `,COALESCE(sum((cost->>'amount')::numeric) FILTER(WHERE cost->>'status'='estimated' AND cost->>'currency'='` + c + `'),0)::text,count(*) FILTER(WHERE cost->>'status'='estimated' AND cost->>'currency'='` + c + `')::text`
	}
	return out
}
func scanUsage(row scanner, date *string) (domain.ModelUsageSummary, error) {
	u := emptyUsageSummary()
	dest := []any{}
	if date != nil {
		dest = append(dest, date)
	}
	dest = append(dest, &u.Calls, &u.RunningCalls, &u.InterruptedCalls, &u.InputTokens, &u.OutputTokens, &u.TotalTokens, &u.MissingUsageCalls, &u.InvalidUsageCalls, &u.UnpricedCalls)
	var amounts, counts [3]string
	for i := 0; i < 3; i++ {
		dest = append(dest, &amounts[i], &counts[i])
	}
	if err := row.Scan(dest...); err != nil {
		return u, err
	}
	for i, c := range []string{"USD", "CNY", "EUR"} {
		if counts[i] != "0" {
			a := amounts[i]
			if strings.Contains(a, ".") {
				a = strings.TrimRight(strings.TrimRight(a, "0"), ".")
			}
			u.EstimatedCosts = append(u.EstimatedCosts, domain.EstimatedCostTotal{Currency: c, Amount: a, PricedCalls: counts[i]})
		}
	}
	return u, nil
}
func (s *Store) ModelUsage(ctx context.Context, f domain.ModelCallFilter) (domain.ModelUsageReport, error) {
	var zero domain.ModelUsageReport
	first, a := time.Parse("2006-01-02", f.Range.From)
	last, b := time.Parse("2006-01-02", f.Range.To)
	if a != nil || b != nil || !first.Before(last) || last.After(first.AddDate(0, 0, 93)) || f.From.After(f.To) {
		return zero, domain.ErrInvalid
	}
	if _, err := time.LoadLocation(f.Range.TimeZone); err != nil {
		return zero, domain.ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return transaction(ctx, s, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) (domain.ModelUsageReport, error) {
		out := domain.ModelUsageReport{Range: f.Range, Daily: []domain.ModelUsageDay{}}
		if f.MissionID != "" {
			out.SummaryScope = "range"
			if f.AllTime {
				out.SummaryScope = "all_time"
			}
		}
		if err := checkLedgerMission(ctx, tx, f.MissionID); err != nil {
			return zero, err
		}
		var err error
		out.CoverageStartedAt, err = ledgerCoverage(ctx, tx)
		if err != nil {
			return zero, err
		}
		where, args := ledgerFilter(f, !f.AllTime)
		out.Summary, err = scanUsage(tx.QueryRow(ctx, `SELECT `+usageSums+costSums()+` FROM model_calls WHERE `+where, args...), nil)
		if err != nil {
			return zero, err
		}
		where, args = ledgerFilter(f, true)
		args = append(args, f.Range.TimeZone)
		rows, err := tx.Query(ctx, fmt.Sprintf(`SELECT to_char(started_at AT TIME ZONE $%d,'YYYY-MM-DD') AS day,`, len(args))+usageSums+costSums()+` FROM model_calls WHERE `+where+` GROUP BY day ORDER BY day`, args...)
		if err != nil {
			return zero, err
		}
		days := map[string]domain.ModelUsageSummary{}
		for rows.Next() {
			var day string
			u, e := scanUsage(rows, &day)
			if e != nil {
				rows.Close()
				return zero, e
			}
			days[day] = u
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return zero, err
		}
		for d := first; d.Before(last); d = d.AddDate(0, 0, 1) {
			day := d.Format("2006-01-02")
			u, ok := days[day]
			if !ok {
				u = emptyUsageSummary()
			}
			out.Daily = append(out.Daily, domain.ModelUsageDay{Date: day, ModelUsageSummary: u})
		}
		return out, nil
	})
}
