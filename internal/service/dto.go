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
