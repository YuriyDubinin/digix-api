package crypto

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ───────────────────────── Opaque-токен ─────────────────────────
// «Opaque» значит «непрозрачный»: токен — просто случайные байты в base64url,
// внутри него никаких данных. Все атрибуты лежат в auth_tokens (employee_id,
// expires_at и т.д.). Это основной токен сессии.

// DefaultTokenByteLength — длина опакового токена в байтах.
// 32 байта = 256 бит энтропии — с большим запасом против перебора.
const DefaultTokenByteLength = 32

// MinTokenSecretLength — минимальная длина секрета для HMAC-подписи.
// 32 байта (256 бит) — нижняя граница для безопасности HMAC-SHA256.
const MinTokenSecretLength = 32

// GenerateOpaqueToken возвращает криптостойкую случайную строку длиной
// `byteLen` байт в URL-safe base64 без паддинга. При byteLen <= 0 берёт
// DefaultTokenByteLength.
//
// Применение:
//   - сгенерировать токен при логине
//   - вернуть клиенту в открытом виде (один раз, в ответе на login)
//   - в auth_tokens.token_hash положить HashToken(<этот токен>)
//   - при последующих запросах клиент шлёт сырой токен в заголовке
//     Authorization: Bearer ... — сервис хэширует и ищет в БД по token_hash.
func GenerateOpaqueToken(byteLen int) (string, error) {
	if byteLen <= 0 {
		byteLen = DefaultTokenByteLength
	}
	buf := make([]byte, byteLen)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("crypto: generate random token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// HashToken — детерминированный SHA-256 хэш (hex) сырого токена.
// Используется и при записи в auth_tokens.token_hash, и при поиске по нему.
// Помощник один на оба слоя — критически важно, чтобы алгоритм совпадал.
//
// Внимание: для ПАРОЛЕЙ этот хэш НЕ годится (SHA-256 слишком быстр для
// перебора). Пароли — через PasswordHasher (bcrypt).
func HashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// ───────────────────────── Signed-токен с payload ─────────────────────────
// HMAC-подписанный токен с встроенными данными. Для случаев, когда серверу
// удобно прокинуть данные через клиента без хранения состояния:
//
//   • Ссылка подтверждения email (вшиваем employee_id + purpose)
//   • Ссылка сброса пароля (employee_id + expires_at)
//   • Magic-link для одноразового входа
//
// Формат: base64url(payload_json) + "." + base64url(hmac_sha256_signature)
// Подпись считается от base64url(payload) — стандартная схема, минимальный
// JWT-подобный формат без alg-флага (он у нас единственный — HMAC-SHA256).
//
// Когда НЕ использовать:
//   - основной токен сессии (используйте GenerateOpaqueToken + auth_tokens)
//   - там, где данные внутри токена могут устареть (например, role
//     сотрудника — в БД она актуальная, в токене — снимок на момент выдачи)

var (
	ErrSignedTokenMalformed = errors.New("crypto: signed token malformed")
	ErrSignedTokenInvalid   = errors.New("crypto: signed token signature invalid")
	ErrSignedTokenExpired   = errors.New("crypto: signed token expired")
)

// SignedTokenPayload — данные, встраиваемые в подписанный токен.
// Все поля помечены omitempty — для случаев, когда «вшивать ничего не надо»,
// можно сгенерировать токен с пустым payload (например, как nonce).
//
// Имена короткие (sub/prp/iat/exp/ext) — это типичная JWT-конвенция,
// делает токен компактнее.
type SignedTokenPayload struct {
	// Subject — обычно employee_id в виде строки.
	Subject string `json:"sub,omitempty"`
	// Purpose — назначение токена ("email_verify", "password_reset", ...).
	// Помогает верификатору отбить токен, выданный для другого сценария.
	Purpose string `json:"prp,omitempty"`
	// IssuedAt / ExpiresAt — unix seconds. ExpiresAt == 0 означает «без срока»
	// (используйте с осторожностью, обычно у одноразовых токенов всегда есть срок).
	IssuedAt  int64 `json:"iat,omitempty"`
	ExpiresAt int64 `json:"exp,omitempty"`
	// Extra — произвольные строковые атрибуты для гибкости.
	Extra map[string]string `json:"ext,omitempty"`
}

// IsExpired возвращает true, если ExpiresAt задан и уже наступил.
func (p *SignedTokenPayload) IsExpired(now time.Time) bool {
	if p.ExpiresAt == 0 {
		return false
	}
	return now.Unix() >= p.ExpiresAt
}

// TokenSigner подписывает и верифицирует токены с payload.
// Секрет берётся из config.Auth.TokenSecret (env AUTH_TOKEN_SECRET).
type TokenSigner struct {
	secret []byte
	clock  func() time.Time
}

// NewTokenSigner создаёт подписывальщика. Возвращает ошибку, если секрет
// короче MinTokenSecretLength — это базовое требование к безопасности HMAC.
func NewTokenSigner(secret string) (*TokenSigner, error) {
	if len(secret) < MinTokenSecretLength {
		return nil, fmt.Errorf("crypto: token signer secret too short, want >= %d chars, got %d", MinTokenSecretLength, len(secret))
	}
	return &TokenSigner{
		secret: []byte(secret),
		clock:  time.Now,
	}, nil
}

// Sign возвращает подписанный токен в формате "<payload>.<signature>".
// Не модифицирует payload (поля IssuedAt/ExpiresAt вызывающий заполняет сам —
// чтобы хелпер оставался чистым и предсказуемым).
func (s *TokenSigner) Sign(payload SignedTokenPayload) (string, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("crypto: marshal signed token payload: %w", err)
	}
	bodyEnc := base64.RawURLEncoding.EncodeToString(body)
	sig := hmacSHA256(s.secret, []byte(bodyEnc))
	sigEnc := base64.RawURLEncoding.EncodeToString(sig)
	return bodyEnc + "." + sigEnc, nil
}

// Verify проверяет подпись и срок действия. Возвращает декодированный payload
// или одну из ошибок ErrSignedToken*. Доменные проверки (например, что
// Purpose == "password_reset") — ответственность вызывающего.
func (s *TokenSigner) Verify(token string) (*SignedTokenPayload, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return nil, ErrSignedTokenMalformed
	}

	wantSig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, ErrSignedTokenMalformed
	}
	gotSig := hmacSHA256(s.secret, []byte(parts[0]))
	// hmac.Equal — constant-time, защищает от timing-атак на подбор подписи.
	if !hmac.Equal(wantSig, gotSig) {
		return nil, ErrSignedTokenInvalid
	}

	body, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, ErrSignedTokenMalformed
	}
	var payload SignedTokenPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, ErrSignedTokenMalformed
	}

	if payload.IsExpired(s.clock()) {
		return nil, ErrSignedTokenExpired
	}
	return &payload, nil
}

func hmacSHA256(key, data []byte) []byte {
	m := hmac.New(sha256.New, key)
	m.Write(data)
	return m.Sum(nil)
}
