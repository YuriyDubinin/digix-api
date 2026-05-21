package service

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/YuriyDubinin/digix-api/internal/domain"
)

type mockRepo struct {
	createFn func(ctx context.Context, f *domain.FeedbackRequest) error
	getFn    func(ctx context.Context, id uuid.UUID) (*domain.FeedbackRequest, error)
}

func (m *mockRepo) Create(ctx context.Context, f *domain.FeedbackRequest) error {
	if m.createFn != nil {
		return m.createFn(ctx, f)
	}
	return nil
}

func (m *mockRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.FeedbackRequest, error) {
	if m.getFn != nil {
		return m.getFn(ctx, id)
	}
	return nil, domain.ErrNotFound
}

type mockNotifier struct {
	notifyFn func(ctx context.Context, f *domain.FeedbackRequest) error
}

func (m *mockNotifier) NotifyNewFeedback(ctx context.Context, f *domain.FeedbackRequest) error {
	if m.notifyFn != nil {
		return m.notifyFn(ctx, f)
	}
	return nil
}

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func newServiceWithClock(repo domain.FeedbackRepository, now time.Time) *FeedbackService {
	return newServiceFull(repo, &mockNotifier{}, now)
}

func newServiceFull(repo domain.FeedbackRepository, notifier domain.FeedbackNotifier, now time.Time) *FeedbackService {
	s := NewFeedbackService(repo, notifier, quietLogger())
	s.clock = func() time.Time { return now }
	return s
}

func validInput() CreateFeedbackInput {
	return CreateFeedbackInput{
		Name:    "John Doe",
		Email:   "john@example.com",
		Phone:   "+1 555 0100",
		Subject: "Hello",
		Message: "Please get back to me about the project.",
	}
}

func TestCreateFeedback_Success(t *testing.T) {
	now := time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC)

	var captured *domain.FeedbackRequest
	repo := &mockRepo{
		createFn: func(_ context.Context, f *domain.FeedbackRequest) error {
			captured = f
			return nil
		},
	}
	svc := newServiceWithClock(repo, now)

	in := CreateFeedbackInput{
		Name:    "  John Doe  ",
		Email:   "  John@Example.COM ",
		Phone:   "  +1 555 0100  ",
		Subject: "  Hello  ",
		Message: "  Please get back to me about the project.  ",
	}

	out, err := svc.CreateFeedback(context.Background(), in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out == nil {
		t.Fatal("expected output, got nil")
	}
	if out.ID == uuid.Nil {
		t.Error("expected non-zero ID")
	}
	if out.Status != string(domain.FeedbackStatusNew) {
		t.Errorf("status = %q, want %q", out.Status, domain.FeedbackStatusNew)
	}
	if !out.CreatedAt.Equal(now) {
		t.Errorf("CreatedAt = %v, want %v", out.CreatedAt, now)
	}

	if captured == nil {
		t.Fatal("repo.Create was not called")
	}
	if captured.Name != "John Doe" {
		t.Errorf("Name = %q, want trimmed", captured.Name)
	}
	if captured.Email != "john@example.com" {
		t.Errorf("Email = %q, want trimmed + lowercased", captured.Email)
	}
	if captured.Phone != "+1 555 0100" {
		t.Errorf("Phone = %q, want trimmed", captured.Phone)
	}
	if captured.Subject != "Hello" {
		t.Errorf("Subject = %q, want trimmed", captured.Subject)
	}
	if captured.Message != "Please get back to me about the project." {
		t.Errorf("Message = %q, want trimmed", captured.Message)
	}
	if captured.Status != domain.FeedbackStatusNew {
		t.Errorf("Status = %q, want %q", captured.Status, domain.FeedbackStatusNew)
	}
	if !captured.CreatedAt.Equal(now) || !captured.UpdatedAt.Equal(now) {
		t.Errorf("timestamps not set from clock: created=%v updated=%v", captured.CreatedAt, captured.UpdatedAt)
	}
	if captured.ID != out.ID {
		t.Errorf("captured.ID %v != out.ID %v", captured.ID, out.ID)
	}
}

