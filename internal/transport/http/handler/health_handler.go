package handler

import (
	"context"
	"net/http"
	"time"

	"github.com/YuriyDubinin/digix-api/internal/transport/http/response"
)

type DBPinger interface {
	Ping(ctx context.Context) error
}

type HealthHandler struct {
	pinger DBPinger
}

func NewHealthHandler(pinger DBPinger) *HealthHandler {
	return &HealthHandler{pinger: pinger}
}

func (h *HealthHandler) Liveness(w http.ResponseWriter, _ *http.Request) {
	response.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *HealthHandler) Readiness(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	if err := h.pinger.Ping(ctx); err != nil {
		response.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unavailable"})
		return
	}
	response.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
