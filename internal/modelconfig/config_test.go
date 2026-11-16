package modelconfig

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"wscyber/internal/domain"
)

const testID = "00112233445566778899aabbccddeeff"

func testVault(t *testing.T) *Vault {
	t.Helper()
	v, err := NewVault(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{42}, 32)))
	if err != nil {
		t.Fatal(err)
	}
	return v
}
func input() domain.ModelConfigWrite {
	return domain.ModelConfigWrite{ModelConnection: domain.ModelConnection{Name: "本地模型", Protocol: "openai_chat", BaseURL: "http://127.0.0.1:12345/v1", Model: "fixture", Auth: "bearer", TimeoutSeconds: 30, MaxOutputTokens: 4096}, Enabled: true,
		APIKey:     domain.SecretChange[string]{Mode: "replace", Value: "private-key"},
		Headers:    domain.SecretChange[map[string]string]{Mode: "replace", Value: map[string]string{"X-Private": "header-secret"}},
		Parameters: domain.SecretChange[map[string]json.RawMessage]{Mode: "replace", Value: map[string]json.RawMessage{"temperature": json.RawMessage(`0.2`), "metadata": json.RawMessage(`{"note":"parameter-secret"}`)}},
	}
}
func TestVaultBoundedAuthenticatedEncryption(t *testing.T) {
	v := testVault(t)
	plain := []byte("private configuration")
	a, err := v.seal(testID, 1, plain)
	if err != nil {
		t.Fatal(err)
	}
	b, err := v.seal(testID, 1, plain)
	if err != nil || bytes.Equal(a, b) || bytes.Contains(a, plain) {
		t.Fatal("nonce应随机且不得保存明文", err)
	}
	p, err := v.open(testID, 1, a)
	if err != nil || !bytes.Equal(p, plain) {
		t.Fatal(err)
	}
	wrong, _ := NewVault(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{3}, 32)))
	if _, err := wrong.open(testID, 1, a); err == nil {
		t.Fatal("错密钥不得解密")
	}
	for _, pair := range []struct {
		id       string
		revision int64
	}{{testID, 2}, {"ffeeddccbbaa99887766554433221100", 1}} {
		if _, err := v.open(pair.id, pair.revision, a); err == nil {
			t.Fatal("ID/版本替换不得解密")
		}
	}
	for i := range a {
		changed := bytes.Clone(a)
		changed[i] ^= 1
		if _, err := v.open(testID, 1, changed); err == nil {
			t.Fatal("篡改未拒绝", i)
		}
	}
	for _, bad := range []string{"", "not-base64", base64.StdEncoding.EncodeToString(make([]byte, 31)), base64.StdEncoding.EncodeToString(make([]byte, 32)) + "\n"} {
		if _, err := NewVault(bad); err == nil {
			t.Fatal("非法key被接受")
		}
	}
	if _, err := v.seal(testID, 1, make([]byte, maxDocumentBytes+1)); err == nil {
		t.Fatal("超限未拒绝")
	}
}

func TestConnectionValidation(t *testing.T) {
	for _, endpoint := range []string{"https://api.example.test/v1/", "http://127.0.0.1:3456/v1", "http://[::1]:1234/", "https://10.0.0.1/api"} {
		if err := validateEndpoint(endpoint); err != nil {
			t.Fatal(endpoint, err)
		}
	}
	for _, endpoint := range []string{"http://example.test", "http://localhost:9999", "https://user:pass@host/v1", "https://host?secret=x", "https://host?", "https://host#", "https://host/%2e%2e/", "https://host/a/../v1", "https://host//v1", "https://host:0", "https://host:65536", "https://host:", "https://host\\evil", "https://169.254.169.254", "https://[::]", "https://[ff02::1]", "https://bad_host", " https://host"} {
		if err := validateEndpoint(endpoint); !errors.Is(err, domain.ErrInvalid) {
			t.Fatal("接受了非法endpoint", endpoint, err)
		}
	}
	for _, mutate := range []func(*domain.ModelConnection){func(c *domain.ModelConnection) { c.Protocol = "unknown" }, func(c *domain.ModelConnection) { c.TimeoutSeconds = 0 }, func(c *domain.ModelConnection) { c.MaxOutputTokens = 32769 }, func(c *domain.ModelConnection) { c.Auth = "api_key" }, func(c *domain.ModelConnection) { c.Model = "x\ny" }} {
		c := input().ModelConnection
		mutate(&c)
		if err := validateConnection(c); !errors.Is(err, domain.ErrInvalid) {
			t.Fatal(err)
		}
	}
}

func TestHeadersAndParameterProtection(t *testing.T) {
	for _, headers := range []map[string]string{
		{"Authorization": "secret"}, {"hOsT": "other"}, {"Content-Type": "text/plain"}, {"X-API-Key": "secret"}, {"X-Forwarded-Host": "other"}, {"X-Stainless-Retry-Count": "2"}, {"Connection": "X-Private"}, {"X-A": "one", "x-a": "two"}, {"X-A": "a\r\nb"}, {"bad name": "x"},
	} {
		if _, err := normalizeHeaders(headers); !errors.Is(err, domain.ErrInvalid) {
			t.Fatal("非法Header未拒绝", err)
		}
	}
	h, err := normalizeHeaders(map[string]string{"x-custom": ""})
	if err != nil || h["X-Custom"] != "" || len(h) != 1 {
		t.Fatal(h, err)
	}
	for _, key := range []string{"model", "messages", "messages.0.role", "stream", "max_tokens", "tools", "extra_body", "response_format", "Max_tokens", "x\\.y", "n"} {
		if err := validateParameters(map[string]json.RawMessage{key: json.RawMessage(`1`)}); !errors.Is(err, domain.ErrInvalid) {
			t.Fatal("非法参数未拒绝", key, err)
		}
	}
	for _, raw := range []string{`{"a":1,"a":2}`, `{"a":1,"a":2}`, `1 2`, strings.Repeat("[", 18) + "1" + strings.Repeat("]", 18)} {
		if err := validateParameters(map[string]json.RawMessage{"metadata": json.RawMessage(raw)}); !errors.Is(err, domain.ErrInvalid) {
			t.Fatal("非法JSON未拒绝", err)
		}
	}
}

