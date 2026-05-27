package service

import (
	"time"

	"github.com/google/uuid"
)

type CreateFeedbackInput struct {
	Name    string
	Email   string
	Phone   string
	Subject string
	Message string
}

type CreateFeedbackOutput struct {
	ID        uuid.UUID
	Status    string
	CreatedAt time.Time
}

// ───────────────────────── Авторизация / Логин ─────────────────────────

// ClientInfo — метаданные клиента, извлечённые HTTP-слоем из запроса.
// Service-слой получает их из handler и кладёт в auth_tokens как есть.
// Все поля опциональны (пустая строка → NULL в БД).
type ClientInfo struct {
	IPAddress      string
	UserAgent      string
	DeviceType     string // ENUM auth_device_type; пусто → "UNKNOWN" в репозитории
	DeviceName     string
	OS             string
	OSVersion      string
	Browser        string
	BrowserVersion string
	AppVersion     string
}

type LoginInput struct {
	Email      string
	Password   string
	ClientInfo ClientInfo
}

// LoginEmployee — компактный срез сотрудника для ответа клиенту.
type LoginEmployee struct {
	ID       uuid.UUID
	FullName string
	Email    string
	Role     string
}

type LoginOutput struct {
	Token     string // сырой opaque-токен; возвращаем клиенту один раз
	TokenType string // "Bearer" — для удобства сборки Authorization-заголовка
	ExpiresAt time.Time
	Employee  LoginEmployee
}

// ───────────────────────── Registry ─────────────────────────

type CreateRegistryInput struct {
	Name      string
	Type      string
	URL       string
	Username  string
	Password  string // открытый пароль/токен; сервис шифрует перед записью
	Email     string
	Namespace string
	IsDefault bool
	Insecure  bool
}

// CreateRegistryOutput — без пароля (даже зашифрованного): наружу секрет не отдаём.
// HasCredentials сообщает, были ли сохранены учётные данные.
type CreateRegistryOutput struct {
	ID             uuid.UUID
	Name           string
	Type           string
	URL            string
	Username       string
	Email          string
	Namespace      string
	IsDefault      bool
	IsActive       bool
	Insecure       bool
	HasCredentials bool
	CreatedAt      time.Time
}

// ListRegistriesInput — параметры списка (из query-string). Page с 1.
type ListRegistriesInput struct {
	Page      int
	PageSize  int
	Type      string
	IsActive  *bool
	IsDefault *bool
	Search    string
	SortBy    string
	Order     string // "asc" | "desc"
}

// RegistryItem — элемент списка. Без пароля; HasCredentials = есть ли он.
type RegistryItem struct {
	ID             uuid.UUID
	Name           string
	Type           string
	URL            string
	Username       string
	Email          string
	Namespace      string
	IsDefault      bool
	IsActive       bool
	Insecure       bool
	HasCredentials bool
	LastCheckedAt  *time.Time
	LastStatus     string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type Pagination struct {
	Page       int
	PageSize   int
	Total      int
	TotalPages int
}

type ListRegistriesOutput struct {
	Items      []RegistryItem
	Pagination Pagination
}

// UpdateRegistryInput — полное обновление (PUT). Password — указатель:
//   nil    → оставить текущий пароль
//   ""     → очистить учётные данные
//   "..."  → задать новый (будет зашифрован)
type UpdateRegistryInput struct {
	ID        uuid.UUID
	Name      string
	Type      string
	URL       string
	Username  string
	Password  *string
	Email     string
	Namespace string
	IsDefault bool
	IsActive  bool
	Insecure  bool
}

type UpdateRegistryOutput struct {
	ID             uuid.UUID
	Name           string
	Type           string
	URL            string
	Username       string
	Email          string
	Namespace      string
	IsDefault      bool
	IsActive       bool
	Insecure       bool
	HasCredentials bool
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type DeleteRegistryOutput struct {
	ID        uuid.UUID
	DeletedAt time.Time
}

type ConnectRegistryOutput struct {
	ID            uuid.UUID
	Connected     bool
	Authenticated bool
	Status        string // OK | AUTH_FAILED | UNREACHABLE | TLS_ERROR | ERROR
	Message       string
	APIVersion    string
	IsActive      bool
	CheckedAt     time.Time
}

type PingRegistryOutput struct {
	ID            uuid.UUID
	Connected     bool
	Authenticated bool
	Status        string // OK | AUTH_FAILED | UNREACHABLE | TLS_ERROR | ERROR
	Message       string
	APIVersion    string
	IsActive      bool
	CheckedAt     time.Time
}

// ListRegistryImagesInput — листинг образов сохранённого registry по id.
// Namespace опционален: переопределяет namespace из записи (нужно DockerHub).
type ListRegistryImagesInput struct {
	ID        uuid.UUID
	Namespace string
}

type ListRegistryImagesOutput struct {
	RegistryID uuid.UUID
	Type       string
	Source     string // hub_api | registry_v2
	Namespace  string
	Total      int
	Images     []RegistryImage
}

type RegistryImage struct {
	Name        string
	Tags        []string
	TagCount    int
	Description string
	IsPrivate   *bool
	PullCount   *int64
	StarCount   *int64
	LastUpdated string
}
