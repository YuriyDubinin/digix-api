package dto

import (
	"time"

	"github.com/google/uuid"

	"github.com/YuriyDubinin/dijex-api/internal/service"
)

// LoginHTTPRequest — тело POST /api/auth/login.
// Валидация пароля намеренно минимальная (только required) — иначе сообщение
// «password too short» при ошибке логина подсказывало бы атакующему,
// что пароль не прошёл по длине, а не по неверности.
type LoginHTTPRequest struct {
	Email    string `json:"email"    validate:"required,email,max=255"`
	Password string `json:"password" validate:"required,max=72"`
}

func (r LoginHTTPRequest) ToServiceInput(ci service.ClientInfo) service.LoginInput {
	return service.LoginInput{
		Email:      r.Email,
		Password:   r.Password,
		ClientInfo: ci,
	}
}

// LoginHTTPResponse — успешный ответ логина.
// Сам токен возвращается ОДИН РАЗ — клиент обязан сохранить его на своей стороне.
// На сервере хранится только хэш, восстановить токен по нему невозможно.
type LoginHTTPResponse struct {
	Token     string                `json:"token"`
	TokenType string                `json:"token_type"` // "Bearer"
	ExpiresAt time.Time             `json:"expires_at"`
	Employee  LoginEmployeeResponse `json:"employee"`
}

type LoginEmployeeResponse struct {
	ID       uuid.UUID `json:"id"`
	FullName string    `json:"full_name"`
	Email    string    `json:"email"`
	Role     string    `json:"role"`
}

func FromLoginOutput(o *service.LoginOutput) LoginHTTPResponse {
	return LoginHTTPResponse{
		Token:     o.Token,
		TokenType: o.TokenType,
		ExpiresAt: o.ExpiresAt,
		Employee: LoginEmployeeResponse{
			ID:       o.Employee.ID,
			FullName: o.Employee.FullName,
			Email:    o.Employee.Email,
			Role:     o.Employee.Role,
		},
	}
}
