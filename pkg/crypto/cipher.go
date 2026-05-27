package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
)

// MinCipherSecretLength — минимальная длина секрета, из которого выводится ключ.
const MinCipherSecretLength = 16

var (
	ErrCipherSecretTooShort = fmt.Errorf("crypto: cipher secret too short, want >= %d chars", MinCipherSecretLength)
	ErrCipherMalformed      = errors.New("crypto: ciphertext malformed")
)

// Cipher — симметричное шифрование строк через AES-256-GCM.
//
// Применяется для секретов, которые нужны позже в ОТКРЫТОМ виде (пароли/токены
// доступа к Docker registry: чтобы залогиниться в registry, нужен сам пароль).
// Это принципиально отличается от паролей пользователей — там односторонний
// bcrypt (PasswordHasher), восстановить нельзя.
//
// GCM даёт и конфиденциальность, и аутентичность (подделку шифртекста Open отвергнет).
type Cipher struct {
	gcm cipher.AEAD
}

// NewCipher выводит 32-байтный ключ AES-256 из секрета через SHA-256
// (секрет может быть произвольной длины >= MinCipherSecretLength).
func NewCipher(secret string) (*Cipher, error) {
	if len(secret) < MinCipherSecretLength {
		return nil, ErrCipherSecretTooShort
	}
	key := sha256.Sum256([]byte(secret))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, fmt.Errorf("crypto: new aes cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("crypto: new gcm: %w", err)
	}
	return &Cipher{gcm: gcm}, nil
}

// Encrypt возвращает base64(nonce || ciphertext). Nonce случайный на каждый
// вызов, поэтому один и тот же открытый текст даёт разный шифртекст.
func (c *Cipher) Encrypt(plaintext string) (string, error) {
	nonce := make([]byte, c.gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("crypto: generate nonce: %w", err)
	}
	sealed := c.gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(sealed), nil
}

// Decrypt принимает результат Encrypt и возвращает открытый текст.
// Возвращает ErrCipherMalformed для битого ввода, обёрнутую ошибку — если
// аутентификация GCM не прошла (неверный ключ / подделка).
func (c *Cipher) Decrypt(encoded string) (string, error) {
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", ErrCipherMalformed
	}
	ns := c.gcm.NonceSize()
	if len(data) < ns {
		return "", ErrCipherMalformed
	}
	nonce, ciphertext := data[:ns], data[ns:]
	plaintext, err := c.gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("crypto: decrypt: %w", err)
	}
	return string(plaintext), nil
}
