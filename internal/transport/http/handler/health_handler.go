package handler

import (
	"net/http"

	"github.com/YuriyDubinin/dijex-api/internal/transport/http/response"
)

type HealthHandler struct{}

func NewHealthHandler() *HealthHandler {
	return &HealthHandler{}
}

func (h *HealthHandler) Ping(w http.ResponseWriter, _ *http.Request) {
	response.WriteJSON(w, http.StatusOK, map[string]string{
		"status":  "OK",
		"message": "API is up and running",
	})
}
