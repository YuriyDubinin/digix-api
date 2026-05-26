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
