package modelledger

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	"wscyber/internal/domain"
	"wscyber/internal/modelconfig"
	"wscyber/internal/modelprovider"
)

type Service struct {
	repo    domain.ModelCallRepository
	vault   *modelconfig.Vault
	models  *modelconfig.Manager
	stopped atomic.Bool
	once    sync.Once
	failed  chan struct{}
	// 同一进程的发送准入和持久失败串行化；不锁住整个网络请求。
	gate sync.Mutex
}

func New(repo domain.ModelCallRepository, vault *modelconfig.Vault, models *modelconfig.Manager) *Service {
	return &Service{repo: repo, vault: vault, models: models, failed: make(chan struct{})}
}
func (s *Service) Available() bool         { return s != nil && s.repo != nil && s.vault != nil }
func (s *Service) Failed() <-chan struct{} { return s.failed }

// StopDispatch 将研究活动的持久失败接入同一个发送门闩；不重放任何调用。
func (s *Service) StopDispatch() {
	if s == nil {
		return
	}
	s.gate.Lock()
	defer s.gate.Unlock()
	_ = s.fail()
}
func (s *Service) fail() error {
	s.stopped.Store(true)
	s.once.Do(func() { close(s.failed) })
	return fmt.Errorf("%w：模型调用账本持久化未确认，已停止后续模型派发；禁止自动重发", domain.ErrUnavailable)
}

type Identity struct {
	Source, ConfigID, ConfigName           string
	ConfigRevision                         int64
	MissionID, ThreadID, AttemptID, TestID string
	SkillLoadIDs                           []string
	SupplementIDs                          []string
	Assembly                               *domain.ContextAssembly
}

