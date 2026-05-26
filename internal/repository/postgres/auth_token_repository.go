package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/YuriyDubinin/dijex-api/internal/domain"
)

type AuthTokenRepository struct {
	pool *pgxpool.Pool
}

func NewAuthTokenRepository(pool *pgxpool.Pool) *AuthTokenRepository {
	return &AuthTokenRepository{pool: pool}
}

// Create вставляет токен в auth_tokens. Все опциональные текстовые поля
// (ip_address, user_agent, device_*, os/browser, страна/город) при пустой
// строке преобразуются в NULL через NULLIF — иначе INET-каст на пустой
// строке упадёт.
func (r *AuthTokenRepository) Create(ctx context.Context, t *domain.AuthToken) error {
	const query = `
		INSERT INTO auth_tokens (
			id, employee_id, token_hash, token_type,
			issued_at, expires_at,
			ip_address, user_agent,
			device_type, device_name,
			os, os_version, browser, browser_version, app_version,
			country_code, city
		) VALUES (
			$1, $2, $3, $4::auth_token_type,
			$5, $6,
			NULLIF($7, '')::inet,
			NULLIF($8, ''),
			COALESCE(NULLIF($9, ''), 'UNKNOWN')::auth_device_type,
			NULLIF($10, ''),
			NULLIF($11, ''), NULLIF($12, ''), NULLIF($13, ''), NULLIF($14, ''), NULLIF($15, ''),
			NULLIF($16, ''), NULLIF($17, '')
		)
	`

	_, err := r.pool.Exec(ctx, query,
		t.ID,
		t.EmployeeID,
		t.TokenHash,
		t.TokenType,
		t.IssuedAt,
		t.ExpiresAt,
		t.IPAddress,
		t.UserAgent,
		t.DeviceType,
		t.DeviceName,
		t.OS,
		t.OSVersion,
		t.Browser,
		t.BrowserVersion,
		t.AppVersion,
		t.CountryCode,
		t.City,
	)
	if err != nil {
		return fmt.Errorf("postgres: create auth token: %w", err)
	}
	return nil
}

// FindActiveByHash подтягивает токен и актуальные role/status владельца
// одним SQL-запросом. Логические проверки (истёк/отозван/сотрудник DISABLED)
// делает service-слой — здесь только чтение из БД.
func (r *AuthTokenRepository) FindActiveByHash(ctx context.Context, hash string) (*domain.AuthTokenWithEmployee, error) {
	const query = `
		SELECT
			t.id, t.employee_id, t.token_hash, t.token_type::text,
			t.issued_at, t.expires_at, t.last_used_at, t.revoked_at,
			COALESCE(t.revoked_reason, ''),
			e.role::text, e.status::text
		FROM auth_tokens t
		JOIN employees e ON e.id = t.employee_id
		WHERE t.token_hash = $1
	`

	var (
		res       domain.AuthTokenWithEmployee
		tokenType string
		role      string
		status    string
	)
	err := r.pool.QueryRow(ctx, query, hash).Scan(
		&res.Token.ID,
		&res.Token.EmployeeID,
		&res.Token.TokenHash,
		&tokenType,
		&res.Token.IssuedAt,
		&res.Token.ExpiresAt,
		&res.Token.LastUsedAt,
		&res.Token.RevokedAt,
		&res.Token.RevokedReason,
		&role,
		&status,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("postgres: find auth token by hash: %w", domain.ErrNotFound)
		}
		return nil, fmt.Errorf("postgres: find auth token by hash: %w", err)
	}

	res.Token.TokenType = tokenType
	res.EmployeeRole = role
	res.EmployeeStatus = status
	return &res, nil
}

// TouchLastUsed обновляет last_used_at для токена. Вызывается асинхронно
// из service-слоя, поэтому коротким контекстом и без возврата count'а.
func (r *AuthTokenRepository) TouchLastUsed(ctx context.Context, id uuid.UUID, now time.Time) error {
	const query = `UPDATE auth_tokens SET last_used_at = $1 WHERE id = $2`
	if _, err := r.pool.Exec(ctx, query, now, id); err != nil {
		return fmt.Errorf("postgres: touch auth token last_used_at: %w", err)
	}
	return nil
}

// Revoke помечает токен отозванным. Условие `revoked_at IS NULL` защищает
// от затирания истории: если токен уже был отозван (например, триггером
// revoke_tokens_on_employee_disable), повторный вызов ничего не меняет.
func (r *AuthTokenRepository) Revoke(ctx context.Context, id uuid.UUID, reason string, now time.Time) error {
	const query = `
		UPDATE auth_tokens
		SET revoked_at = $1,
		    revoked_reason = $2
		WHERE id = $3
		  AND revoked_at IS NULL
	`
	if _, err := r.pool.Exec(ctx, query, now, reason, id); err != nil {
		return fmt.Errorf("postgres: revoke auth token: %w", err)
	}
	return nil
}
