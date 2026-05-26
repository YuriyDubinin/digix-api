package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/YuriyDubinin/dijex-api/internal/domain"
)

type EmployeeRepository struct {
	pool *pgxpool.Pool
}

func NewEmployeeRepository(pool *pgxpool.Pool) *EmployeeRepository {
	return &EmployeeRepository{pool: pool}
}

// FindByEmail возвращает сотрудника по email. Запрос ищет точное совпадение —
// email нормализуется (trim/lowercase) на стороне сервиса перед вызовом.
// Если сотрудника нет — возвращает domain.ErrNotFound (через wrap).
func (r *EmployeeRepository) FindByEmail(ctx context.Context, email string) (*domain.Employee, error) {
	const query = `
		SELECT
			id, full_name, email,
			COALESCE(phone, ''),
			role::text, status::text,
			password_hash,
			created_at, updated_at, deleted_at
		FROM employees
		WHERE email = $1
	`

	var (
		e      domain.Employee
		role   string
		status string
	)
	err := r.pool.QueryRow(ctx, query, email).Scan(
		&e.ID,
		&e.FullName,
		&e.Email,
		&e.Phone,
		&role,
		&status,
		&e.PasswordHash,
		&e.CreatedAt,
		&e.UpdatedAt,
		&e.DeletedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("postgres: find employee by email: %w", domain.ErrNotFound)
		}
		return nil, fmt.Errorf("postgres: find employee by email: %w", err)
	}
	e.Role = role
	e.Status = status
	return &e, nil
}