// Complete 先持久化，再进入固定供应商边界；返回前必须持久化完成回执。
// validate 是本地研究Decision校验，只有本次调用成功落盘后才允许调用者采用结果。
func (s *Service) Complete(ctx context.Context, id Identity, doc domain.ModelDocument, in modelprovider.Input, validate func(string) bool) (modelprovider.Result, error) {
	zero := modelprovider.Result{Outcome: modelprovider.NotSent, Usage: modelprovider.MissingUsage()}
	if s == nil {
		r := modelprovider.Complete(ctx, doc, in)
		return r, nil
	}
	if s.stopped.Load() {
		return zero, domain.ErrUnavailable
	}
	if _, err := modelconfig.ValidateForUse(doc); err != nil || len(in.System)+len(in.User) > 256<<10 || strings.TrimSpace(in.User) == "" || !utf8.ValidString(in.System) || !utf8.ValidString(in.User) || in.MaxOutputTokens < 1 || in.MaxOutputTokens > doc.MaxOutputTokens || ctx.Err() != nil {
		return zero, domain.ErrInvalid
	}
	if id.Assembly != nil {
		if err := domain.ValidateContextAssembly(id.Assembly); err != nil || id.Assembly.InputBytes != len(in.System)+len(in.User) || id.Assembly.MissionID != id.MissionID || id.Assembly.ThreadID != id.ThreadID || id.Assembly.AttemptID != id.AttemptID {
			return zero, domain.ErrInvalid
		}
	}
	recording := false
	if id.MissionID != "" {
		v, err := s.repo.GetModelRecording(ctx, id.MissionID)
		if err != nil {
			return zero, s.fail()
		}
		recording = v.RecordModelContent
	}
	if recording && !s.Available() {
		return zero, domain.ErrUnavailable
	}
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return zero, domain.ErrUnavailable
	}
	callID := hex.EncodeToString(b)
	purpose := "research"
	if id.TestID != "" {
		purpose = "connectivity_test"
	} else if id.Assembly != nil && id.Assembly.Policy == domain.VerificationContextPolicy {
		purpose = "verification"
	}
	call := domain.ModelCall{ID: callID, ModelPriceKey: domain.ModelPriceKey{Source: id.Source, ModelConfigID: id.ConfigID, Protocol: doc.Protocol, Model: doc.Model}, ConfigName: id.ConfigName, Purpose: purpose, MissionID: id.MissionID, ThreadID: id.ThreadID, AttemptID: id.AttemptID, TestID: id.TestID, RecordModelContent: recording}
	start := domain.BeginModelCall{Call: call, ConfigRevision: id.ConfigRevision, SkillLoadIDs: id.SkillLoadIDs, SupplementIDs: id.SupplementIDs, Assembly: id.Assembly}
	if recording {
		raw, _ := json.Marshal(struct {
			System string `json:"system"`
			User   string `json:"user"`
		}{in.System, in.User})
		safe, redacted := redact(raw, doc.Secrets)
		defer clear(raw)
		defer clear(safe)
		encrypted, err := s.vault.SealContent(callID, "input", safe)
		if err != nil {
			return zero, domain.ErrUnavailable
		}
		start.Input = &domain.SealedCallContent{Kind: "input", Ciphertext: encrypted, MediaType: "application/json", Complete: true, Redacted: redacted}
	}
	s.gate.Lock()
	if s.stopped.Load() {
		s.gate.Unlock()
		return zero, domain.ErrUnavailable
	}
	began, err := s.repo.BeginModelCall(ctx, start)
	if err != nil {
		err = s.fail()
		s.gate.Unlock()
		return zero, err
	}
	s.gate.Unlock()
	// begin落盘后取消仍要保存not_sent，而非删除这次已经进入调用边界的记录。
	in.AllowSend = func() bool { return !s.stopped.Load() }
	result := modelprovider.Complete(ctx, doc, in)
	defer clear(result.Response)
	// 凭据回显不能静默改字后执行：那会变成模型从未提出的另一动作。
	// 保留usage，但拒绝整次研究Decision；连通测试从不采纳/持久化Text。
	rejectedText := ""
	if (purpose == "research" || purpose == "verification") && result.Text != "" {
		rawText := []byte(result.Text)
		clean, redacted := redact(rawText, doc.Secrets)
		if redacted {
			rejectedText = result.Text
			result.Text = ""
			result.Outcome = modelprovider.InvalidResponse
		}
		clear(clean)
		clear(rawText)
	}
	if result.Outcome == modelprovider.ValidResponse && validate != nil && !validate(result.Text) {
		result.Outcome = modelprovider.InvalidResponse
	}
	finish := domain.FinishModelCall{ID: callID, Outcome: result.Outcome, Attempted: result.Attempted, FinishedAt: time.Now().UTC(), Usage: result.Usage, Cost: Estimate(doc.Protocol, result.Usage, result.ExtraBilling, began.Cost)}
	finish.DurationMS = max(0, finish.FinishedAt.Sub(began.StartedAt).Milliseconds())
	if result.HTTPStatus != 0 {
		finish.HTTPStatus = &result.HTTPStatus
	}
	if recording && result.ResponseReceived {
		// 同时移除被拒绝的完整Text，覆盖JSON信封中再次转义的Decision字符串。
		safe, redacted := redact(result.Response, doc.Secrets, rejectedText)
		defer clear(safe)
		encrypted, e := s.vault.SealContent(callID, "response", safe)
		if e != nil {
			return zero, s.fail()
		}
		finish.Response = &domain.SealedCallContent{Kind: "response", Ciphertext: encrypted, MediaType: result.ResponseMediaType, Complete: result.ResponseComplete, Redacted: redacted}
	}
	clear(result.Response)
	result.Response = nil
	persist, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s.gate.Lock()
	err = s.repo.FinishModelCall(persist, finish)
	if err != nil {
		err = s.fail()
	}
	s.gate.Unlock()
	if err != nil {
		return zero, err
	}
	if s.stopped.Load() {
		return zero, domain.ErrUnavailable // 另一在途回执失败时，本次也不得继续驱动研究。
	}
	return result, nil
}

