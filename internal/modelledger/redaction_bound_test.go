package modelledger

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"wscyber/internal/domain"
	"wscyber/internal/modelconfig"
)

func TestRedactMaximumCredentialOverlappingResponse(t *testing.T) {
	doc := domain.ModelDocument{ModelConnection: domain.ModelConnection{Name: "fixture", Protocol: "openai_chat", BaseURL: "http://127.0.0.1:9/v1", Model: "fixture", Auth: "bearer", TimeoutSeconds: 1, MaxOutputTokens: 1}, Secrets: domain.ModelSecrets{APIKey: strings.Repeat("a", 8192)}}
	if _, err := modelconfig.ValidateForUse(doc); err != nil {
		t.Fatal("测试必须使用真实允许的长凭据", err)
	}
	raw := bytes.Repeat([]byte("a"), 1<<20)
	started := time.Now()
	clean, changed := redact(raw, doc.Secrets)
	t.Logf("8192字节合法重复凭据 × 1MiB重叠响应: %s", time.Since(started))
	if !changed || len(clean) != 0 {
		t.Fatal("重叠覆盖不完整", len(clean))
	}
	if time.Since(started) > 5*time.Second {
		t.Fatal("有界脱敏耗时异常")
	}
}
