package webapi

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"syscall"
)

// 资产测试账号 secret 的静态加密：AES-256-GCM，密钥文件 <state-dir>/webapi-secrets.key
// （64 位 hex，0600 普通文件）。AAD 绑定 asset_id+credential_id，密文不能挪到别的行。
const (
	secretKeyFileName = "webapi-secrets.key"
	secretCipherV1    = "v1:"
)

var (
	errSecretKeyMissing = errors.New("密钥文件 webapi-secrets.key 缺失，已保存的测试账号密文无法解密；请恢复原密钥文件（不会生成新密钥覆盖）")
	errSecretKeyInvalid = errors.New("密钥文件 webapi-secrets.key 不可用：须为权限 0600 的普通文件，内容为 64 位 hex")
	errSecretDecrypt    = errors.New("测试账号密文无法用当前密钥解密（密钥文件可能被替换）")
)

// secretBox 懒加载密钥：首次需要加密且库中没有任何密文时生成；已有密文而密钥缺失时报错。
type secretBox struct {
	path string
	mu   sync.Mutex
	aead cipher.AEAD
}

func newSecretBox(path string) *secretBox { return &secretBox{path: path} }

// load 读取密钥；文件不存在时 allowCreate=true 才生成（O_EXCL，0600）。
func (b *secretBox) load(allowCreate bool) (cipher.AEAD, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.aead != nil {
		return b.aead, nil
	}
	if b.path == "" {
		return nil, errSecretKeyInvalid
	}
	key, err := readSecretKey(b.path)
	if errors.Is(err, os.ErrNotExist) {
		if !allowCreate {
			return nil, errSecretKeyMissing
		}
		key, err = createSecretKey(b.path)
	}
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	b.aead = aead
	return aead, nil
}

func readSecretKey(path string) ([]byte, error) {
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, os.ErrNotExist
		}
		return nil, errSecretKeyInvalid
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
		return nil, errSecretKeyInvalid
	}
	raw, err := io.ReadAll(io.LimitReader(file, 256))
	if err != nil {
		return nil, errSecretKeyInvalid
	}
	key, err := hex.DecodeString(strings.TrimSpace(string(raw)))
	if err != nil || len(key) != 32 {
		return nil, errSecretKeyInvalid
	}
	return key, nil
}

func createSecretKey(path string) ([]byte, error) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, fmt.Errorf("生成密钥文件 webapi-secrets.key: %w", err)
	}
	if _, err := file.WriteString(hex.EncodeToString(key) + "\n"); err != nil {
		file.Close()
		_ = os.Remove(path)
		return nil, fmt.Errorf("写密钥文件: %w", err)
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return nil, err
	}
	if err := file.Close(); err != nil {
		return nil, err
	}
	// umask 可能收窄但不会放宽；再显式确保 0600。
	if err := os.Chmod(path, 0600); err != nil {
		return nil, err
	}
	return key, nil
}

func secretAAD(assetID, credentialID string) []byte {
	return []byte("webapi-asset-credential\x00" + assetID + "\x00" + credentialID)
}

func sealSecret(aead cipher.AEAD, assetID, credentialID, plain string) (string, error) {
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	sealed := aead.Seal(nonce, nonce, []byte(plain), secretAAD(assetID, credentialID))
	return secretCipherV1 + base64.StdEncoding.EncodeToString(sealed), nil
}

func openSecret(aead cipher.AEAD, assetID, credentialID, stored string) (string, error) {
	if !strings.HasPrefix(stored, secretCipherV1) {
		return "", errSecretDecrypt
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(stored, secretCipherV1))
	if err != nil || len(raw) < aead.NonceSize() {
		return "", errSecretDecrypt
	}
	plain, err := aead.Open(nil, raw[:aead.NonceSize()], raw[aead.NonceSize():], secretAAD(assetID, credentialID))
	if err != nil {
		return "", errSecretDecrypt
	}
	return string(plain), nil
}
