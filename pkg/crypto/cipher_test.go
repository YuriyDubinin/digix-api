package crypto

import (
	"errors"
	"strings"
	"testing"
)

const testCipherSecret = "registry-encryption-secret-key-123"

func newTestCipher(t *testing.T) *Cipher {
	t.Helper()
	c, err := NewCipher(testCipherSecret)
	if err != nil {
		t.Fatalf("NewCipher: %v", err)
	}
	return c
}

func TestCipher_EncryptDecrypt_RoundTrip(t *testing.T) {
	c := newTestCipher(t)
	plaintext := "8Xk@7!mQ#2z$9rV&4fL"

	enc, err := c.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if enc == plaintext {
		t.Fatal("ciphertext equals plaintext")
	}

	dec, err := c.Decrypt(enc)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if dec != plaintext {
		t.Errorf("round-trip mismatch: got %q, want %q", dec, plaintext)
	}
}

func TestCipher_Encrypt_DifferentNonceEachTime(t *testing.T) {
	c := newTestCipher(t)
	a, _ := c.Encrypt("same")
	b, _ := c.Encrypt("same")
	if a == b {
		t.Error("two encryptions of same text are identical — nonce не случаен")
	}
	// Но обе расшифровываются в исходный текст.
	if d, _ := c.Decrypt(a); d != "same" {
		t.Errorf("decrypt a = %q", d)
	}
	if d, _ := c.Decrypt(b); d != "same" {
		t.Errorf("decrypt b = %q", d)
	}
}

func TestCipher_EmptyString(t *testing.T) {
	c := newTestCipher(t)
	enc, err := c.Encrypt("")
	if err != nil {
		t.Fatalf("Encrypt empty: %v", err)
	}
	dec, err := c.Decrypt(enc)
	if err != nil || dec != "" {
		t.Errorf("empty round-trip: dec=%q err=%v", dec, err)
	}
}

func TestCipher_Decrypt_WrongKeyFails(t *testing.T) {
	c1 := newTestCipher(t)
	c2, _ := NewCipher("totally-different-secret-key-9999")

	enc, _ := c1.Encrypt("secret")
	if _, err := c2.Decrypt(enc); err == nil {
		t.Error("Decrypt with wrong key succeeded, want error")
	}
}

func TestCipher_Decrypt_Malformed(t *testing.T) {
	c := newTestCipher(t)
	for _, in := range []string{"", "!!!notbase64!!!", "YWJj"} { // последний — валидный b64, но короче nonce
		if _, err := c.Decrypt(in); err == nil {
			t.Errorf("Decrypt(%q) succeeded, want error", in)
		}
	}
}

func TestNewCipher_ShortSecret(t *testing.T) {
	if _, err := NewCipher("short"); !errors.Is(err, ErrCipherSecretTooShort) {
		t.Errorf("NewCipher short: err = %v, want ErrCipherSecretTooShort", err)
	}
}

func TestCipher_TamperedCiphertextRejected(t *testing.T) {
	c := newTestCipher(t)
	enc, _ := c.Encrypt("payload")
	// Портим последний символ base64.
	tampered := enc[:len(enc)-1] + flip(enc[len(enc)-1])
	if _, err := c.Decrypt(tampered); err == nil {
		t.Error("tampered ciphertext accepted, GCM auth должна была отвергнуть")
	}
	_ = strings.TrimSpace
}

func flip(b byte) string {
	if b == 'A' {
		return "B"
	}
	return "A"
}