func (s *Service) Content(ctx context.Context, id, mission string) (domain.ModelCallContent, error) {
	out := domain.ModelCallContent{CallID: id}
	c, err := s.repo.GetModelCall(ctx, id, mission)
	if err != nil {
		return out, err
	}
	out.Recorded = c.RecordModelContent
	if !out.Recorded {
		return out, nil
	}
	if !s.Available() {
		return out, fmt.Errorf("%w：模型原文加密密钥未配置", domain.ErrUnavailable)
	}
	state := "missing"
	if c.State == "running" {
		state = "pending"
	}
	out.Input = &domain.ModelContentPart{State: state, MediaType: "application/json"}
	out.Response = &domain.ModelContentPart{State: state, MediaType: "application/octet-stream"}
	parts, err := s.repo.GetSealedCallContent(ctx, id)
	if err != nil {
		return out, err
	}
	for _, p := range parts {
		plain, e := s.vault.OpenContent(id, p.Kind, p.Ciphertext)
		if e != nil {
			return domain.ModelCallContent{}, e
		}
		b64 := base64.StdEncoding.EncodeToString(plain)
		clear(plain)
		v := &domain.ModelContentPart{State: "available", BodyBase64: &b64, MediaType: p.MediaType, Complete: p.Complete, Redacted: p.Redacted}
		if p.Kind == "input" {
			out.Input = v
		} else {
			out.Response = v
		}
	}
	return out, nil
}
func (s *Service) SavePrice(ctx context.Context, id string, in domain.ModelPriceWrite) (domain.ModelPrice, bool, error) {
	if !modelconfig.ValidID(id) {
		return domain.ModelPrice{}, false, domain.ErrInvalid
	}
	if err := ValidatePrice(in); err != nil {
		return domain.ModelPrice{}, false, err
	}
	if in.Source == "managed" && (s.models == nil || !s.models.Available()) {
		return domain.ModelPrice{}, false, domain.ErrUnavailable
	}
	return s.repo.SaveModelPrice(ctx, id, in, func(r domain.SealedModelConfig) bool { return s.models.MatchesModel(r, in.Protocol, in.Model) })
}

