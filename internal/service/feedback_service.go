package service

import (
	"context"
	"log/slog"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/YuriyDubinin/digix-api/internal/domain"
)

const notifyTimeout = 10 * time.Second

type FeedbackService struct {
	repo     domain.FeedbackRepository
	notifier domain.FeedbackNotifier
	logger   *slog.Logger
	clock    func() time.Time
}

func NewFeedbackService(repo domain.FeedbackRepository, notifier domain.FeedbackNotifier, logger *slog.Logger) *FeedbackService {
	return &FeedbackService{
		repo:     repo,
		notifier: notifier,
		logger:   logger,
		clock:    time.Now,
	}
}

func (s *FeedbackService) CreateFeedback(ctx context.Context, input CreateFeedbackInput) (*CreateFeedbackOutput, error) {
	in := normalizeCreateInput(input)

	if errs := validateCreateInput(in); len(errs) > 0 {
		return nil, errs
	}

	now := s.clock()
	f := &domain.FeedbackRequest{
		ID:        uuid.New(),
		Name:      in.Name,
		Email:     in.Email,
		Phone:     in.Phone,
		Subject:   in.Subject,
		Message:   in.Message,
		Status:    domain.FeedbackStatusNew,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := s.repo.Create(ctx, f); err != nil {
		return nil, err
	}

	s.logger.Info("feedback created", "feedback_id", f.ID)

	// Уведомление в Telegram уходит асинхронно: недоступность бота не должна
	// ломать создание заявки. Контекст у горутины свой — он переживёт возврат
	// HTTP-ответа клиенту.
	s.notifyAsync(*f)

	return &CreateFeedbackOutput{
		ID:        f.ID,
		Status:    string(f.Status),
		CreatedAt: f.CreatedAt,
	}, nil
}

func (s *FeedbackService) notifyAsync(f domain.FeedbackRequest) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), notifyTimeout)
		defer cancel()
		if err := s.notifier.NotifyNewFeedback(ctx, &f); err != nil {
			s.logger.Error("notify feedback",
				"err", err,
				"feedback_id", f.ID,
			)
			return
		}
		s.logger.Info("feedback notification sent", "feedback_id", f.ID)
	}()
}

func normalizeCreateInput(in CreateFeedbackInput) CreateFeedbackInput {
	return CreateFeedbackInput{
		Name:    strings.TrimSpace(in.Name),
		Email:   strings.ToLower(strings.TrimSpace(in.Email)),
		Phone:   strings.TrimSpace(in.Phone),
		Subject: strings.TrimSpace(in.Subject),
		Message: strings.TrimSpace(in.Message),
	}
}

func validateCreateInput(in CreateFeedbackInput) domain.ValidationErrors {
	var errs domain.ValidationErrors

	switch {
	case in.Name == "":
		errs = append(errs, &domain.ValidationError{Field: "name", Message: "is required"})
	default:
		if l := utf8.RuneCountInString(in.Name); l < 2 || l > 255 {
			errs = append(errs, &domain.ValidationError{Field: "name", Message: "must be between 2 and 255 characters"})
		}
	}

	switch {
	case in.Email == "":
		errs = append(errs, &domain.ValidationError{Field: "email", Message: "is required"})
	default:
		if utf8.RuneCountInString(in.Email) > 255 {
			errs = append(errs, &domain.ValidationError{Field: "email", Message: "must be at most 255 characters"})
		}
		if _, err := mail.ParseAddress(in.Email); err != nil {
			errs = append(errs, &domain.ValidationError{Field: "email", Message: "is not a valid email"})
		}
	}

	if in.Phone != "" && utf8.RuneCountInString(in.Phone) > 50 {
		errs = append(errs, &domain.ValidationError{Field: "phone", Message: "must be at most 50 characters"})
	}

	if in.Subject != "" && utf8.RuneCountInString(in.Subject) > 500 {
		errs = append(errs, &domain.ValidationError{Field: "subject", Message: "must be at most 500 characters"})
	}

	switch {
	case in.Message == "":
		errs = append(errs, &domain.ValidationError{Field: "message", Message: "is required"})
	default:
		if l := utf8.RuneCountInString(in.Message); l < 10 || l > 5000 {
			errs = append(errs, &domain.ValidationError{Field: "message", Message: "must be between 10 and 5000 characters"})
		}
	}

	return errs
}
