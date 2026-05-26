package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/google/uuid"

	"github.com/YuriyDubinin/dijex-api/internal/domain"
	"github.com/YuriyDubinin/dijex-api/internal/service"
	"github.com/YuriyDubinin/dijex-api/internal/transport/http/clientinfo"
	"github.com/YuriyDubinin/dijex-api/internal/transport/http/dto"
	mw "github.com/YuriyDubinin/dijex-api/internal/transport/http/middleware"
	"github.com/YuriyDubinin/dijex-api/internal/transport/http/response"
	"github.com/YuriyDubinin/dijex-api/pkg/validator"
)

// AuthService — узкий контракт, на котором зависит хендлер.
// Позволяет мокать в тестах и не таскает в handler весь *service.AuthService.
type AuthService interface {
	Login(ctx context.Context, input service.LoginInput) (*service.LoginOutput, error)
	Logout(ctx context.Context, tokenID uuid.UUID) error
}

type AuthHandler struct {
	service   AuthService
	validator *validator.Validator
	logger    *slog.Logger
}

func NewAuthHandler(svc AuthService, v *validator.Validator, logger *slog.Logger) *AuthHandler {
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

// Logout обрабатывает POST /api/auth/logout.
//
// Маршрут защищённый — все проверки токена (есть/истёк/отозван/disabled)
// делает Auth middleware и до handler доходят только валидные токены.
// Поэтому здесь только сама операция отзыва.
//
// Ответы:
//   - 200 OK         — токен отозван (или уже был отозван — Revoke идемпотентен).
//   - 500 INTERNAL_ERROR — ошибка БД.
//   - 401 …          — отдают мидлвари до handler (битый/истёкший/отозванный токен).
func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	principal, ok := mw.PrincipalFromContext(r.Context())
	if !ok {
		// Сюда не должны попадать: маршрут под Auth middleware.
		h.logger.Error("logout: principal missing in context",
			"request_id", mw.RequestIDFromContext(r.Context()),
		)
		response.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "principal missing in context")
		return
	}

	if err := h.service.Logout(r.Context(), principal.TokenID); err != nil {
		h.logger.Error("logout",
			"err", err,
			"request_id", mw.RequestIDFromContext(r.Context()),
			"token_id", principal.TokenID,
		)
		response.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error")
		return
	}

	response.WriteJSON(w, http.StatusOK, map[string]string{
		"status":  "LOGGED_OUT",
		"message": "token successfully revoked",
	})
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
