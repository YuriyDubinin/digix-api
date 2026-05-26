package domain

import (
	"time"

	"github.com/google/uuid"
)

type FeedbackStatus string

const (
	FeedbackStatusNew        FeedbackStatus = "NEW"
	FeedbackStatusProcessing FeedbackStatus = "PROCESSING"
	FeedbackStatusClosed     FeedbackStatus = "CLOSED"
)

func (s FeedbackStatus) IsValid() bool {
	switch s {
	case FeedbackStatusNew, FeedbackStatusProcessing, FeedbackStatusClosed:
		return true
	default:
		return false
	}
}

type FeedbackRequest struct {
	ID        uuid.UUID
	Name      string
	Email     string
	Phone     string
	Subject   string
	Message   string
	Status    FeedbackStatus
	CreatedAt time.Time
	UpdatedAt time.Time
}
