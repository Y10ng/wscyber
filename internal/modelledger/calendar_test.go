package modelledger

import (
	"net/url"
	"testing"
	"time"
	"wscyber/internal/domain"
)

func TestCalendarMidnightGapAndSkippedDate(t *testing.T) {
	for _, c := range []struct {
		zone, from, to string
		hours          int
		hour           int
	}{{"America/Sao_Paulo", "2018-11-04", "2018-11-05", 23, 1}, {"Pacific/Apia", "2011-12-29", "2011-12-31", 24, 0}, {"Pacific/Apia", "2011-12-30", "2011-12-31", 0, 0}} {
		f, e := ParseFilter(url.Values{"from": {c.from}, "to": {c.to}, "time_zone": {c.zone}}, "", true, time.Now())
		if e != nil || f.To.Sub(f.From) != time.Duration(c.hours)*time.Hour || f.From.Hour() != c.hour || f.Range.From != c.from || f.Range.To != c.to {
			t.Fatal(c, f, e)
		}
	}
}
func TestRedactionDoesNotCreateAnotherSecret(t *testing.T) {
	// 具体验证在ledger_test中的同包helper；短字符串删除后不可重新拼成另一秘密。
	raw := []byte(`{"value":"aXb"}`)
	got, changed := redact(raw, domain.ModelSecrets{APIKey: "ab", Headers: map[string]string{"X-Custom": "X"}})
	if !changed || len(got) != 0 {
		t.Fatal(string(got), changed)
	}
}
