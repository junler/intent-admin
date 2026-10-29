package secret

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"strings"
	"testing"
)

const key32 = "0123456789abcdef0123456789abcdef"

func TestParseKeyAcceptsRawBase64AndHex(t *testing.T) {
	raw := []byte(key32)
	for name, input := range map[string]string{
		"raw":    key32,
		"base64": base64.StdEncoding.EncodeToString(raw),
		"hex":    hex.EncodeToString(raw),
		"padded": "  " + base64.StdEncoding.EncodeToString(raw) + "\n",
	} {
		got, err := ParseKey(input)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if !bytes.Equal(got, raw) {
			t.Errorf("%s: 解析结果不一致", name)
		}
	}
}

func TestParseKeyRejectsWrongLength(t *testing.T) {
	for _, bad := range []string{"", "short", strings.Repeat("x", 31), strings.Repeat("x", 33)} {
		if _, err := ParseKey(bad); err == nil {
			t.Errorf("ParseKey(%q) 应当报错", bad)
		}
	}
}

func TestRoundTrip(t *testing.T) {
	c, err := New(key32)
	if err != nil {
		t.Fatal(err)
	}
	for _, plain := range []string{"sk-abcdef123456", "", "中文密钥", strings.Repeat("x", 500)} {
		enc, err := c.Encrypt(plain)
		if err != nil {
			t.Fatalf("Encrypt(%q): %v", plain, err)
		}
		dec, err := c.Decrypt(enc)
		if err != nil {
			t.Fatalf("Decrypt: %v", err)
		}
		if dec != plain {
			t.Errorf("往返不一致: got %q, want %q", dec, plain)
		}
	}
}

func TestNonceIsRandomPerCall(t *testing.T) {
	c, _ := New(key32)
	a, _ := c.Encrypt("same-secret")
	b, _ := c.Encrypt("same-secret")
	// Identical plaintexts must not produce identical ciphertexts, otherwise
	// anyone reading the table can tell which providers share a key.
	if a == b {
		t.Fatal("相同明文两次加密的密文相同，说明 nonce 没有随机化")
	}
}

func TestWrongKeyFailsReadably(t *testing.T) {
	c1, _ := New(key32)
	c2, _ := New("fedcba9876543210fedcba9876543210")
	enc, _ := c1.Encrypt("sk-secret")

	_, err := c2.Decrypt(enc)
	if err == nil {
		t.Fatal("用错误的密钥解密应当失败")
	}
	// The most likely cause is a misconfigured deployment, so the message must
	// point at the fix rather than leaking crypto internals.
	if !strings.Contains(err.Error(), "encrypt_key") {
		t.Errorf("错误信息应提示 encrypt_key，实际 %v", err)
	}
}

func TestDecryptRejectsGarbage(t *testing.T) {
	c, _ := New(key32)
	for _, bad := range []string{"not-base64!!", base64.StdEncoding.EncodeToString([]byte("tiny"))} {
		if _, err := c.Decrypt(bad); err == nil {
			t.Errorf("Decrypt(%q) 应当报错", bad)
		}
	}
}

func TestDecryptRejectsTamperedCiphertext(t *testing.T) {
	c, err := New(key32)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := c.Encrypt("sk-secret")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	raw[len(raw)-1] ^= 1
	if _, err := c.Decrypt(base64.StdEncoding.EncodeToString(raw)); err == nil {
		t.Fatal("被篡改的密文应当报错")
	}
}

func TestMaskHidesMiddleAndNeverLeaksShortSecrets(t *testing.T) {
	cases := map[string]func(string) bool{
		"":              func(s string) bool { return s == "" },
		"short":         func(s string) bool { return !strings.Contains(s, "short") },
		"sk-1234567890": func(s string) bool { return strings.Contains(s, "•") },
		"sk-proj-abcdefghijklmnop": func(s string) bool {
			return strings.HasPrefix(s, "sk-pr") && strings.HasSuffix(s, "mnop")
		},
	}
	for input, check := range cases {
		got := Mask(input)
		if !check(got) {
			t.Errorf("Mask(%q) = %q，不符合预期", input, got)
		}
		if input != "" && got == input {
			t.Errorf("Mask(%q) 原样返回了明文", input)
		}
	}
}
