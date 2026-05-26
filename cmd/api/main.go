package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/YuriyDubinin/dijex-api/internal/config"
	"github.com/YuriyDubinin/dijex-api/internal/notifier/telegram"
	"github.com/YuriyDubinin/dijex-api/internal/repository/postgres"
	"github.com/YuriyDubinin/dijex-api/internal/service"
	"github.com/YuriyDubinin/dijex-api/internal/sysinfo"
	transporthttp "github.com/YuriyDubinin/dijex-api/internal/transport/http"
	"github.com/YuriyDubinin/dijex-api/internal/transport/http/handler"
	"github.com/YuriyDubinin/dijex-api/pkg/crypto"
	"github.com/YuriyDubinin/dijex-api/pkg/logger"
	"github.com/YuriyDubinin/dijex-api/pkg/validator"
)

// appVersion — резерв для билд-флага: можно задавать через
//   go build -ldflags "-X main.appVersion=..."
// Сейчас по умолчанию — из VCS info через runtime/debug.
var appVersion = "0.1.0"

func main() {
	startedAt := time.Now().UTC()

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
	employeeRepo := postgres.NewEmployeeRepository(pool)

	telegramNotifier := telegram.NewClient(cfg.Telegram.BotToken, cfg.Telegram.ChatID)

	passwordHasher, err := crypto.NewPasswordHasher(cfg.Auth.PasswordHashCost)
	if err != nil {
		log.Error("init password hasher", "err", err)
		os.Exit(1)
	}

	feedbackService := service.NewFeedbackService(feedbackRepo, telegramNotifier, log)
	authService, err := service.NewAuthService(authTokenRepo, employeeRepo, passwordHasher, cfg.Auth.TokenTTL, log)
	if err != nil {
		log.Error("init auth service", "err", err)
		os.Exit(1)
	}

	v := validator.New()

	systemCollector := sysinfo.NewCollector(sysinfo.AppMeta{
		Name:      "dijex-api",
		Env:       cfg.App.Env,
		Version:   appVersion,
		StartedAt: startedAt,
		HTTPPort:  cfg.HTTP.Port,
	}, pool)

	healthHandler := handler.NewHealthHandler()
	feedbackHandler := handler.NewFeedbackHandler(feedbackService, v, log)
	authHandler := handler.NewAuthHandler(authService, v, log)
	meHandler := handler.NewMeHandler()
	systemHandler := handler.NewSystemHandler(systemCollector, log)

	router := transporthttp.NewRouter(transporthttp.Deps{
		Logger:          log,
		Authenticator:   authService,
		HealthHandler:   healthHandler,
		FeedbackHandler: feedbackHandler,
		AuthHandler:     authHandler,
		MeHandler:       meHandler,
		SystemHandler:   systemHandler,
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
