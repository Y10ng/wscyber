package modelledger

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"time"
	_ "time/tzdata"

	"wscyber/internal/domain"
	"wscyber/internal/modelconfig"
)

// StrictObject 的所有键必需，bool值只决定是否接受显式null。
func StrictObject(raw []byte, fields map[string]bool) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	t, e := d.Token()
	if e != nil || t != json.Delim('{') {
		return domain.ErrInvalid
	}
	seen := map[string]bool{}
	for d.More() {
		t, e = d.Token()
		if e != nil {
			return domain.ErrInvalid
		}
		k, ok := t.(string)
		nullable, known := fields[k]
		if !ok || !known || seen[k] {
			return domain.ErrInvalid
		}
		seen[k] = true
		var v json.RawMessage
		if d.Decode(&v) != nil || (!nullable && bytes.Equal(v, []byte("null"))) {
			return domain.ErrInvalid
		}
	}
	if t, e = d.Token(); e != nil || t != json.Delim('}') {
		return domain.ErrInvalid
	}
	if _, e = d.Token(); e != io.EOF || len(seen) != len(fields) {
		return domain.ErrInvalid
	}
	return nil
}

func ParseFilter(query url.Values, mission string, summary bool, now time.Time) (domain.ModelCallFilter, error) {
	var f domain.ModelCallFilter
	invalid := func() (domain.ModelCallFilter, error) {
		return f, fmt.Errorf("%w：筛选、日期、时区或分页参数无效", domain.ErrInvalid)
	}
	allowed := map[string]bool{"mission_id": true, "model_config_id": true, "purpose": true, "outcome": true, "from": true, "to": true, "time_zone": true}
	if !summary {
		allowed["cursor"] = true
		allowed["limit"] = true
	}
	for k, v := range query {
		if !allowed[k] || len(v) != 1 || v[0] == "" {
			return invalid()
		}
	}
	f.MissionID = query.Get("mission_id")
	if mission != "" {
		if f.MissionID != "" && f.MissionID != mission {
			return invalid()
		}
		f.MissionID = mission
	}
	if len(f.MissionID) > 128 {
		return invalid()
	}
	f.ModelConfigID = query.Get("model_config_id")
	if f.ModelConfigID != "" && !modelconfig.ValidID(f.ModelConfigID) {
		return invalid()
	}
	f.Purpose = query.Get("purpose")
	if f.Purpose != "" && f.Purpose != "research" && f.Purpose != "verification" && f.Purpose != "connectivity_test" {
		return invalid()
	}
	f.Outcome = query.Get("outcome")
	switch f.Outcome {
	case "", "pending", "not_sent", "transport_unknown", "http_error", "invalid_response", "valid_response", "interrupted":
	default:
		return invalid()
	}
	zone := query.Get("time_zone")
	if zone == "" {
		zone = "UTC"
	}
	if zone == "Local" {
		return invalid()
	}
	loc, err := time.LoadLocation(zone)
	if err != nil {
		return invalid()
	}
	local := now.In(loc)
	// 日历运算用UTC承载年月日，不在有午夜跳变的时区上循环AddDate。
	// 否则如Pacific/Apia跳过整日，会把下一日归回同一日并使填空日循环不前进。
	today := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.UTC)
	from, to := query.Get("from"), query.Get("to")
	explicit := from != "" || to != ""
	first, last := today.AddDate(0, 0, -6), today.AddDate(0, 0, 1)
	if explicit {
		if from != "" {
			first, err = time.Parse("2006-01-02", from)
			if err != nil {
				return invalid()
			}
		}
		if to != "" {
			last, err = time.Parse("2006-01-02", to)
			if err != nil {
				return invalid()
			}
		}
		if from == "" {
			first = last.AddDate(0, 0, -7)
		}
		if to == "" {
			last = first.AddDate(0, 0, 7)
		}
	}
	if !first.Before(last) || last.After(first.AddDate(0, 0, 93)) {
		return invalid()
	}
	f.From, f.To = localDayBoundary(first, loc), localDayBoundary(last, loc)
	f.Range = domain.ModelCallRange{From: first.Format("2006-01-02"), To: last.Format("2006-01-02"), TimeZone: zone}
	f.AllTime = summary && mission != "" && !explicit
	f.Limit = 25
	f.Cursor = query.Get("cursor")
	if len(f.Cursor) > 4096 {
		return invalid()
	}
	if l := query.Get("limit"); l != "" {
		f.Limit, err = strconv.Atoi(l)
		if err != nil || f.Limit < 1 || f.Limit > 100 {
			return invalid()
		}
	}
	return f, nil
}

// 找到本地日期开始的第一个真实时刻：午夜缺失取01:00，整日缺失取次日边界。
func localDayBoundary(day time.Time, loc *time.Location) time.Time {
	target := day.Format("2006-01-02")
	lo, hi := day.Add(-36*time.Hour).Unix(), day.Add(36*time.Hour).Unix()
	for lo < hi {
		mid := lo + (hi-lo)/2
		if time.Unix(mid, 0).In(loc).Format("2006-01-02") < target {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	return time.Unix(lo, 0).In(loc)
}
