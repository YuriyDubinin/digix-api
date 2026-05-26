// Package crypto — низкоуровневые крипто-помощники: хэширование паролей,
// генерация и подпись токенов. Без бизнес-логики, переиспользуемо.
package crypto

import (
	"errors"
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

const (
	// MinPasswordCost / MaxPasswordCost — границы bcrypt cost factor.
	MinPasswordCost = bcrypt.MinCost // 4 — допустимо только в тестах
	MaxPasswordCost = bcrypt.MaxCost // 31

	// DefaultPasswordCost — прод-дефолт. На современной машине ~250ms,
	// что даёт хороший баланс между UX логина и сопротивлением перебору.
	DefaultPasswordCost = 12

	// MaxPasswordBytes — жёсткий лимит bcrypt: всё, что длиннее, ОБРЕЗАЕТСЯ
	// без ошибки. Чтобы не получать silent truncation, явно отбиваем длинные
	// пароли на уровне хелпера.
	MaxPasswordBytes = 72
)

var (
	ErrPasswordEmpty    = errors.New("crypto: password is empty")
	ErrPasswordTooLong  = fmt.Errorf("crypto: password exceeds %d bytes (bcrypt limit)", MaxPasswordBytes)
	ErrPasswordMismatch = errors.New("crypto: password mismatch")
)

// PasswordHasher хэширует пароли через bcrypt с заданным cost factor.
// Соль bcrypt генерирует автоматически и зашивает в результат — отдельной
// env-переменной для соли не требуется.
//
// Использовать так:
//
//	h, err := crypto.NewPasswordHasher(crypto.DefaultPasswordCost)
//	hashed, _ := h.Hash("Pa$$w0rd")          // -> "$2a$12$..."
//	err = h.Verify("Pa$$w0rd", hashed)        // nil, если пароль совпал
type PasswordHasher struct {
	cost int
}

// NewPasswordHasher проверяет границы cost и возвращает готовый хешер.
// При cost вне [MinPasswordCost, MaxPasswordCost] возвращает ошибку.
func NewPasswordHasher(cost int) (*PasswordHasher, error) {
	if cost < MinPasswordCost || cost > MaxPasswordCost {
		return nil, fmt.Errorf("crypto: bcrypt cost %d out of range [%d, %d]", cost, MinPasswordCost, MaxPasswordCost)
	}
	return &PasswordHasher{cost: cost}, nil
}

// Cost возвращает текущий cost factor (для логов/тестов).
func (h *PasswordHasher) Cost() int { return h.cost }

// Hash возвращает bcrypt-хэш пароля. Результат — стандартная bcrypt-строка
// формата "$2a$<cost>$<22-char-salt><31-char-hash>", безопасная для записи
// в employees.password_hash.
func (h *PasswordHasher) Hash(raw string) (string, error) {
	if raw == "" {
		return "", ErrPasswordEmpty
	}
	if len(raw) > MaxPasswordBytes {
		return "", ErrPasswordTooLong
	}
	hashed, err := bcrypt.GenerateFromPassword([]byte(raw), h.cost)
	if err != nil {
		return "", fmt.Errorf("crypto: hash password: %w", err)
	}
	return string(hashed), nil
}

// Verify сверяет сырой пароль с сохранённым в БД bcrypt-хэшем.
// Возвращает ErrPasswordMismatch, если пароль не совпал; nil — если ок;
// другую обёрнутую ошибку — при внутренней ошибке bcrypt.
//
// Сравнение внутри bcrypt — constant-time, безопасно к timing-атакам.
func (h *PasswordHasher) Verify(raw, hashed string) error {
	if err := bcrypt.CompareHashAndPassword([]byte(hashed), []byte(raw)); err != nil {
		if errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
			return ErrPasswordMismatch
		}
		return fmt.Errorf("crypto: verify password: %w", err)
	}
	return nil
}
