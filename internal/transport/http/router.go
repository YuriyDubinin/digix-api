package transporthttp

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/YuriyDubinin/digix-api/internal/transport/http/handler"
	mw "github.com/YuriyDubinin/digix-api/internal/transport/http/middleware"
)

type Deps struct {
	Logger          *slog.Logger
	HealthHandler   *handler.HealthHandler
	FeedbackHandler *handler.FeedbackHandler
}

func NewRouter(deps Deps) http.Handler {
	r := chi.NewRouter()

	r.Use(mw.RequestID)
	r.Use(mw.Logger(deps.Logger))
	r.Use(mw.Recover(deps.Logger))
	r.Use(mw.CORS)

	r.Route("/api", func(r chi.Router) {
		r.Get("/ping", deps.HealthHandler.Ping)

		r.Route("/feedbacks", func(r chi.Router) {
			r.Post("/requests", deps.FeedbackHandler.CreateRequest)
		})
	})

	return r
}
