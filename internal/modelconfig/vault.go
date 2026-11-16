// Package modelconfig 管理显式配置及加密版本，不在保存/读取时调用供应商。
package modelconfig

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
)

const maxDocumentBytes = 96 << 10

// Vault 不保存主密钥的字符串形式，不生成替代密钥；部署方负责独立备份。
type Vault struct{ aead cipher.AEAD }

func NewVault(encodedKey string) (*Vault, error) {
	key, err := base64.StdEncoding.Strict().DecodeString(encodedKey)
	if err != nil || len(key) != 32 || base64.StdEncoding.EncodeToString(key) != encodedKey {
		return nil, errors.New("模型配置加密密钥必须为标准 base64 编码的32随机字节")
	}
	defer clear(key)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, errors.New("无法初始化模型配置加密")
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, errors.New("无法初始化模型配置加密")
	}
	return &Vault{aead: aead}, nil
}

func configAAD(id string, revision int64) []byte {
	return []byte(fmt.Sprintf("wscyber/model-config/v1/%s/%d", id, revision))
}

func (v *Vault) seal(id string, revision int64, plaintext []byte) ([]byte, error) {
	if v == nil || !ValidID(id) || revision < 1 || len(plaintext) > maxDocumentBytes {
		return nil, errors.New("模型配置无法加密")
	}
	nonce := make([]byte, v.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, errors.New("模型配置随机数生成失败")
	}
	// 首字节为密文格式版本；不是数据库/业务配置版本。
	out := append([]byte{1}, nonce...)
	return v.aead.Seal(out, nonce, plaintext, configAAD(id, revision)), nil
}

func (v *Vault) open(recordID string, revision int64, sealed []byte) ([]byte, error) {
	if v == nil || !ValidID(recordID) || revision < 1 {
		return nil, errors.New("模型配置无法解密，请核对部署密钥")
	}
	n := v.aead.NonceSize()
	if len(sealed) < 1+n+v.aead.Overhead() || len(sealed) > 1+n+v.aead.Overhead()+maxDocumentBytes || sealed[0] != 1 {
		return nil, errors.New("模型配置密文格式无效")
	}
	plaintext, err := v.aead.Open(nil, sealed[1:1+n], sealed[1+n:], configAAD(recordID, revision))
	if err != nil {
		return nil, errors.New("模型配置无法解密，请核对部署密钥和数据完整性")
	}
	return plaintext, nil
}