func TestCreateFeedback_Validation(t *testing.T) {
	now := time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name       string
		mutate     func(*CreateFeedbackInput)
		wantFields []string
	}{
		{
			name:       "empty name",
			mutate:     func(in *CreateFeedbackInput) { in.Name = "   " },
			wantFields: []string{"name"},
		},
		{
			name:       "invalid email",
			mutate:     func(in *CreateFeedbackInput) { in.Email = "not-an-email" },
			wantFields: []string{"email"},
		},
		{
			name:       "message too short",
			mutate:     func(in *CreateFeedbackInput) { in.Message = "short" },
			wantFields: []string{"message"},
		},
		{
			name:       "message too long",
			mutate:     func(in *CreateFeedbackInput) { in.Message = strings.Repeat("a", 5001) },
			wantFields: []string{"message"},
		},
		{
			name: "multiple errors",
			mutate: func(in *CreateFeedbackInput) {
				in.Name = ""
				in.Email = "bad"
				in.Message = ""
			},
			wantFields: []string{"name", "email", "message"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo := &mockRepo{
				createFn: func(_ context.Context, _ *domain.FeedbackRequest) error {
					t.Error("repo.Create should not be called on validation error")
					return nil
				},
			}
			svc := newServiceWithClock(repo, now)

			in := validInput()
			tc.mutate(&in)

			_, err := svc.CreateFeedback(context.Background(), in)
			if err == nil {
				t.Fatal("expected validation error, got nil")
			}

			var verr domain.ValidationErrors
			if !errors.As(err, &verr) {
				t.Fatalf("expected domain.ValidationErrors, got %T: %v", err, err)
			}
			if len(verr) != len(tc.wantFields) {
				t.Errorf("got %d errors, want %d (%v)", len(verr), len(tc.wantFields), tc.wantFields)
			}

			gotFields := make(map[string]struct{}, len(verr))
			for _, ve := range verr {
				gotFields[ve.Field] = struct{}{}
			}
			for _, f := range tc.wantFields {
				if _, ok := gotFields[f]; !ok {
					t.Errorf("missing error for field %q, got: %v", f, verr)
				}
			}
		})
	}
}

func TestCreateFeedback_RepoErrorPropagates(t *testing.T) {
	repoErr := errors.New("boom")
	repo := &mockRepo{
		createFn: func(_ context.Context, _ *domain.FeedbackRequest) error {
			return repoErr
		},
	}
	notifier := &mockNotifier{
		notifyFn: func(_ context.Context, _ *domain.FeedbackRequest) error {
			t.Error("notifier should not be called when repo fails")
			return nil
		},
	}
	svc := newServiceFull(repo, notifier, time.Now())

	_, err := svc.CreateFeedback(context.Background(), validInput())
	if !errors.Is(err, repoErr) {
		t.Fatalf("expected repo error, got %v", err)
	}
}

func TestCreateFeedback_NotifierCalledOnSuccess(t *testing.T) {
	notified := make(chan *domain.FeedbackRequest, 1)
	notifier := &mockNotifier{
		notifyFn: func(_ context.Context, f *domain.FeedbackRequest) error {
			notified <- f
			return nil
		},
	}
	repo := &mockRepo{}
	svc := newServiceFull(repo, notifier, time.Now())

	out, err := svc.CreateFeedback(context.Background(), validInput())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	select {
	case f := <-notified:
		if f.ID != out.ID {
			t.Errorf("notifier got ID %v, want %v", f.ID, out.ID)
		}
		if f.Email != "john@example.com" {
			t.Errorf("notifier got Email %q, want trimmed/lowered", f.Email)
		}
	case <-time.After(time.Second):
		t.Fatal("notifier was not called within 1s")
	}
}

func TestCreateFeedback_NotifierErrorDoesNotFail(t *testing.T) {
	notifyErr := errors.New("telegram unavailable")
	notified := make(chan struct{}, 1)
	notifier := &mockNotifier{
		notifyFn: func(_ context.Context, _ *domain.FeedbackRequest) error {
			notified <- struct{}{}
			return notifyErr
		},
	}
	repo := &mockRepo{}
	svc := newServiceFull(repo, notifier, time.Now())

	out, err := svc.CreateFeedback(context.Background(), validInput())
	if err != nil {
		t.Fatalf("expected success even when notifier fails, got %v", err)
	}
	if out == nil || out.ID == uuid.Nil {
		t.Fatal("expected non-nil output with valid ID")
	}

	select {
	case <-notified:
		// нотификатор был вызван — ок
	case <-time.After(time.Second):
		t.Fatal("notifier was not called within 1s")
	}
}
