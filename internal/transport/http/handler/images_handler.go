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

// imagesCollectTimeout — общий бюджет на сбор образов. Один Engine API запрос,
// но при большом числе образов и медленном демоне нужен потолок.
const imagesCollectTimeout = 15 * time.Second

// ImagesCollector — узкий контракт хендлера. Реализуется *docker.Collector.
type ImagesCollector interface {
	CollectImages(ctx context.Context) *docker.ImagesInfo
}

type ImagesHandler struct {
	collector ImagesCollector
	logger    *slog.Logger
}

func NewImagesHandler(c ImagesCollector, logger *slog.Logger) *ImagesHandler {
	return &ImagesHandler{collector: c, logger: logger}
}

// List возвращает все образы хоста с максимумом данных. Защищённый роут.
//
// Всегда 200: недоступность Docker — это валидное состояние (available=false),
// а не ошибка сервера. Фронт отрисует «Docker недоступен» из тела ответа.
func (h *ImagesHandler) List(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), imagesCollectTimeout)
	defer cancel()

	info := h.collector.CollectImages(ctx)

	if !info.Available {
		h.logger.Warn("docker unavailable",
			"reason", info.Reason,
			"request_id", mw.RequestIDFromContext(r.Context()),
		)
	}

	response.WriteJSON(w, http.StatusOK, info)
}
