package modelconfig

import (
	"crypto/rand"
	"fmt"

	"wscyber/internal/domain"
)

func contentAAD(id, kind string) []byte {
	return []byte("wscyber/model-call/content/v1/" + id + "/" + kind)
}
func validContent(id, kind string, n int) bool {
	return ValidID(id) && (kind == "input" || kind == "response") && n <= 2<<20
}

// SealContent 与配置密文使用不同AAD；输入与返回也不可互换。
func (v *Vault) SealContent(id, kind string, body []byte) ([]byte, error) {
	if v == nil || !validContent(id, kind, len(body)) {
		return nil, domain.ErrUnavailable
	}
	nonce := make([]byte, v.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, domain.ErrUnavailable
	}
	return v.aead.Seal(append([]byte{1}, nonce...), nonce, body, contentAAD(id, kind)), nil
}
func (v *Vault) OpenContent(id, kind string, sealed []byte) ([]byte, error) {
	bad := func() ([]byte, error) {
		return nil, fmt.Errorf("%w：模型原文无法解密，请核对本机密钥及数据完整性", domain.ErrUnavailable)
	}
	if v == nil || !validContent(id, kind, 0) {
		return bad()
	}
	n := v.aead.NonceSize()
	if len(sealed) < 1+n+v.aead.Overhead() || len(sealed) > 2<<20+1+n+v.aead.Overhead() || sealed[0] != 1 {
		return bad()
	}
	b, err := v.aead.Open(nil, sealed[1:1+n], sealed[1+n:], contentAAD(id, kind))
	if err != nil {
		return bad()
	}
	return b, nil
}

func (m *Manager) MatchesModel(r domain.SealedModelConfig, protocol, model string) bool {
	if !m.Available() {
		return false
	}
	doc, err := m.document(r)
	return err == nil && doc.Protocol == protocol && doc.Model == model
}
