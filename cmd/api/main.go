package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/YuriyDubinin/dijex-api/internal/config"
	"github.com/YuriyDubinin/dijex-api/internal/docker"
	"github.com/YuriyDubinin/dijex-api/internal/geo"
	"github.com/YuriyDubinin/dijex-api/internal/notifier/telegram"
	"github.com/YuriyDubinin/dijex-api/internal/registryclient"
	"github.com/YuriyDubinin/dijex-api/internal/remotedocker"
	"github.com/YuriyDubinin/dijex-api/internal/remoteinfo"
	"github.com/YuriyDubinin/dijex-api/internal/remotesystemd"
	"github.com/YuriyDubinin/dijex-api/internal/repository/postgres"
	"github.com/YuriyDubinin/dijex-api/internal/service"
	"github.com/YuriyDubinin/dijex-api/internal/sshclient"
	"github.com/YuriyDubinin/dijex-api/internal/sshkey"
	"github.com/YuriyDubinin/dijex-api/internal/sysinfo"
	"github.com/YuriyDubinin/dijex-api/internal/systemd"
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
	registryRepo := postgres.NewRegistryRepository(pool)
	serverRepo := postgres.NewServerRepository(pool)

	telegramNotifier := telegram.NewClient(cfg.Telegram.BotToken, cfg.Telegram.ChatID)

	passwordHasher, err := crypto.NewPasswordHasher(cfg.Auth.PasswordHashCost)
	if err != nil {
		log.Error("init password hasher", "err", err)
		os.Exit(1)
	}

	registryCipher, err := crypto.NewCipher(cfg.Registry.EncryptionKey)
	if err != nil {
		log.Error("init registry cipher", "err", err)
		os.Exit(1)
	}

	feedbackService := service.NewFeedbackService(feedbackRepo, telegramNotifier, log)
	authService, err := service.NewAuthService(authTokenRepo, employeeRepo, passwordHasher, cfg.Auth.TokenTTL, log)
	if err != nil {
		log.Error("init auth service", "err", err)
		os.Exit(1)
	}
	registryChecker := registryclient.NewChecker()
	registryService := service.NewRegistryService(registryRepo, registryCipher, registryChecker, log)

	sshManager := sshkey.NewManager(cfg.SSH.KeyPath)
	sshConnector := sshclient.NewConnector(sshManager)

	// Geo-резолвер: одна mmdb-база на весь сервис. Используется sysinfo (для
	// своей машины) и serverService (для удалённых серверов через ServerFacts).
	// Если файл базы повреждён/пуст — это ошибка сборки, не запускаемся вообще.
	geoResolver, err := geo.NewResolver()
	if err != nil {
		log.Error("init geo resolver", "err", err)
		os.Exit(1)
	}
	defer geoResolver.Close()

	// Коллекторы для удалённых /system/* эндпоинтов (через тот же SSH-стек).
	remoteSystemCollector := remoteinfo.NewCollector(geoResolver)
	remoteContainersCollector := remotedocker.NewCollector()
	remoteServicesCollector := remotesystemd.NewCollector()

	// Тот же шифр используем для секретов серверов (один app-ключ на все секреты).
	serverService := service.NewServerService(
		serverRepo, registryCipher, sshConnector, sshManager, geoResolver,
		remoteSystemCollector, remoteContainersCollector, remoteServicesCollector,
		log,
	)

	v := validator.New()

	dockerCollector := docker.NewCollector(cfg.Docker.Host)
	servicesCollector := systemd.NewCollector()

	systemCollector := sysinfo.NewCollector(sysinfo.AppMeta{
		Name:      "dijex-api",
		Env:       cfg.App.Env,
		Version:   appVersion,
		StartedAt: startedAt,
		HTTPPort:  cfg.HTTP.Port,
		PublicIP:  os.Getenv("HOST_PUBLIC_IP"),
	}, pool, dockerCollector, geoResolver)

	healthHandler := handler.NewHealthHandler()
	feedbackHandler := handler.NewFeedbackHandler(feedbackService, v, log)
	authHandler := handler.NewAuthHandler(authService, v, log)
	meHandler := handler.NewMeHandler()
	systemHandler := handler.NewSystemHandler(systemCollector, log)
	containersHandler := handler.NewContainersHandler(dockerCollector, log)
	servicesHandler := handler.NewServicesHandler(servicesCollector, log)
	registryHandler := handler.NewRegistryHandler(registryService, v, log)
	serverHandler := handler.NewServerHandler(serverService, v, log)
	sshHandler := handler.NewSSHHandler(sshManager, log)

	router := transporthttp.NewRouter(transporthttp.Deps{
		Logger:            log,
		Authenticator:     authService,
		HealthHandler:     healthHandler,
		FeedbackHandler:   feedbackHandler,
		AuthHandler:       authHandler,
		MeHandler:         meHandler,
		SystemHandler:     systemHandler,
		ContainersHandler: containersHandler,
		ServicesHandler:   servicesHandler,
		RegistryHandler:   registryHandler,
		ServerHandler:     serverHandler,
		SSHHandler:        sshHandler,
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
