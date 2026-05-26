package crypto

import (
	"errors"
	"strings"
	"testing"
	"time"
)

const testSecret = "0123456789abcdef0123456789abcdef" // ровно 32 символа

// ───────────────────────── Opaque-токен ─────────────────────────

func TestGenerateOpaqueToken_DefaultLength(t *testing.T) {
	tok, err := GenerateOpaqueToken(0) // 0 → DefaultTokenByteLength
	if err != nil {
		t.Fatalf("GenerateOpaqueToken: %v", err)
	}
	if tok == "" {
		t.Fatal("empty token")
	}
	// base64url без паддинга: 32 байта → 43 символа
	if want := 43; len(tok) != want {
		t.Errorf("len = %d, want %d", len(tok), want)
	}
}

func TestGenerateOpaqueToken_Uniqueness(t *testing.T) {
	seen := make(map[string]struct{}, 100)
	for i := 0; i < 100; i++ {
		tok, err := GenerateOpaqueToken(16)
		if err != nil {
			t.Fatalf("GenerateOpaqueToken: %v", err)
		}
		if _, dup := seen[tok]; dup {
			t.Fatalf("duplicate token generated: %s", tok)
		}
		seen[tok] = struct{}{}
	}
}

func TestHashToken_Deterministic(t *testing.T) {
	a := HashToken("hello")
	b := HashToken("hello")
	if a != b {
		t.Errorf("HashToken non-deterministic: %q vs %q", a, b)
	}
	// SHA-256 hex = 64 символа
	if len(a) != 64 {
		t.Errorf("len = %d, want 64", len(a))
	}
}

func TestHashToken_DifferentInputs(t *testing.T) {
	if HashToken("a") == HashToken("b") {
		t.Error("HashToken should differ for different inputs")
	}
}

// ───────────────────────── Signed-токен ─────────────────────────

func newTestSigner(t *testing.T) *TokenSigner {
	t.Helper()
	s, err := NewTokenSigner(testSecret)
	if err != nil {
		t.Fatalf("NewTokenSigner: %v", err)
	}
	return s
}

func TestNewTokenSigner_TooShortSecret(t *testing.T) {
	if _, err := NewTokenSigner("short"); err == nil {
		t.Error("NewTokenSigner accepted too-short secret")
	}
}

func TestTokenSigner_SignVerify_RoundTrip(t *testing.T) {
	s := newTestSigner(t)
	in := SignedTokenPayload{
		Subject:   "employee-123",
		Purpose:   "password_reset",
		IssuedAt:  time.Now().Unix(),
		ExpiresAt: time.Now().Add(15 * time.Minute).Unix(),
		Extra:     map[string]string{"email": "u@example.com"},
	}

	tok, err := s.Sign(in)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if !strings.Contains(tok, ".") {
		t.Fatalf("token does not look like <payload>.<sig>: %q", tok)
	}

	out, err := s.Verify(tok)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if out.Subject != in.Subject || out.Purpose != in.Purpose {
		t.Errorf("payload mismatch: got %+v, want %+v", out, in)
	}
	if got, want := out.Extra["email"], in.Extra["email"]; got != want {
		t.Errorf("Extra[email] = %q, want %q", got, want)
	}
}

func TestTokenSigner_SignVerify_EmptyPayload(t *testing.T) {
	// «вшивать ничего не надо» — должно работать.
	s := newTestSigner(t)
	tok, err := s.Sign(SignedTokenPayload{})
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if _, err := s.Verify(tok); err != nil {
		t.Errorf("Verify empty payload: %v", err)
	}
}

func TestTokenSigner_Verify_TamperedSignature(t *testing.T) {
	s := newTestSigner(t)
	tok, _ := s.Sign(SignedTokenPayload{Subject: "x"})

	// Меняем последний символ подписи.
	tampered := tok[:len(tok)-1] + "A"
	if tampered == tok {
		tampered = tok[:len(tok)-1] + "B"
	}

	_, err := s.Verify(tampered)
	if !errors.Is(err, ErrSignedTokenInvalid) {
		t.Errorf("Verify tampered sig: err = %v, want ErrSignedTokenInvalid", err)
	}
}

func TestTokenSigner_Verify_TamperedPayload(t *testing.T) {
	s := newTestSigner(t)
	tok, _ := s.Sign(SignedTokenPayload{Subject: "x"})

	parts := strings.Split(tok, ".")
	if len(parts) != 2 {
		t.Fatalf("unexpected token format: %q", tok)
	}
	// Подменяем первый символ payload — подпись больше не сойдётся.
	tampered := "A" + parts[0][1:] + "." + parts[1]

	_, err := s.Verify(tampered)
	if !errors.Is(err, ErrSignedTokenInvalid) && !errors.Is(err, ErrSignedTokenMalformed) {
		t.Errorf("Verify tampered payload: err = %v, want ErrSignedTokenInvalid or ErrSignedTokenMalformed", err)
	}
}

func TestTokenSigner_Verify_Expired(t *testing.T) {
	s := newTestSigner(t)
	tok, err := s.Sign(SignedTokenPayload{
		Subject:   "x",
		ExpiresAt: time.Now().Add(-time.Minute).Unix(),
	})
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	_, err = s.Verify(tok)
	if !errors.Is(err, ErrSignedTokenExpired) {
		t.Errorf("Verify expired: err = %v, want ErrSignedTokenExpired", err)
	}
}

func TestTokenSigner_Verify_NoExpirationOK(t *testing.T) {
	// ExpiresAt == 0 — без срока, не должно отбиваться как expired.
	s := newTestSigner(t)
	tok, _ := s.Sign(SignedTokenPayload{Subject: "x"})
	if _, err := s.Verify(tok); err != nil {
		t.Errorf("Verify no-exp token: %v", err)
	}
}

func TestTokenSigner_Verify_Malformed(t *testing.T) {
	s := newTestSigner(t)
	cases := []string{
		"",
		"no-dot-here",
		".onlydot",
		"only-dot.",
		"too.many.dots",
		"!!invalid_b64.??",
	}
	for _, in := range cases {
		_, err := s.Verify(in)
		if err == nil {
			t.Errorf("Verify(%q): err = nil, want some error", in)
		}
	}
}

func TestTokenSigner_DifferentSecrets_RejectEachOther(t *testing.T) {
	s1, _ := NewTokenSigner("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	s2, _ := NewTokenSigner("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")

	tok, _ := s1.Sign(SignedTokenPayload{Subject: "x"})
	if _, err := s2.Verify(tok); !errors.Is(err, ErrSignedTokenInvalid) {
		t.Errorf("Verify with different secret: err = %v, want ErrSignedTokenInvalid", err)
	}
}
