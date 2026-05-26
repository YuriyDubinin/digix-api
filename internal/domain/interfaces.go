package domain

import (
	"context"
	"time"

	"github.com/google/uuid"
)

type FeedbackRepository interface {
	Create(ctx context.Context, f *FeedbackRequest) error
	GetByID(ctx context.Context, id uuid.UUID) (*FeedbackRequest, error)
}

type FeedbackNotifier interface {
	NotifyNewFeedback(ctx context.Context, f *FeedbackRequest) error
}

// TokenRepository — контракт хранения и поиска авторизационных токенов.
type TokenRepository interface {
	// Create вставляет новый токен в auth_tokens. Все поля устройства/сети
	// опциональны: пустые строки сохраняются как NULL.
	Create(ctx context.Context, t *AuthToken) error

	// FindActiveByHash находит токен по его SHA-256 хэшу и возвращает его
	// вместе с актуальным role/status сотрудника (одним SQL-запросом через JOIN).
	// Если токена нет — возвращает ErrNotFound. Проверка истечения/отзыва
	// выполняется в service-слое, чтобы различать причины отказа.
	FindActiveByHash(ctx context.Context, hash string) (*AuthTokenWithEmployee, error)

	// TouchLastUsed обновляет last_used_at = now для токена.
	// Best-effort — допускается ошибка, она не должна ломать запрос.
	TouchLastUsed(ctx context.Context, id uuid.UUID, now time.Time) error
}

// EmployeeRepository — контракт чтения сотрудников.
// Записывает сотрудников пока никто (создаём вручную в БД), поэтому
// только методы чтения.
type EmployeeRepository interface {
	// FindByEmail находит сотрудника по email. Email сравнивается как есть —
	// нормализация (trim/lowercase) выполняется в service-слое.
	// Возвращает ErrNotFound, если сотрудник не найден.
	FindByEmail(ctx context.Context, email string) (*Employee, error)
}
