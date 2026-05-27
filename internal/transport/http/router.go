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
	AuthHandler       *handler.AuthHandler
	MeHandler         *handler.MeHandler
	SystemHandler     *handler.SystemHandler
	ContainersHandler *handler.ContainersHandler
	ServicesHandler   *handler.ServicesHandler
	RegistryHandler   *handler.RegistryHandler
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

		// Логин не требует Bearer-токена — это вход в систему.
		r.Route("/auth", func(r chi.Router) {
			r.Post("/login", deps.AuthHandler.Login)
		})

		// ──────────────────────── Защищённые роуты ─────────────────────────
		// Внутри группы действует мидлварь Auth: каждый запрос обязан принести
		// заголовок `Authorization: Bearer <token>`. Мидлварь валидирует токен
		// и кладёт Principal в context. При ошибке вернёт 401, не пуская
		// дальше по цепочке.
		r.Group(func(r chi.Router) {
			r.Use(mw.Auth(deps.Authenticator, deps.Logger))

			r.Get("/me", deps.MeHandler.Get)

			// Полный снимок состояния машины для админ-консоли (вкладка Core).
			r.Get("/system/main", deps.SystemHandler.Get)

			// Список Docker-контейнеров (вкладка Containers).
			r.Get("/system/containers", deps.ContainersHandler.List)

			// Список системных сервисов systemd (вкладка Servers).
			r.Get("/system/services", deps.ServicesHandler.List)

			// Подключения к Docker registry.
			r.Post("/registries/create", deps.RegistryHandler.Create)
			r.Get("/registries/list", deps.RegistryHandler.List)
			r.Put("/registries/update", deps.RegistryHandler.Update)
			r.Delete("/registries/delete", deps.RegistryHandler.Delete)
			r.Post("/registries/connect", deps.RegistryHandler.Connect)
			r.Post("/registries/ping", deps.RegistryHandler.Ping)
			r.Post("/registries/images", deps.RegistryHandler.Images)

			// Логaut — защищённый, потому что нельзя «разлогиниться» без
			// валидного токена. Auth middleware сам отдаст 401 при любом
			// негативном исходе (нет токена / истёк / уже отозван).
			r.Post("/auth/logout", deps.AuthHandler.Logout)

			// Сюда же добавляются новые защищённые эндпоинты, например:
			//   r.Route("/employees", func(r chi.Router) {
			//       r.Get("/", deps.EmployeeHandler.List)
			//       r.Get("/{id}", deps.EmployeeHandler.GetByID)
			//   })
		})
	})

	return r
}
