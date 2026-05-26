package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/YuriyDubinin/dijex-api/internal/domain"
	"github.com/YuriyDubinin/dijex-api/internal/service"
	"github.com/YuriyDubinin/dijex-api/internal/transport/http/clientinfo"
	"github.com/YuriyDubinin/dijex-api/internal/transport/http/dto"
	mw "github.com/YuriyDubinin/dijex-api/internal/transport/http/middleware"
	"github.com/YuriyDubinin/dijex-api/internal/transport/http/response"
	"github.com/YuriyDubinin/dijex-api/pkg/validator"
)

// AuthLoginService — узкий контракт, на котором зависит хендлер.
// Позволяет легко мокать в тестах и не таскает в handler весь AuthService.
type AuthLoginService interface {
	Login(ctx context.Context, input service.LoginInput) (*service.LoginOutput, error)
}

type AuthHandler struct {
	service   AuthLoginService
	validator *validator.Validator
	logger    *slog.Logger
}

func NewAuthHandler(svc AuthLoginService, v *validator.Validator, logger *slog.Logger) *AuthHandler {
	return &AuthHandler{service: svc, validator: v, logger: logger}
}

// Login обрабатывает POST /api/auth/login.
//
// Контракт ошибок (по убыванию приоритета):
//   - 400 INVALID_JSON              — тело не парсится / неизвестные поля
//   - 422 VALIDATION_ERROR          — провал структурной валидации (email/пароль пустой и т.п.)
//   - 401 INVALID_CREDENTIALS       — единый ответ на «нет email / пароль не совпал / DISABLED»
//   - 500 INTERNAL_ERROR            — любая внутренняя ошибка (БД и т.п.)
//   - 201 / 200                     — успех; тело — LoginHTTPResponse
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)

	var req dto.LoginHTTPRequest
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

	// Собираем метаданные клиента: IP, UA-парсинг, X-Device-Name, X-App-Version.
	ci := clientinfo.Extract(r)
	svcInput := req.ToServiceInput(toServiceClientInfo(ci))

	out, err := h.service.Login(r.Context(), svcInput)
	if err != nil {
		// Единый ответ для всех негативных исходов логина.
		if errors.Is(err, domain.ErrInvalidCredentials) {
			response.WriteError(w, http.StatusUnauthorized, "INVALID_CREDENTIALS", "invalid email or password")
			return
		}
		h.logger.Error("login",
			"err", err,
			"request_id", mw.RequestIDFromContext(r.Context()),
		)
		response.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error")
		return
	}

	response.WriteJSON(w, http.StatusOK, dto.FromLoginOutput(out))
}

func toServiceClientInfo(ci clientinfo.ClientInfo) service.ClientInfo {
	return service.ClientInfo{
		IPAddress:      ci.IPAddress,
		UserAgent:      ci.UserAgent,
		DeviceType:     ci.DeviceType,
		DeviceName:     ci.DeviceName,
		OS:             ci.OS,
		OSVersion:      ci.OSVersion,
		Browser:        ci.Browser,
		BrowserVersion: ci.BrowserVersion,
		AppVersion:     ci.AppVersion,
	}
}
