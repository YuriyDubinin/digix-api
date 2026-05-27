package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/YuriyDubinin/dijex-api/internal/domain"
)

const uniqueViolationCode = "23505"

type RegistryRepository struct {
	pool *pgxpool.Pool
}

func NewRegistryRepository(pool *pgxpool.Pool) *RegistryRepository {
	return &RegistryRepository{pool: pool}
}

// Create вставляет registry в транзакции. Если новая запись помечена как
// default — сначала снимаем флаг со всех живых записей, чтобы не нарушить
// частичный UNIQUE-индекс idx_registries_single_default.
func (r *RegistryRepository) Create(ctx context.Context, reg *domain.Registry) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("postgres: begin tx: %w", err)
	}
	defer tx.Rollback(ctx) // no-op после успешного Commit

	if reg.IsDefault {
		const clearDefault = `
			UPDATE registries SET is_default = FALSE
			WHERE is_default = TRUE AND deleted_at IS NULL
		`
		if _, err := tx.Exec(ctx, clearDefault); err != nil {
			return fmt.Errorf("postgres: clear default registry: %w", err)
		}
	}

	const insert = `
		INSERT INTO registries
			(id, name, type, url, username, password_encrypted, email, namespace,
			 is_default, is_active, insecure, created_at, updated_at)
		VALUES
			($1, $2, $3::registry_type, $4,
			 NULLIF($5, ''), NULLIF($6, ''), NULLIF($7, ''), NULLIF($8, ''),
			 $9, $10, $11, $12, $13)
	`
	_, err = tx.Exec(ctx, insert,
		reg.ID,
		reg.Name,
		reg.Type,
		reg.URL,
		reg.Username,
		reg.PasswordEncrypted,
		reg.Email,
		reg.Namespace,
		reg.IsDefault,
		reg.IsActive,
		reg.Insecure,
		reg.CreatedAt,
		reg.UpdatedAt,
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == uniqueViolationCode {
			return fmt.Errorf("postgres: create registry: %w", domain.ErrAlreadyExists)
		}
		return fmt.Errorf("postgres: create registry: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("postgres: commit tx: %w", err)
	}
	return nil
}

// SoftDelete помечает registry удалённым. Условие deleted_at IS NULL делает
// операцию идемпотентной: повторное удаление вернёт ErrNotFound (0 строк).
// Снимаем is_default, чтобы удалённая запись не занимала слот «по умолчанию».
func (r *RegistryRepository) SoftDelete(ctx context.Context, id uuid.UUID) (time.Time, error) {
	const query = `
		UPDATE registries
		SET deleted_at = NOW(), is_default = FALSE
		WHERE id = $1 AND deleted_at IS NULL
		RETURNING deleted_at
	`
	var deletedAt time.Time
	if err := r.pool.QueryRow(ctx, query, id).Scan(&deletedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return time.Time{}, fmt.Errorf("postgres: soft delete registry %s: %w", id, domain.ErrNotFound)
		}
		return time.Time{}, fmt.Errorf("postgres: soft delete registry %s: %w", id, err)
	}
	return deletedAt, nil
}

// UpdateConnectionStatus сохраняет итог проверки подключения. Не возвращает
// ErrNotFound: это best-effort обновление (вызывающий уже проверил существование).
//
// Если activate=true — включает registry (is_active=TRUE). При activate=false
// поле is_active не трогается (CASE), чтобы неудачная проверка не выключала
// уже активное подключение.
func (r *RegistryRepository) UpdateConnectionStatus(ctx context.Context, id uuid.UUID, status, errMsg string, checkedAt time.Time, setActive *bool) error {
	const query = `
		UPDATE registries
		SET last_checked_at = $1,
		    last_status     = $2,
		    last_error      = NULLIF($3, ''),
		    is_active       = COALESCE($5, is_active)
		WHERE id = $4 AND deleted_at IS NULL
	`
	if _, err := r.pool.Exec(ctx, query, checkedAt, status, errMsg, id, setActive); err != nil {
		return fmt.Errorf("postgres: update registry connection status %s: %w", id, err)
	}
	return nil
}

// registrySortColumns — whitelist полей сортировки (защита от SQL-инъекции:
// имя колонки нельзя параметризовать через $N, поэтому подставляем только
// проверенные значения).
var registrySortColumns = map[string]string{
	"created_at": "created_at",
	"updated_at": "updated_at",
	"name":       "name",
	"type":       "type",
}

func registrySortColumn(s string) string {
	if col, ok := registrySortColumns[s]; ok {
		return col
	}
	return "created_at"
}

