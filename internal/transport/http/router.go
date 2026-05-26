package transporthttp

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/YuriyDubinin/dijex-api/internal/domain"
	"github.com/YuriyDubinin/dijex-api/internal/transport/http/handler"
	mw "github.com/YuriyDubinin/dijex-api/internal/transport/http/middleware"
)

type Deps struct {
	Logger          *slog.Logger
	Authenticator   domain.Authenticator
	HealthHandler   *handler.HealthHandler
	FeedbackHandler *handler.FeedbackHandler
	MeHandler       *handler.MeHandler
}

func NewRouter(deps Deps) http.Handler {
	r := chi.NewRouter()

	// Глобальные мидлвари — применяются ко всем роутам, в том числе публичным.
	r.Use(mw.RequestID)
	r.Use(mw.Logger(deps.Logger))
	r.Use(mw.Recover(deps.Logger))
	r.Use(mw.CORS)

	r.Route("/api", func(r chi.Router) {
		// ───────────────────────── Публичные роуты ─────────────────────────
		// Доступны без авторизации: health-check и приём заявок с лендинга.
		r.Get("/ping", deps.HealthHandler.Ping)

		r.Route("/feedbacks", func(r chi.Router) {
			r.Post("/requests", deps.FeedbackHandler.CreateRequest)
		})

		// ──────────────────────── Защищённые роуты ─────────────────────────
		// Внутри группы действует мидлварь Auth: каждый запрос обязан принести
		// заголовок `Authorization: Bearer <token>`. Мидлварь валидирует токен
		// и кладёт Principal в context. При ошибке вернёт 401, не пуская
		// дальше по цепочке.
		r.Group(func(r chi.Router) {
			r.Use(mw.Auth(deps.Authenticator, deps.Logger))

			r.Get("/me", deps.MeHandler.Get)

			// Сюда же добавляются новые защищённые эндпоинты, например:
			//   r.Route("/employees", func(r chi.Router) {
			//       r.Get("/", deps.EmployeeHandler.List)
			//       r.Get("/{id}", deps.EmployeeHandler.GetByID)
			//   })
		})
	})

	return r
}