func TestDecodeRejectsAmbiguousSecretChanges(t *testing.T) {
	raw, err := json.Marshal(input())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeWrite(raw); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{
		strings.Replace(string(raw), `"enabled":true`, `"enabled":true,"enabled":false`, 1),
		strings.Replace(string(raw), `"enabled":true`, `"Enabled":true`, 1),
		strings.Replace(string(raw), `"enabled":true`, `"enabled":null`, 1),
		strings.Replace(string(raw), `"mode":"replace","value":"private-key"`, `"mode":"keep","value":""`, 1),
		strings.Replace(string(raw), `"mode":"replace","value":"private-key"`, `"mode":"replace"`, 1),
		strings.Replace(string(raw), `"mode":"replace","value":"private-key"`, `"mode":"replace","value":null`, 1),
		string(raw) + ` {}`, `null`, `[]`,
	} {
		if _, err := DecodeWrite([]byte(bad)); !errors.Is(err, domain.ErrInvalid) {
			t.Fatal("歧义字段未拒绝", err)
		}
	}
	for _, bad := range []string{`{"default_config_id":"","expected_revision":null}`, `{"Default_config_id":"","expected_revision":0}`, `{"default_config_id":"","expected_revision":0,"expected_revision":1}`} {
		if _, _, err := DecodeDefault([]byte(bad)); !errors.Is(err, domain.ErrInvalid) {
			t.Fatal(err)
		}
	}
}

type memoryRepo struct {
	domain.ModelConfigRepository
	record domain.SealedModelConfig
	writes int
}

func (r *memoryRepo) GetModelConfig(context.Context, string, int64) (domain.SealedModelConfig, error) {
	if r.record.ID == "" {
		return r.record, domain.ErrNotFound
	}
	return r.record, nil
}
func (r *memoryRepo) SaveModelConfig(_ context.Context, in domain.SealedModelConfig, expected int64) (domain.SealedModelConfig, error) {
	if r.record.Revision != expected {
		return domain.SealedModelConfig{}, domain.ErrConflict
	}
	in.CreatedAt = time.Now()
	r.record = in
	r.writes++
	return in, nil
}
func TestManagerSecretLifecycleAndSafeViews(t *testing.T) {
	ctx := context.Background()
	repo := &memoryRepo{}
	m := New(repo, testVault(t))
	in := input()
	v, err := m.Save(ctx, testID, in)
	if err != nil || v.Revision != 1 || !v.APIKeySet {
		t.Fatal(v, err)
	}
	encoded, _ := json.Marshal(v)
	for _, secret := range []string{"private-key", "header-secret", "parameter-secret"} {
		if bytes.Contains(encoded, []byte(secret)) || bytes.Contains(repo.record.Ciphertext, []byte(secret)) {
			t.Fatal("秘密泄漏")
		}
	}
	if _, err := m.Save(ctx, testID, in); !errors.Is(err, domain.ErrConflict) {
		t.Fatal("重复创建必须冲突", err)
	}
	in.ExpectedRevision = 1
	in.Name = "改名"
	in.APIKey = domain.SecretChange[string]{Mode: "keep"}
	in.Headers = domain.SecretChange[map[string]string]{Mode: "keep"}
	in.Parameters = domain.SecretChange[map[string]json.RawMessage]{Mode: "keep"}
	v, err = m.Save(ctx, testID, in)
	if err != nil || v.Revision != 2 || !v.APIKeySet {
		t.Fatal(v, err)
	}
	doc, err := m.document(repo.record)
	if err != nil || doc.Secrets.APIKey != "private-key" {
		t.Fatal(err)
	}
	in.ExpectedRevision = 2
	in.BaseURL = "https://other.test/v1"
	if _, err := m.Save(ctx, testID, in); !errors.Is(err, domain.ErrInvalid) || repo.writes != 2 {
		t.Fatal("接收方变化不应保留秘密", err)
	}
	in.APIKey = domain.SecretChange[string]{Mode: "clear"}
	in.Headers = domain.SecretChange[map[string]string]{Mode: "clear"}
	in.Parameters = domain.SecretChange[map[string]json.RawMessage]{Mode: "clear"}
	if _, err := m.Save(ctx, testID, in); !errors.Is(err, domain.ErrInvalid) {
		t.Fatal("启用bearer不得缺凭据", err)
	}
	in.Enabled = false
	v, err = m.Save(ctx, testID, in)
	if err != nil || v.APIKeySet || len(v.HeaderNames) != 0 || len(v.ParameterKeys) != 0 {
		t.Fatal(v, err)
	}
	if _, err := New(repo, nil).Get(ctx, testID); !errors.Is(err, domain.ErrUnavailable) {
		t.Fatal("缺密钥必须不可用", err)
	}
	wrong, _ := NewVault(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{3}, 32)))
	if _, err := New(repo, wrong).Get(ctx, testID); err == nil {
		t.Fatal("错密钥不能冒充空配置")
	}
}
