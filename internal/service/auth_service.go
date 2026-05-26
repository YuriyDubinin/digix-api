package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/YuriyDubinin/dijex-api/internal/domain"
	"github.com/YuriyDubinin/dijex-api/pkg/crypto"
)

const touchLastUsedTimeout = 2 * time.Second

// dummyPasswordPlaceholder — произвольная строка, по которой при инициализации
// сервиса считаем bcrypt-хэш. Используется как заглушка для constant-time
// сравнения, когда сотрудник не найден (защита от user enumeration по таймингу).
const dummyPasswordPlaceholder = "dummy-password-for-constant-time-bcrypt-comparison"

type AuthService struct {
	tokens    domain.TokenRepository
	employees domain.EmployeeRepository
	passwords *crypto.PasswordHasher
	tokenTTL  time.Duration
	logger    *slog.Logger
	clock     func() time.Time

	// dummyPasswordHash — заранее посчитанный bcrypt-хэш, чтобы Verify
	// в ветке «email не найден» работал столько же, сколько настоящая проверка.
	dummyPasswordHash string
}

// NewAuthService собирает сервис аутентификации.
//
// passwords — обязателен для Login. Authenticate (Bearer-токены) его не
// использует, но в одном сервисе оба метода живут потому что концептуально
// относятся к одному use-case'у «auth».
func NewAuthService(
	tokens domain.TokenRepository,
	employees domain.EmployeeRepository,
	passwords *crypto.PasswordHasher,
	tokenTTL time.Duration,
	logger *slog.Logger,
) (*AuthService, error) {
	// Считаем dummy-хэш один раз при инициализации — иначе пришлось бы
	// делать bcrypt-вычисление на каждый failed login, что заметно медленнее.
	dummyHash, err := passwords.Hash(dummyPasswordPlaceholder)
	if err != nil {
		return nil, fmt.Errorf("auth: precompute dummy password hash: %w", err)
	}
	return &AuthService{
		tokens:            tokens,
		employees:         employees,
		passwords:         passwords,
		tokenTTL:          tokenTTL,
		logger:            logger,
		clock:             time.Now,
		dummyPasswordHash: dummyHash,
	}, nil
}

// Authenticate проверяет сырой токен из заголовка Authorization.
// На входе — токен в открытом виде; в БД хранится его SHA-256 хэш, поэтому
// здесь хэшируем и ищем по хэшу. На выходе — Principal либо одна из
// доменных ошибок (ErrTokenInvalid / Expired / Revoked / EmployeeDisabled).
func (s *AuthService) Authenticate(ctx context.Context, rawToken string) (*domain.Principal, error) {
	raw := strings.TrimSpace(rawToken)
	if raw == "" {
		return nil, domain.ErrUnauthenticated
	}

	hash := crypto.HashToken(raw)

	row, err := s.tokens.FindActiveByHash(ctx, hash)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil, domain.ErrTokenInvalid
		}
		return nil, fmt.Errorf("auth: find token: %w", err)
	}

	now := s.clock()
	switch {
	case row.Token.RevokedAt != nil:
		return nil, domain.ErrTokenRevoked
	case !row.Token.IsActive(now):
		return nil, domain.ErrTokenExpired
	case row.EmployeeStatus != domain.EmployeeStatusEnabled:
		// Даже если триггер revoke_tokens_on_employee_disable почему-то
		// не сработал — здесь страхуем: DISABLED-сотрудник не аутентифицируется.
		return nil, domain.ErrEmployeeDisabled
	}

	// Обновление last_used_at — best-effort, в отдельной горутине с собственным
	// контекстом (по той же логике, что notifyAsync в FeedbackService).
	// Контекст запроса может уже закрыться к моменту записи в БД — это нормально.
	s.touchLastUsedAsync(row.Token.ID, now)

	return &domain.Principal{
		EmployeeID: row.Token.EmployeeID,
		TokenID:    row.Token.ID,
		Role:       row.EmployeeRole,
		Status:     row.EmployeeStatus,
	}, nil
}

