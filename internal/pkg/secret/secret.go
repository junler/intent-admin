// Package secret encrypts the credentials that must live in the database
// (model provider API keys, object storage secrets) using AES-256-GCM.
//
// The key comes from app.encrypt_key and is required at startup. Losing it
// makes every stored credential unrecoverable, which is why `migrate rekey`
// exists and why the README tells operators to back it up with the database.
//
// Standard library crypto is used deliberately rather than a helper library:
// this is the one place in the project where the exact construction matters.
package secret

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// KeySize is the required key length for AES-256.
const KeySize = 32

// Cipher encrypts and decrypts credential values.
type Cipher struct {
	aead cipher.AEAD
}

// ParseKey accepts a raw 32-byte string, or a base64/hex encoding of one.
// Accepting several encodings keeps `openssl rand -base64 32` and
// `openssl rand -hex 32` both usable, which is what operators will reach for.
func ParseKey(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, errors.New("加密密钥为空")
	}
	if len(s) == KeySize {
		return []byte(s), nil
	}
	if b, err := base64.StdEncoding.DecodeString(s); err == nil && len(b) == KeySize {
		return b, nil
	}
	if b, err := hex.DecodeString(s); err == nil && len(b) == KeySize {
		return b, nil
	}
	return nil, fmt.Errorf("加密密钥必须是 %d 字节（可用 base64 或 hex 编码）", KeySize)
}

// New builds a Cipher from a configured key string.
func New(key string) (*Cipher, error) {
	raw, err := ParseKey(key)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(raw)
	if err != nil {
		return nil, fmt.Errorf("构建 AES cipher 失败: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("构建 GCM 失败: %w", err)
	}
	return &Cipher{aead: aead}, nil
}

// Encrypt returns base64(nonce || ciphertext). A fresh random nonce per call
// means the same plaintext yields different ciphertexts, so an observer of the
// table cannot tell that two providers share a key.
func (c *Cipher) Encrypt(plain string) (string, error) {
	if plain == "" {
		return "", nil
	}
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("生成 nonce 失败: %w", err)
	}
	sealed := c.aead.Seal(nonce, nonce, []byte(plain), nil)
	return base64.StdEncoding.EncodeToString(sealed), nil
}

// Decrypt reverses Encrypt. A wrong key surfaces as a readable error rather
// than a panic, because the most likely cause is a misconfigured deployment.
func (c *Cipher) Decrypt(encoded string) (string, error) {
	if encoded == "" {
		return "", nil
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("密文格式错误: %w", err)
	}
	size := c.aead.NonceSize()
	if len(raw) < size {
		return "", errors.New("密文长度不足")
	}
	plain, err := c.aead.Open(nil, raw[:size], raw[size:], nil)
	if err != nil {
		return "", errors.New("解密失败：app.encrypt_key 可能已变更，请用 migrate rekey 轮换或恢复原密钥")
	}
	return string(plain), nil
}

// Mask renders a credential for display: keeps a short prefix and suffix so a
// human can recognise which key it is, hides the rest. Short values are hidden
// entirely rather than partially revealed.
func Mask(plain string) string {
	n := len(plain)
	switch {
	case n == 0:
		return ""
	case n <= 8:
		return strings.Repeat("•", 8)
	case n <= 16:
		return plain[:2] + strings.Repeat("•", 8) + plain[n-2:]
	default:
		return plain[:5] + strings.Repeat("•", 8) + plain[n-4:]
	}
}
