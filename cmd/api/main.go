package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/YuriyDubinin/dijex-api/internal/config"
	"github.com/YuriyDubinin/dijex-api/internal/notifier/telegram"
	"github.com/YuriyDubinin/dijex-api/internal/repository/postgres"
	"github.com/YuriyDubinin/dijex-api/internal/service"
	transporthttp "github.com/YuriyDubinin/dijex-api/internal/transport/http"
	"github.com/YuriyDubinin/dijex-api/internal/transport/http/handler"
	"github.com/YuriyDubinin/dijex-api/pkg/logger"
	"github.com/YuriyDubinin/dijex-api/pkg/validator"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load config: %v\n", err)
		os.Exit(1)
	}

	log := logger.New(cfg.Log.Level, cfg.App.Env)
	log.Info("service starting",
		"env", cfg.App.Env,
		"http_port", cfg.HTTP.Port,
	)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	pool, err := postgres.NewPool(ctx, cfg.Postgres.DSN(), cfg.Postgres.MaxConns)
	if err != nil {
		log.Error("connect to database", "err", err)
		os.Exit(1)
	}
	defer pool.Close()
	log.Info("database connected")

	if err := postgres.RunMigrations(cfg.Postgres.DSN(), "migrations", log); err != nil {
		log.Error("run migrations", "err", err)
		os.Exit(1)
	}

	feedbackRepo := postgres.NewFeedbackRepository(pool)
	authTokenRepo := postgres.NewAuthTokenRepository(pool)

	telegramNotifier := telegram.NewClient(cfg.Telegram.BotToken, cfg.Telegram.ChatID)

	feedbackService := service.NewFeedbackService(feedbackRepo, telegramNotifier, log)
	authService := service.NewAuthService(authTokenRepo, log)

	v := validator.New()

	healthHandler := handler.NewHealthHandler()
	feedbackHandler := handler.NewFeedbackHandler(feedbackService, v, log)
	meHandler := handler.NewMeHandler()

	router := transporthttp.NewRouter(transporthttp.Deps{
		Logger:          log,
		Authenticator:   authService,
		HealthHandler:   healthHandler,
		FeedbackHandler: feedbackHandler,
		MeHandler:       meHandler,
	})
	srv := transporthttp.NewServer(cfg.HTTP, router, log)

	log.Info("http server starting on :" + cfg.HTTP.Port)
	log.Info("ready")

	if err := srv.Run(ctx); err != nil {
		log.Error("http server", "err", err)
		os.Exit(1)
	}

	log.Info("service stopped")
}
