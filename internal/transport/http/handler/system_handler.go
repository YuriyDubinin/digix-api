package handler

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/YuriyDubinin/dijex-api/internal/sysinfo"
	mw "github.com/YuriyDubinin/dijex-api/internal/transport/http/middleware"
	"github.com/YuriyDubinin/dijex-api/internal/transport/http/response"
)

// systemCollectionTimeout — общий таймаут сбора данных. Если за это время не
// успели — отдаём 500 (но обычно сбор укладывается в ~300-500ms из-за CPU-семпла).
const systemCollectionTimeout = 8 * time.Second

// SystemCollector — узкий контракт, на котором зависит хендлер.
// Реализуется *sysinfo.Collector.
type SystemCollector interface {
	Collect(ctx context.Context) *sysinfo.SystemInfo
}

type SystemHandler struct {
	collector SystemCollector
	logger    *slog.Logger
}

func NewSystemHandler(c SystemCollector, logger *slog.Logger) *SystemHandler {
	return &SystemHandler{collector: c, logger: logger}
}

// Get возвращает полный снимок состояния машины. Защищённый роут —
// валидация токена выполняется Auth middleware до вызова handler.
func (h *SystemHandler) Get(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), systemCollectionTimeout)
	defer cancel()

	info := h.collector.Collect(ctx)

	// Если ctx был отменён клиентом — лог в info, отдаём как есть
	// (то, что успели собрать). На контракт ответа это не влияет.
	if err := ctx.Err(); err != nil {
		h.logger.Warn("system collect: context",
			"err", err,
			"request_id", mw.RequestIDFromContext(r.Context()),
		)
	}

	response.WriteJSON(w, http.StatusOK, info)
}
