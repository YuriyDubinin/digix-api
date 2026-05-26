package crypto

import (
	"errors"
	"strings"
	"testing"
)

// В тестах используем минимальный bcrypt cost, чтобы прогон занимал миллисекунды.
const testPasswordCost = MinPasswordCost

func newTestHasher(t *testing.T) *PasswordHasher {
	t.Helper()
	h, err := NewPasswordHasher(testPasswordCost)
	if err != nil {
		t.Fatalf("NewPasswordHasher: %v", err)
	}
	return h
}

func TestPasswordHasher_HashAndVerify_Success(t *testing.T) {
	h := newTestHasher(t)

	hashed, err := h.Hash("Pa$$w0rd!")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	if hashed == "" {
		t.Fatal("Hash returned empty string")
	}
	if !strings.HasPrefix(hashed, "$2") {
		t.Errorf("Hash result does not look like bcrypt output: %q", hashed)
	}

	if err := h.Verify("Pa$$w0rd!", hashed); err != nil {
		t.Errorf("Verify: %v, want nil", err)
	}
}

func TestPasswordHasher_Verify_Mismatch(t *testing.T) {
	h := newTestHasher(t)

	hashed, err := h.Hash("correct-password")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}

	err = h.Verify("wrong-password", hashed)
	if !errors.Is(err, ErrPasswordMismatch) {
		t.Errorf("Verify with wrong password: err = %v, want ErrPasswordMismatch", err)
	}
}

func TestPasswordHasher_Hash_EmptyPassword(t *testing.T) {
	h := newTestHasher(t)
	if _, err := h.Hash(""); !errors.Is(err, ErrPasswordEmpty) {
		t.Errorf("Hash(\"\"): err = %v, want ErrPasswordEmpty", err)
	}
}

func TestPasswordHasher_Hash_TooLong(t *testing.T) {
	h := newTestHasher(t)
	longPassword := strings.Repeat("a", MaxPasswordBytes+1)
	if _, err := h.Hash(longPassword); !errors.Is(err, ErrPasswordTooLong) {
		t.Errorf("Hash(<long>): err = %v, want ErrPasswordTooLong", err)
	}
}

func TestPasswordHasher_Hash_DifferentSaltsForSamePassword(t *testing.T) {
	// bcrypt автоматически генерирует разную соль на каждый вызов:
	// два хэша одного пароля должны различаться, но оба пройти Verify.
	h := newTestHasher(t)
	hashed1, _ := h.Hash("repeated")
	hashed2, _ := h.Hash("repeated")
	if hashed1 == hashed2 {
		t.Error("Hash produced identical output twice — соль не работает?")
	}
	if err := h.Verify("repeated", hashed1); err != nil {
		t.Errorf("Verify against hashed1: %v", err)
	}
	if err := h.Verify("repeated", hashed2); err != nil {
		t.Errorf("Verify against hashed2: %v", err)
	}
}

func TestNewPasswordHasher_InvalidCost(t *testing.T) {
	cases := []int{
		MinPasswordCost - 1,
		MaxPasswordCost + 1,
		-1,
	}
	for _, cost := range cases {
		if _, err := NewPasswordHasher(cost); err == nil {
			t.Errorf("NewPasswordHasher(%d): err = nil, want error", cost)
		}
	}
}