// redact 只处理明确已知连接凭据，不承诺通用研究材料脱敏。无改动时保留原字节。
func redact(raw []byte, secrets domain.ModelSecrets, extraValues ...string) ([]byte, bool) {
	values := append([]string{secrets.APIKey}, extraValues...)
	for _, v := range secrets.Headers {
		values = append(values, v)
	}
	var stringsIn func(any)
	stringsIn = func(v any) {
		switch x := v.(type) {
		case string:
			values = append(values, x)
		case map[string]any:
			for _, v := range x {
				stringsIn(v)
			}
		case []any:
			for _, v := range x {
				stringsIn(v)
			}
		}
	}
	for _, v := range secrets.Parameters {
		var x any
		d := json.NewDecoder(bytes.NewReader(v))
		d.UseNumber()
		if d.Decode(&x) == nil {
			stringsIn(x)
		}
	}
	seen := map[string]bool{}
	for _, secret := range values {
		if secret != "" {
			seen[secret] = true
			// Snapshot/Decision本身是JSON文本时，外层JSON又转义一次；纳入标准转义形式。
			q, _ := json.Marshal(secret)
			seen[string(q[1:len(q)-1])] = true
		}
	}
	if len(seen) == 0 {
		return raw, false
	}
	// 不解码为map：重复键会丢失较早的凭据，损坏/截断JSON也必须脱敏。
	// 保留解码字节到原文的映射，只删除命中片段，不重新编码整个业务原件。
	// 删除而非替换标记，保证短凭据不会使有界响应膨胀，也不会产生新的凭据回显。
	normalized := make([]byte, 0, len(raw))
	starts, ends := make([]uint32, 0, len(raw)), make([]uint32, 0, len(raw))
	for i := 0; i < len(raw); {
		start := i
		decoded, size := decodeJSONEscape(raw[i:])
		if size == 0 {
			decoded, size = raw[i:i+1], 1
		}
		i += size
		normalized = append(normalized, decoded...)
		for range decoded {
			starts = append(starts, uint32(start))
			ends = append(ends, uint32(i))
		}
	}
	defer clear(normalized)
	removed := make([]bool, len(raw))
	changed := false
	for secret := range seen {
		// KMP线性搜索重叠命中；逐次bytes.Index会反复比较重复长凭据。
		prefix := make([]int, len(secret))
		for i, j := 1, 0; i < len(secret); i++ {
			for j > 0 && secret[i] != secret[j] {
				j = prefix[j-1]
			}
			if secret[i] == secret[j] {
				j++
			}
			prefix[i] = j
		}
		for _, encoded := range []bool{false, true} {
			text := raw
			if encoded {
				text = normalized
			}
			matched, covered := 0, 0
			for i, b := range text {
				for matched > 0 && b != secret[matched] {
					matched = prefix[matched-1]
				}
				if b == secret[matched] {
					matched++
				}
				if matched != len(secret) {
					continue
				}
				left, right := i+1-len(secret), i+1
				if encoded {
					left, right = int(starts[left]), int(ends[right-1])
				}
				// 同一搜索的命中区间有序，只标新尾部：O(N)，不是O(N×秘密长度)。
				for at := max(left, covered); at < right; at++ {
					removed[at] = true
				}
				covered = max(covered, right)
				changed = true
				matched = prefix[matched-1]
			}
		}
	}
	if !changed {
		return raw, false
	}
	clean := make([]byte, 0, len(raw))
	for i, b := range raw {
		if !removed[i] {
			clean = append(clean, b)
		}
	}
	// 删除片段可能把两侧拼接成另一个已知秘密；这种极端情形宁可移除原件，不能回显。
	check := make([]byte, 0, len(clean))
	for i := 0; i < len(clean); {
		decoded, size := decodeJSONEscape(clean[i:])
		if size == 0 {
			decoded, size = clean[i:i+1], 1
		}
		check = append(check, decoded...)
		i += size
	}
	defer clear(check)
	for secret := range seen {
		if bytes.Contains(clean, []byte(secret)) || bytes.Contains(check, []byte(secret)) {
			clear(clean)
			return []byte{}, true
		}
	}
	return clean, true
}

// 单次JSON转义解码也适用于重复键、非JSON和截断片段；不会信任其结构。
func decodeJSONEscape(raw []byte) ([]byte, int) {
	if len(raw) < 2 || raw[0] != '\\' {
		return nil, 0
	}
	var r rune
	switch raw[1] {
	case '"', '\\', '/':
		return []byte{raw[1]}, 2
	case 'b':
		return []byte{'\b'}, 2
	case 'f':
		return []byte{'\f'}, 2
	case 'n':
		return []byte{'\n'}, 2
	case 'r':
		return []byte{'\r'}, 2
	case 't':
		return []byte{'\t'}, 2
	case 'u':
		if len(raw) < 6 {
			return nil, 0
		}
		n, err := strconv.ParseUint(string(raw[2:6]), 16, 16)
		if err != nil {
			return nil, 0
		}
		r = rune(n)
		if utf16.IsSurrogate(r) {
			if len(raw) < 12 || raw[6] != '\\' || raw[7] != 'u' {
				return nil, 0
			}
			second, e := strconv.ParseUint(string(raw[8:12]), 16, 16)
			if e != nil {
				return nil, 0
			}
			r = utf16.DecodeRune(r, rune(second))
			if r == utf8.RuneError {
				return nil, 0
			}
			return utf8.AppendRune(nil, r), 12
		}
		return utf8.AppendRune(nil, r), 6
	}
	return nil, 0
}
