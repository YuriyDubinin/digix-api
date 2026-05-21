package dto

import (
	"time"

	"github.com/google/uuid"

	"github.com/YuriyDubinin/digix-api/internal/service"
)

type CreateFeedbackHTTPRequest struct {
	Name    string `json:"name"    validate:"required,min=2,max=255"`
	Email   string `json:"email"   validate:"required,email,max=255"`
	Phone   string `json:"phone"   validate:"omitempty,max=50"`
	Subject string `json:"subject" validate:"omitempty,max=500"`
	Message string `json:"message" validate:"required,min=10,max=5000"`
}

type CreateFeedbackHTTPResponse struct {
	ID        uuid.UUID `json:"id"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

func (r CreateFeedbackHTTPRequest) ToServiceInput() service.CreateFeedbackInput {
	return service.CreateFeedbackInput{
		Name:    r.Name,
		Email:   r.Email,
		Phone:   r.Phone,
		Subject: r.Subject,
		Message: r.Message,
	}
}

func FromServiceOutput(o *service.CreateFeedbackOutput) CreateFeedbackHTTPResponse {
	return CreateFeedbackHTTPResponse{
		ID:        o.ID,
		Status:    o.Status,
		CreatedAt: o.CreatedAt,
	}
}
