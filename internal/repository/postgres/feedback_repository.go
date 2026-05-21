package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/YuriyDubinin/digix-api/internal/domain"
)

type FeedbackRepository struct {
	pool *pgxpool.Pool
}

func NewFeedbackRepository(pool *pgxpool.Pool) *FeedbackRepository {
	return &FeedbackRepository{pool: pool}
}

func (r *FeedbackRepository) Create(ctx context.Context, f *domain.FeedbackRequest) error {
	const query = `
		INSERT INTO feedback_requests
			(id, name, email, phone, subject, message, status, created_at, updated_at)
		VALUES
			($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`

	_, err := r.pool.Exec(ctx, query,
		f.ID,
		f.Name,
		f.Email,
		f.Phone,
		f.Subject,
		f.Message,
		f.Status,
		f.CreatedAt,
		f.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("postgres: create feedback request: %w", err)
	}
	return nil
}

func (r *FeedbackRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.FeedbackRequest, error) {
	const query = `
		SELECT id, name, email,
		       COALESCE(phone, ''), COALESCE(subject, ''),
		       message, status, created_at, updated_at
		FROM feedback_requests
		WHERE id = $1
	`

	var f domain.FeedbackRequest
	err := r.pool.QueryRow(ctx, query, id).Scan(
		&f.ID,
		&f.Name,
		&f.Email,
		&f.Phone,
		&f.Subject,
		&f.Message,
		&f.Status,
		&f.CreatedAt,
		&f.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("postgres: get feedback request %s: %w", id, domain.ErrNotFound)
		}
		return nil, fmt.Errorf("postgres: get feedback request %s: %w", id, err)
	}
	return &f, nil
}
