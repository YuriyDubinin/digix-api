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
	// FindActiveByHash находит токен по его SHA-256 хэшу и возвращает его
	// вместе с актуальным role/status сотрудника (одним SQL-запросом через JOIN).
	// Если токена нет — возвращает ErrNotFound. Проверка истечения/отзыва
	// выполняется в service-слое, чтобы различать причины отказа.
	FindActiveByHash(ctx context.Context, hash string) (*AuthTokenWithEmployee, error)

	// TouchLastUsed обновляет last_used_at = now для токена.
	// Best-effort — допускается ошибка, она не должна ломать запрос.
	TouchLastUsed(ctx context.Context, id uuid.UUID, now time.Time) error
}
