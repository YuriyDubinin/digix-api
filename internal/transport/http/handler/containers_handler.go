package handler

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/YuriyDubinin/dijex-api/internal/docker"
	mw "github.com/YuriyDubinin/dijex-api/internal/transport/http/middleware"
	"github.com/YuriyDubinin/dijex-api/internal/transport/http/response"
)

// containersCollectTimeout — общий бюджет на сбор. Inspect делается параллельно,
// но при большом числе контейнеров и медленном демоне нужен потолок.
const containersCollectTimeout = 15 * time.Second

// ContainersCollector — узкий контракт хендлера. Реализуется *docker.Collector.
type ContainersCollector interface {
	Collect(ctx context.Context) *docker.ContainersInfo
}

type ContainersHandler struct {
	collector ContainersCollector
	logger    *slog.Logger
}

func NewContainersHandler(c ContainersCollector, logger *slog.Logger) *ContainersHandler {
	return &ContainersHandler{collector: c, logger: logger}
}

// List возвращает все контейнеры с максимумом данных. Защищённый роут.
//
// Всегда 200: недоступность Docker — это валидное состояние (available=false),
// а не ошибка сервера. Фронт отрисует «Docker недоступен» из тела ответа.
func (h *ContainersHandler) List(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), containersCollectTimeout)
	defer cancel()

	info := h.collector.Collect(ctx)

	if !info.Available {
		h.logger.Warn("docker unavailable",
			"reason", info.Reason,
			"request_id", mw.RequestIDFromContext(r.Context()),
		)
	}

	response.WriteJSON(w, http.StatusOK, info)
}
