package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/YuriyDubinin/digix-api/internal/domain"
	"github.com/YuriyDubinin/digix-api/internal/service"
	"github.com/YuriyDubinin/digix-api/internal/transport/http/dto"
	mw "github.com/YuriyDubinin/digix-api/internal/transport/http/middleware"
	"github.com/YuriyDubinin/digix-api/internal/transport/http/response"
	"github.com/YuriyDubinin/digix-api/pkg/validator"
)

const maxRequestBody = 1 << 20 // 1 MiB

type FeedbackService interface {
	CreateFeedback(ctx context.Context, input service.CreateFeedbackInput) (*service.CreateFeedbackOutput, error)
}

type FeedbackHandler struct {
	service   FeedbackService
	validator *validator.Validator
	logger    *slog.Logger
}

func NewFeedbackHandler(svc FeedbackService, v *validator.Validator, logger *slog.Logger) *FeedbackHandler {
	return &FeedbackHandler{service: svc, validator: v, logger: logger}
}

func (h *FeedbackHandler) CreateRequest(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)

	var req dto.CreateFeedbackHTTPRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, "INVALID_JSON", "invalid JSON body")
		return
	}

	if err := h.validator.Struct(req); err != nil {
		details := toResponseFieldErrors(validator.TranslateErrors(err))
		response.WriteError(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "request validation failed", details...)
		return
	}

	out, err := h.service.CreateFeedback(r.Context(), req.ToServiceInput())
	if err != nil {
		var verr domain.ValidationErrors
		if errors.As(err, &verr) {
			response.WriteError(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "domain validation failed", domainValidationDetails(verr)...)
			return
		}
		h.logger.Error("create feedback",
			"err", err,
			"request_id", mw.RequestIDFromContext(r.Context()),
		)
		response.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error")
		return
	}

	response.WriteJSON(w, http.StatusCreated, dto.FromServiceOutput(out))
}

func toResponseFieldErrors(in []validator.FieldError) []response.FieldError {
	out := make([]response.FieldError, 0, len(in))
	for _, fe := range in {
		out = append(out, response.FieldError{Field: fe.Field, Message: fe.Message})
	}
	return out
}

func domainValidationDetails(verrs domain.ValidationErrors) []response.FieldError {
	out := make([]response.FieldError, 0, len(verrs))
	for _, ve := range verrs {
		out = append(out, response.FieldError{Field: ve.Field, Message: ve.Message})
	}
	return out
}
