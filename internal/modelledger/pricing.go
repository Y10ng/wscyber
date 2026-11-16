// Package modelledger 保存实际调用边界，不发送重试、不推断缺失用量。
package modelledger

import (
	"encoding/json"
	"fmt"
	"math/big"
	"regexp"
	"strconv"
	"strings"

	"wscyber/internal/domain"
	"wscyber/internal/modelconfig"
)

var decimalPattern = regexp.MustCompile(`^(0|[1-9][0-9]{0,6})(\.[0-9]{1,6})?$`)

func priceUnits(s string) (*big.Int, bool) {
	if !decimalPattern.MatchString(s) {
		return nil, false
	}
	parts := strings.Split(s, ".")
	fraction := ""
	if len(parts) == 2 {
		fraction = parts[1]
	}
	n, ok := new(big.Int).SetString(parts[0]+fraction+strings.Repeat("0", 6-len(fraction)), 10)
	return n, ok && n.Cmp(big.NewInt(1_000_000_000_000)) <= 0
}
func ValidatePrice(in domain.ModelPriceWrite) error {
	bad := func() error {
		return fmt.Errorf("%w：价格身份、币种或十进制单价无效", domain.ErrInvalid)
	}
	if in.ExpectedRevision < 0 || in.ExpectedRevision >= 1<<53-1 {
		return bad()
	}
	if in.Source != "managed" && in.Source != "environment" {
		return bad()
	}
	if (in.Source == "managed" && !modelconfig.ValidID(in.ModelConfigID)) || (in.Source == "environment" && in.ModelConfigID != "") {
		return bad()
	}
	if in.Protocol != "openai_chat" && in.Protocol != "anthropic_messages" {
		return bad()
	}
	if len(in.Model) == 0 || len(in.Model) > 200 || strings.TrimSpace(in.Model) != in.Model {
		return bad()
	}
	if in.Currency != "USD" && in.Currency != "CNY" && in.Currency != "EUR" {
		return bad()
	}
	for _, p := range []*string{&in.InputPerMillion, &in.OutputPerMillion, in.CacheReadPerMillion, in.CacheWrite5mPerMillion, in.CacheWrite1hPerMillion} {
		if p != nil {
			if _, ok := priceUnits(*p); !ok {
				return bad()
			}
		}
	}
	return nil
}

// Estimate 使用百万分之一单价乘tokens，金额固定到10^-12后去掉多余零，无浮点数。
func Estimate(protocol string, u domain.ModelUsage, extra bool, base domain.ModelCost) domain.ModelCost {
	base.Amount = nil
	if u.Status == "missing" || u.Status == "invalid" || u.InputTokens == nil || u.OutputTokens == nil {
		base.Status = "usage_missing"
		return base
	}
	if extra {
		base.Status = "unsupported"
		return base
	}
	if base.PriceSnapshot == nil {
		base.Status = "unpriced"
		return base
	}
	unsupported := func() domain.ModelCost { base.Status = "unsupported"; return base }
	count := func(s *string) (int64, bool) {
		if s == nil {
			return 0, false
		}
		n, e := strconv.ParseInt(*s, 10, 64)
		return n, e == nil && n >= 0 && n <= 1_000_000_000_000
	}
	input, ok := count(u.InputTokens)
	if !ok {
		return unsupported()
	}
	output, ok := count(u.OutputTokens)
	if !ok {
		return unsupported()
	}
	read, ok := count(u.CacheReadInputTokens)
	if !ok {
		return unsupported()
	}
	var five, hour int64
	if protocol == "anthropic_messages" {
		creation, ok := count(u.CacheCreationInputTokens)
		if !ok {
			return unsupported()
		}
		if creation > 0 {
			var a, b bool
			five, a = count(u.CacheCreation5mInputTokens)
			hour, b = count(u.CacheCreation1hInputTokens)
			if !a || !b || five+hour != creation {
				return unsupported()
			}
		}
	} else if protocol != "openai_chat" {
		return unsupported()
	}
	normal := input - read - five - hour
	if normal < 0 {
		return unsupported()
	}
	terms := base.PriceSnapshot
	amount := new(big.Int)
	for _, b := range []struct {
		n int64
		p *string
	}{{normal, &terms.InputPerMillion}, {output, &terms.OutputPerMillion}, {read, terms.CacheReadPerMillion}, {five, terms.CacheWrite5mPerMillion}, {hour, terms.CacheWrite1hPerMillion}} {
		if b.n == 0 {
			continue
		}
		if b.p == nil {
			return unsupported()
		}
		p, ok := priceUnits(*b.p)
		if !ok {
			return unsupported()
		}
		amount.Add(amount, new(big.Int).Mul(p, big.NewInt(b.n)))
	}
	s := formatUnits(amount, 12)
	base.Amount = &s
	base.Status = "estimated"
	return base
}
func formatUnits(n *big.Int, scale int) string {
	s := n.String()
	if len(s) <= scale {
		s = strings.Repeat("0", scale+1-len(s)) + s
	}
	s = s[:len(s)-scale] + "." + s[len(s)-scale:]
	return strings.TrimRight(strings.TrimRight(s, "0"), ".")
}

// DecodePrice 拒绝重复、大小写别名、未知、缺失与不适用null。
func DecodePrice(raw []byte) (domain.ModelPriceWrite, error) {
	var out domain.ModelPriceWrite
	keys := map[string]bool{"expected_revision": false, "source": false, "model_config_id": false, "protocol": false, "model": false, "enabled": false, "currency": false, "input_per_million": false, "output_per_million": false, "cache_read_per_million": true, "cache_write_5m_per_million": true, "cache_write_1h_per_million": true}
	if err := StrictObject(raw, keys); err != nil {
		return out, err
	}
	if json.Unmarshal(raw, &out) != nil {
		return out, domain.ErrInvalid
	}
	return out, ValidatePrice(out)
}