// Logout отзывает токен по его id: ставит revoked_at = now и
// revoked_reason = "user logout". Принимает id из Principal'а — middleware
// уже гарантирует, что токен валиден на момент вызова, поэтому здесь
// никаких дополнительных проверок не делаем.
//
// Идемпотентен: повторный вызов на уже отозванный токен — no-op
// (WHERE revoked_at IS NULL на стороне SQL).
func (s *AuthService) Logout(ctx context.Context, tokenID uuid.UUID) error {
	if err := s.tokens.Revoke(ctx, tokenID, "user logout", s.clock()); err != nil {
		return fmt.Errorf("auth: logout: %w", err)
	}
	s.logger.Info("logout", "token_id", tokenID)
	return nil
}

// Login проверяет email+пароль и при успехе создаёт новый ACCESS-токен.
//
// Стратегия защиты от перечисления пользователей:
//   - все негативные исходы (нет email / неверный пароль / DISABLED) возвращают
//     одинаковую ошибку ErrInvalidCredentials;
//   - даже когда email не найден, мы выполняем bcrypt.Verify против dummy-хэша,
//     чтобы тайминг был сопоставим с реальной проверкой.
func (s *AuthService) Login(ctx context.Context, in LoginInput) (*LoginOutput, error) {
	email := normalizeEmail(in.Email)
	if email == "" || in.Password == "" {
		return nil, domain.ErrInvalidCredentials
	}

	emp, err := s.employees.FindByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			// Constant-time defense: сжигаем сравнимое bcrypt-время,
			// чтобы атакующий не определял существование email по таймингу.
			_ = s.passwords.Verify(in.Password, s.dummyPasswordHash)
			return nil, domain.ErrInvalidCredentials
		}
		return nil, fmt.Errorf("auth: find employee by email: %w", err)
	}

	if err := s.passwords.Verify(in.Password, emp.PasswordHash); err != nil {
		if errors.Is(err, crypto.ErrPasswordMismatch) {
			return nil, domain.ErrInvalidCredentials
		}
		return nil, fmt.Errorf("auth: verify password: %w", err)
	}

	// DISABLED-аккаунты не должны логиниться. Возвращаем тот же код —
	// клиенту не нужно знать, что аккаунт существует, но отключён.
	if !emp.IsActive() {
		return nil, domain.ErrInvalidCredentials
	}

	rawToken, err := crypto.GenerateOpaqueToken(crypto.DefaultTokenByteLength)
	if err != nil {
		return nil, fmt.Errorf("auth: generate token: %w", err)
	}

	now := s.clock()
	token := &domain.AuthToken{
		ID:             uuid.New(),
		EmployeeID:     emp.ID,
		TokenHash:      crypto.HashToken(rawToken),
		TokenType:      domain.TokenTypeAccess,
		IssuedAt:       now,
		ExpiresAt:      now.Add(s.tokenTTL),
		IPAddress:      in.ClientInfo.IPAddress,
		UserAgent:      in.ClientInfo.UserAgent,
		DeviceType:     in.ClientInfo.DeviceType,
		DeviceName:     in.ClientInfo.DeviceName,
		OS:             in.ClientInfo.OS,
		OSVersion:      in.ClientInfo.OSVersion,
		Browser:        in.ClientInfo.Browser,
		BrowserVersion: in.ClientInfo.BrowserVersion,
		AppVersion:     in.ClientInfo.AppVersion,
	}

	if err := s.tokens.Create(ctx, token); err != nil {
		return nil, fmt.Errorf("auth: persist token: %w", err)
	}

	s.logger.Info("login success",
		"employee_id", emp.ID,
		"token_id", token.ID,
		"device_type", token.DeviceType,
	)

	return &LoginOutput{
		Token:     rawToken,
		TokenType: "Bearer",
		ExpiresAt: token.ExpiresAt,
		Employee: LoginEmployee{
			ID:       emp.ID,
			FullName: emp.FullName,
			Email:    emp.Email,
			Role:     emp.Role,
		},
	}, nil
}

func (s *AuthService) touchLastUsedAsync(id uuid.UUID, now time.Time) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), touchLastUsedTimeout)
		defer cancel()
		if err := s.tokens.TouchLastUsed(ctx, id, now); err != nil {
			s.logger.Warn("touch auth token last_used_at",
				"err", err,
				"token_id", id,
			)
		}
	}()
}

func normalizeEmail(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}
