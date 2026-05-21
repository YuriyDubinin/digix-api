package logger

import (
	"log/slog"
	"os"
	"strings"
)

// New строит slog.Logger. В production использует JSON-хендлер, иначе — текстовый.
// Невалидный level откатывается на info.
func New(level, env string) *slog.Logger {
	opts := &slog.HandlerOptions{
		Level: parseLevel(level),
	}

	var handler slog.Handler
	if env == "production" {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		handler = slog.NewTextHandler(os.Stdout, opts)
	}

	return slog.New(handler)
}

func parseLevel(level string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
