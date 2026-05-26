package middleware

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/YuriyDubinin/dijex-api/internal/domain"
	"github.com/YuriyDubinin/dijex-api/internal/transport/http/response"
)

const (
	authHeader   = "Authorization"
	bearerPrefix = "Bearer "
	wwwAuth      = `Bearer realm="api"`
)

type principalKey struct{}

// Auth — мидлварь защищённых роутов.
// Извлекает токен из заголовка `Authorization: Bearer <token>`, прогоняет
// через Authenticator и кладёт Principal в context. При любой ошибке
// валидации отвечает 401 и не вызывает next; при внутренней ошибке — 500.
//
// Используется через chi-группу:
//
//	r.Group(func(r chi.Router) {
//	    r.Use(mw.Auth(authenticator, logger))
//	    r.Get("/me", meHandler.Get)
//	})
func Auth(auth domain.Authenticator, logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw := extractBearerToken(r.Header.Get(authHeader))
			if raw == "" {
				writeUnauthorized(w, "UNAUTHORIZED", "missing or invalid Authorization header")
				return
			}

			principal, err := auth.Authenticate(r.Context(), raw)
			if err != nil {
				if code, msg, ok := mapAuthError(err); ok {
					writeUnauthorized(w, code, msg)
					return
				}
				// Любая другая ошибка — внутренняя (например, отказ БД).
				logger.Error("auth middleware: authenticate",
					"err", err,
					"request_id", RequestIDFromContext(r.Context()),
				)
				response.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error")
				return
			}

			ctx := context.WithValue(r.Context(), principalKey{}, principal)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// PrincipalFromContext возвращает Principal, положенный мидлварью Auth.
// Защищённый хендлер может рассчитывать, что Principal там есть; если нет —
// это означает, что хендлер привинчен мимо мидлвари (баг конфигурации).
func PrincipalFromContext(ctx context.Context) (*domain.Principal, bool) {
	p, ok := ctx.Value(principalKey{}).(*domain.Principal)
	return p, ok
}

func extractBearerToken(header string) string {
	header = strings.TrimSpace(header)
	if header == "" {
		return ""
	}
	if !strings.HasPrefix(header, bearerPrefix) {
		return ""
	}
	return strings.TrimSpace(header[len(bearerPrefix):])
}

// mapAuthError приводит доменные auth-ошибки к (error_code, message)
// для тела 401-ответа. Возвращает ok=false для всех остальных ошибок —
// их следует логировать и отвечать 500.
func mapAuthError(err error) (code, message string, ok bool) {
	switch {
	case errors.Is(err, domain.ErrUnauthenticated), errors.Is(err, domain.ErrTokenInvalid):
		return "UNAUTHORIZED", "invalid auth token", true
	case errors.Is(err, domain.ErrTokenExpired):
		return "TOKEN_EXPIRED", "auth token expired", true
	case errors.Is(err, domain.ErrTokenRevoked):
		return "TOKEN_REVOKED", "auth token revoked", true
	case errors.Is(err, domain.ErrEmployeeDisabled):
		return "EMPLOYEE_DISABLED", "employee account disabled", true
	default:
		return "", "", false
	}
}

func writeUnauthorized(w http.ResponseWriter, code, message string) {
	w.Header().Set("WWW-Authenticate", wwwAuth)
	response.WriteError(w, http.StatusUnauthorized, code, message)
}
