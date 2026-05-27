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

	// Revoke помечает токен отозванным: ставит revoked_at = now и revoked_reason.
	// Не трогает уже отозванные токены (WHERE revoked_at IS NULL),
	// чтобы не перетереть исходную причину/время отзыва.
	Revoke(ctx context.Context, id uuid.UUID, reason string, now time.Time) error
}

// ServerRepository — контракт хранения подключений к серверам.
type ServerRepository interface {
	// Create вставляет сервер. Конфликт уникального имени → ErrAlreadyExists.
	Create(ctx context.Context, s *Server) error

	// List возвращает срез серверов по фильтру + общее число подходящих
	// записей. Удалённые (soft-delete) не включаются.
	List(ctx context.Context, filter ServerListFilter) ([]*Server, int, error)

	// GetByID находит живой (не удалённый) сервер. ErrNotFound, если нет.
	GetByID(ctx context.Context, id uuid.UUID) (*Server, error)

	// Update обновляет сервер по ID. ErrNotFound, если записи нет;
	// ErrAlreadyExists при конфликте имени. CreatedAt/UpdatedAt результата
	// заполняются актуальными значениями из БД.
	Update(ctx context.Context, s *Server) error

	// SoftDelete помечает сервер удалённым (deleted_at = NOW). ErrNotFound,
	// если записи нет или она уже удалена. Возвращает момент удаления.
	SoftDelete(ctx context.Context, id uuid.UUID) (time.Time, error)

	// UpdateConnectionStatus пишет результат проверки подключения:
	// last_checked_at, last_status, last_error. setActive: nil — не трогать
	// is_active; true/false — установить.
	UpdateConnectionStatus(ctx context.Context, id uuid.UUID, status, errMsg string, checkedAt time.Time, setActive *bool) error

	// UpdateFacts сохраняет собранные при подключении факты о сервере.
	UpdateFacts(ctx context.Context, id uuid.UUID, f ServerFacts) error
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

// RegistryRepository — контракт хранения подключений к Docker registry.
type RegistryRepository interface {
	// Create вставляет registry. При IsDefault=true атомарно снимает флаг
	// default с прочих записей. Конфликт уникального имени → ErrAlreadyExists.
	Create(ctx context.Context, r *Registry) error

	// List возвращает срез registry по фильтру + общее число подходящих
	// записей (для пагинации). Удалённые (soft-delete) не включаются.
	List(ctx context.Context, filter RegistryListFilter) ([]*Registry, int, error)

	// GetByID находит живой (не удалённый) registry. ErrNotFound, если нет.
	GetByID(ctx context.Context, id uuid.UUID) (*Registry, error)

	// Update обновляет registry по ID. При IsDefault=true снимает флаг default
	// с прочих. ErrNotFound, если записи нет; ErrAlreadyExists при конфликте имени.
	// Поля CreatedAt/UpdatedAt результата заполняются актуальными значениями из БД.
	Update(ctx context.Context, r *Registry) error

	// SoftDelete помечает registry удалённым (ставит deleted_at = NOW и снимает
	// is_default). Физически строку не удаляет. Возвращает момент удаления.
	// ErrNotFound, если записи нет или она уже удалена.
	SoftDelete(ctx context.Context, id uuid.UUID) (time.Time, error)

	// UpdateConnectionStatus записывает результат проверки подключения:
	// last_checked_at, last_status и last_error (пустая строка → NULL).
	// setActive управляет полем is_active:
	//   nil   — не трогать (например, неуспешный connect не выключает запись);
	//   true  — включить;
	//   false — выключить (например, неуспешный ping).
	UpdateConnectionStatus(ctx context.Context, id uuid.UUID, status, errMsg string, checkedAt time.Time, setActive *bool) error
}
