package handler

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/YuriyDubinin/dijex-api/internal/systemd"
	mw "github.com/YuriyDubinin/dijex-api/internal/transport/http/middleware"
	"github.com/YuriyDubinin/dijex-api/internal/transport/http/response"
)

// servicesCollectTimeout — потолок сбора. Опрос свойств идёт параллельно по
// локальному сокету, но при большом числе юнитов нужен дедлайн.
const servicesCollectTimeout = 15 * time.Second

// ServicesCollector — узкий контракт хендлера. Реализуется *systemd.Collector.
type ServicesCollector interface {
	Collect(ctx context.Context) *systemd.ServicesInfo
}

type ServicesHandler struct {
	collector ServicesCollector
	logger    *slog.Logger
}

func NewServicesHandler(c ServicesCollector, logger *slog.Logger) *ServicesHandler {
	return &ServicesHandler{collector: c, logger: logger}
}

// List возвращает системные сервисы (systemd .service). Защищённый роут.
//
// Всегда 200: недоступность systemd (не Linux / не примонтирован сокет) —
// валидное состояние available=false, а не ошибка сервера.
func (h *ServicesHandler) List(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), servicesCollectTimeout)
	defer cancel()

	info := h.collector.Collect(ctx)

	if !info.Available {
		h.logger.Warn("systemd unavailable",
			"reason", info.Reason,
			"request_id", mw.RequestIDFromContext(r.Context()),
		)
	}

	response.WriteJSON(w, http.StatusOK, info)
}
