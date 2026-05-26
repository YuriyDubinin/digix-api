package domain

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

// AuthToken — авторизационный токен сотрудника (строка из таблицы auth_tokens).
// В БД хранится только TokenHash (SHA-256 от секрета); сам секрет известен
// только клиенту, выдаётся ему один раз при логине.
//
// Поля устройства/сети опциональны и заполняются на этапе выдачи токена
// данными HTTP-запроса. При чтении токена (валидации) они не запрашиваются.
type AuthToken struct {
	ID            uuid.UUID
	EmployeeID    uuid.UUID
	TokenHash     string
	TokenType     string // ENUM auth_token_type: ACCESS | REFRESH
	IssuedAt      time.Time
	ExpiresAt     time.Time
	LastUsedAt    *time.Time
	RevokedAt     *time.Time
	RevokedReason string

	// Сеть
	IPAddress string // строка IP (без порта); пусто = NULL в БД
	UserAgent string // сырая строка из заголовка User-Agent

	// Устройство и система клиента — best-effort, парсятся из UA
	DeviceType     string // ENUM auth_device_type: WEB|MOBILE|TABLET|DESKTOP|API|UNKNOWN
	DeviceName     string // клиент может прислать через X-Device-Name
	OS             string
	OSVersion      string
	Browser        string
	BrowserVersion string
	AppVersion     string // клиент может прислать через X-App-Version

	// Геопозиция (заполняется в будущем, через GeoIP-сервис)
	CountryCode string
	City        string
}

// Значения ENUM auth_token_type из миграции 000003.
const (
	TokenTypeAccess  = "ACCESS"
	TokenTypeRefresh = "REFRESH"
)

// Значения ENUM auth_device_type из миграции 000003.
const (
	DeviceTypeWeb     = "WEB"
	DeviceTypeMobile  = "MOBILE"
	DeviceTypeTablet  = "TABLET"
	DeviceTypeDesktop = "DESKTOP"
	DeviceTypeAPI     = "API"
	DeviceTypeUnknown = "UNKNOWN"
)

// IsActive возвращает true, если токен не отозван и ещё не истёк.
func (t *AuthToken) IsActive(now time.Time) bool {
	if t.RevokedAt != nil {
		return false
	}
	return now.Before(t.ExpiresAt)
}

// AuthTokenWithEmployee — токен вместе с актуальным role/status владельца.
// Используется как результат JOIN-запроса в репозитории, чтобы service-слой
// мог собрать Principal без повторного похода в БД.
type AuthTokenWithEmployee struct {
	Token          AuthToken
	EmployeeRole   string
	EmployeeStatus string
}

// Principal — идентификатор аутентифицированного пользователя.
// Кладётся в context.Context мидлварью Auth, после чего доступен в любом
// защищённом хендлере через middleware.PrincipalFromContext.
type Principal struct {
	EmployeeID uuid.UUID
	Role       string
	Status     string
}

// Authenticator — узкий контракт проверки сырого токена из заголовка.
// Реализация — *service.AuthService. Router зависит от этого интерфейса,
// а не от конкретного типа сервиса.
type Authenticator interface {
	Authenticate(ctx context.Context, rawToken string) (*Principal, error)
}

// Auth-ошибки. Мидлварь маппит их на 401 с разными error-кодами в payload,
// чтобы клиент мог различать причины отказа (истёк / отозван / disabled).
//
// ErrInvalidCredentials — единая ошибка для логина: «нет такого email»,
// «пароль не совпал» или «сотрудник DISABLED». Намеренно одинаковая, чтобы
// клиент (и атакующий) не мог определить, существует ли email в системе.
var (
	ErrUnauthenticated   = errors.New("unauthenticated")
	ErrTokenInvalid      = errors.New("token invalid")
	ErrTokenExpired      = errors.New("token expired")
	ErrTokenRevoked      = errors.New("token revoked")
	ErrEmployeeDisabled  = errors.New("employee disabled")
	ErrInvalidCredentials = errors.New("invalid credentials")
)
