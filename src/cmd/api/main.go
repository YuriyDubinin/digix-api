package main

import (
	"log"
	"os"

	"github.com/digix/digix-api/internal/config"
	"github.com/digix/digix-api/pkg/logger"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Printf("config load failed: %v", err)
		os.Exit(1)
	}

	l := logger.New(cfg.Log.Level, cfg.App.Env)

	l.Info("service starting",
		"env", cfg.App.Env,
		"http_port", cfg.HTTP.Port,
	)

	// TODO: на следующих шагах — HTTP-сервер, БД, ожидание сигнала.

	l.Info("service stopped")
}
