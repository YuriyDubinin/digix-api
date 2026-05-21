package config

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	App      AppConfig
	HTTP     HTTPConfig
	Postgres PostgresConfig
	Log      LogConfig
}

type AppConfig struct {
	Env string
}

type HTTPConfig struct {
	Port            string
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	ShutdownTimeout time.Duration
}

type PostgresConfig struct {
	Host     string
	Port     string
	User     string
	Password string
	Database string
	SSLMode  string
	MaxConns int32
}

type LogConfig struct {
	Level string
}

// DSN собирает строку подключения для pgx/golang-migrate.
func (p PostgresConfig) DSN() string {
	return fmt.Sprintf(
		"postgres://%s:%s@%s:%s/%s?sslmode=%s",
		p.User, p.Password, p.Host, p.Port, p.Database, p.SSLMode,
	)
}

// Load читает .env (если есть), окружение, применяет дефолты и валидирует.
func Load() (*Config, error) {
	// Отсутствие .env — не ошибка: в проде переменные приходят из окружения.
	_ = godotenv.Load()

	cfg := &Config{
		App: AppConfig{
			Env: getEnv("ENV", "development"),
		},
		HTTP: HTTPConfig{
			Port: getEnv("HTTP_PORT", "8080"),
		},
		Postgres: PostgresConfig{
			Host:     getEnv("POSTGRES_HOST", "localhost"),
			Port:     getEnv("POSTGRES_PORT", "5432"),
			User:     os.Getenv("POSTGRES_USER"),
			Password: os.Getenv("POSTGRES_PASSWORD"),
			Database: os.Getenv("POSTGRES_DB"),
			SSLMode:  getEnv("POSTGRES_SSL_MODE", "disable"),
		},
		Log: LogConfig{
			Level: getEnv("LOG_LEVEL", "info"),
		},
	}

	var err error
	if cfg.HTTP.ReadTimeout, err = getDuration("HTTP_READ_TIMEOUT", 10*time.Second); err != nil {
		return nil, err
	}
	if cfg.HTTP.WriteTimeout, err = getDuration("HTTP_WRITE_TIMEOUT", 10*time.Second); err != nil {
		return nil, err
	}
	if cfg.HTTP.ShutdownTimeout, err = getDuration("HTTP_SHUTDOWN_TIMEOUT", 15*time.Second); err != nil {
		return nil, err
	}
	if cfg.Postgres.MaxConns, err = getInt32("POSTGRES_MAX_CONNS", 10); err != nil {
		return nil, err
	}

	if err := validate(cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

func validate(cfg *Config) error {
	var missing []string
	if cfg.Postgres.User == "" {
		missing = append(missing, "POSTGRES_USER")
	}
	if cfg.Postgres.Password == "" {
		missing = append(missing, "POSTGRES_PASSWORD")
	}
	if cfg.Postgres.Database == "" {
		missing = append(missing, "POSTGRES_DB")
	}
	if len(missing) > 0 {
		return fmt.Errorf("config: missing required env vars: %v", missing)
	}
	if cfg.Postgres.MaxConns <= 0 {
		return fmt.Errorf("config: POSTGRES_MAX_CONNS must be > 0, got %d", cfg.Postgres.MaxConns)
	}
	return nil
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getDuration(key string, def time.Duration) (time.Duration, error) {
	v := os.Getenv(key)
	if v == "" {
		return def, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0, fmt.Errorf("config: invalid duration for %s=%q: %w", key, v, err)
	}
	return d, nil
}

func getInt32(key string, def int32) (int32, error) {
	v := os.Getenv(key)
	if v == "" {
		return def, nil
	}
	n, err := strconv.ParseInt(v, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("config: invalid int for %s=%q: %w", key, v, err)
	}
	return int32(n), nil
}
