package domain

import (
	"context"

	"github.com/google/uuid"
)

type FeedbackRepository interface {
	Create(ctx context.Context, f *FeedbackRequest) error
	GetByID(ctx context.Context, id uuid.UUID) (*FeedbackRequest, error)
}

type FeedbackNotifier interface {
	NotifyNewFeedback(ctx context.Context, f *FeedbackRequest) error
}