// List собирает динамический WHERE из фильтра, считает общее число записей
// и возвращает страницу. Удалённые записи всегда исключены.
func (r *RegistryRepository) List(ctx context.Context, f domain.RegistryListFilter) ([]*domain.Registry, int, error) {
	conds := []string{"deleted_at IS NULL"}
	args := []any{}

	if f.Type != "" {
		args = append(args, f.Type)
		conds = append(conds, fmt.Sprintf("type = $%d::registry_type", len(args)))
	}
	if f.IsActive != nil {
		args = append(args, *f.IsActive)
		conds = append(conds, fmt.Sprintf("is_active = $%d", len(args)))
	}
	if f.IsDefault != nil {
		args = append(args, *f.IsDefault)
		conds = append(conds, fmt.Sprintf("is_default = $%d", len(args)))
	}
	if f.Search != "" {
		args = append(args, "%"+f.Search+"%")
		conds = append(conds, fmt.Sprintf("name ILIKE $%d", len(args)))
	}
	where := "WHERE " + strings.Join(conds, " AND ")

	// 1) Общее количество подходящих записей.
	var total int
	if err := r.pool.QueryRow(ctx, "SELECT COUNT(*) FROM registries "+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("postgres: count registries: %w", err)
	}
	if total == 0 {
		return []*domain.Registry{}, 0, nil
	}

	// 2) Страница. ORDER BY со вторичным id для стабильности.
	dir := "ASC"
	if f.SortDesc {
		dir = "DESC"
	}
	args = append(args, f.Limit)
	limitPos := len(args)
	args = append(args, f.Offset)
	offsetPos := len(args)

	query := fmt.Sprintf(`
		SELECT
			id, name, type::text, url,
			COALESCE(username, ''), COALESCE(password_encrypted, ''),
			COALESCE(email, ''), COALESCE(namespace, ''),
			is_default, is_active, insecure,
			last_checked_at, COALESCE(last_status, ''), COALESCE(last_error, ''),
			created_at, updated_at
		FROM registries %s
		ORDER BY %s %s, id
		LIMIT $%d OFFSET $%d
	`, where, registrySortColumn(f.SortBy), dir, limitPos, offsetPos)

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("postgres: list registries: %w", err)
	}
	defer rows.Close()

	var items []*domain.Registry
	for rows.Next() {
		var reg domain.Registry
		if err := rows.Scan(
			&reg.ID,
			&reg.Name,
			&reg.Type,
			&reg.URL,
			&reg.Username,
			&reg.PasswordEncrypted,
			&reg.Email,
			&reg.Namespace,
			&reg.IsDefault,
			&reg.IsActive,
			&reg.Insecure,
			&reg.LastCheckedAt,
			&reg.LastStatus,
			&reg.LastError,
			&reg.CreatedAt,
			&reg.UpdatedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("postgres: scan registry: %w", err)
		}
		items = append(items, &reg)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("postgres: iterate registries: %w", err)
	}

	return items, total, nil
}

const registrySelectColumns = `
	id, name, type::text, url,
	COALESCE(username, ''), COALESCE(password_encrypted, ''),
	COALESCE(email, ''), COALESCE(namespace, ''),
	is_default, is_active, insecure,
	last_checked_at, COALESCE(last_status, ''), COALESCE(last_error, ''),
	created_at, updated_at
`

func scanRegistry(row pgx.Row) (*domain.Registry, error) {
	var reg domain.Registry
	err := row.Scan(
		&reg.ID, &reg.Name, &reg.Type, &reg.URL,
		&reg.Username, &reg.PasswordEncrypted,
		&reg.Email, &reg.Namespace,
		&reg.IsDefault, &reg.IsActive, &reg.Insecure,
		&reg.LastCheckedAt, &reg.LastStatus, &reg.LastError,
		&reg.CreatedAt, &reg.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &reg, nil
}

// GetByID возвращает живой registry. ErrNotFound, если не найден / удалён.
func (r *RegistryRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.Registry, error) {
	query := "SELECT " + registrySelectColumns + " FROM registries WHERE id = $1 AND deleted_at IS NULL"
	reg, err := scanRegistry(r.pool.QueryRow(ctx, query, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("postgres: get registry %s: %w", id, domain.ErrNotFound)
		}
		return nil, fmt.Errorf("postgres: get registry %s: %w", id, err)
	}
	return reg, nil
}

// Update обновляет registry в транзакции. updated_at проставляется триггером;
// возвращается через RETURNING вместе с created_at.
func (r *RegistryRepository) Update(ctx context.Context, reg *domain.Registry) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("postgres: begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	if reg.IsDefault {
		const clearDefault = `
			UPDATE registries SET is_default = FALSE
			WHERE is_default = TRUE AND deleted_at IS NULL AND id <> $1
		`
		if _, err := tx.Exec(ctx, clearDefault, reg.ID); err != nil {
			return fmt.Errorf("postgres: clear default registry: %w", err)
		}
	}

	const update = `
		UPDATE registries SET
			name               = $1,
			type               = $2::registry_type,
			url                = $3,
			username           = NULLIF($4, ''),
			password_encrypted = NULLIF($5, ''),
			email              = NULLIF($6, ''),
			namespace          = NULLIF($7, ''),
			is_default         = $8,
			is_active          = $9,
			insecure           = $10
		WHERE id = $11 AND deleted_at IS NULL
		RETURNING created_at, updated_at
	`
	err = tx.QueryRow(ctx, update,
		reg.Name,
		reg.Type,
		reg.URL,
		reg.Username,
		reg.PasswordEncrypted,
		reg.Email,
		reg.Namespace,
		reg.IsDefault,
		reg.IsActive,
		reg.Insecure,
		reg.ID,
	).Scan(&reg.CreatedAt, &reg.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("postgres: update registry %s: %w", reg.ID, domain.ErrNotFound)
		}
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == uniqueViolationCode {
			return fmt.Errorf("postgres: update registry: %w", domain.ErrAlreadyExists)
		}
		return fmt.Errorf("postgres: update registry: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("postgres: commit tx: %w", err)
	}
	return nil
}
