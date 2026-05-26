package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"

	"github.com/YuriyDubinin/dijex-api/pkg/crypto"
)

type Config struct {
	App      AppConfig
	HTTP     HTTPConfig
	Postgres PostgresConfig
	Log      LogConfig
	Telegram TelegramConfig
	Auth     AuthConfig
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

type TelegramConfig struct {
	BotToken string
	ChatID   string
}

// AuthConfig — параметры для крипто-помощников из pkg/crypto.
//
//   - TokenSecret — секрет для HMAC-подписи signed-токенов (>= 32 символов).
//     Подписывает токены с payload: ссылки подтверждения email, сброса пароля и т.п.
//     НЕ используется для основного auth-токена сессии (тот opaque).
//
//   - PasswordHashCost — bcrypt cost factor для хэширования паролей.
//     Дефолт DefaultPasswordCost (12) — ~250ms на современной машине.
type AuthConfig struct {
	TokenSecret      string
	PasswordHashCost int
}

func Load() (*Config, error) {
	// .env — опционально (например, для локальной разработки).
	// Отсутствие файла не ошибка; ошибки парсинга прокидываем дальше.
	if err := godotenv.Load(); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("load .env: %w", err)
	}

	cfg := &Config{
		App: AppConfig{
			Env: getString("ENV", "development"),
		},
		HTTP: HTTPConfig{
			Port: getString("HTTP_PORT", "8080"),
		},
		Postgres: PostgresConfig{
			Host:     getString("POSTGRES_HOST", "localhost"),
			Port:     getString("POSTGRES_PORT", "5432"),
			User:     os.Getenv("POSTGRES_USER"),
			Password: os.Getenv("POSTGRES_PASSWORD"),
			Database: os.Getenv("POSTGRES_DB"),
			SSLMode:  getString("POSTGRES_SSL_MODE", "disable"),
		},
		Log: LogConfig{
			Level: getString("LOG_LEVEL", "info"),
		},
		Telegram: TelegramConfig{
			BotToken: os.Getenv("TELEGRAM_BOT_TOKEN"),
			ChatID:   os.Getenv("TELEGRAM_CHAT_ID"),
		},
		Auth: AuthConfig{
			TokenSecret: os.Getenv("AUTH_TOKEN_SECRET"),
		},
	}

	var err error
	if cfg.HTTP.ReadTimeout, err = getDuration("HTTP_READ_TIMEOUT", 10*time.Second); err != nil {
		return nil, err
	}
	if cfg.HTTP.WriteTimeout, err = getDuration("HTTP_WRITE_TIMEOUT", 10*time.Second); err != nil {
		return nil, err
	}
	if cfg.HTTP.ShutdownTimeout, err = getDuration("HTTP_SHUTDOWN_TIMEOUT", 10*time.Second); err != nil {
		return nil, err
	}
	if cfg.Postgres.MaxConns, err = getInt32("POSTGRES_MAX_CONNS", 10); err != nil {
		return nil, err
	}
	if cfg.Auth.PasswordHashCost, err = getInt("PASSWORD_HASH_COST", crypto.DefaultPasswordCost); err != nil {
		return nil, err
	}

	if err := cfg.validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

func (c *Config) validate() error {
	var missing []string
	if c.Postgres.User == "" {
		missing = append(missing, "POSTGRES_USER")
	}
	if c.Postgres.Password == "" {
		missing = append(missing, "POSTGRES_PASSWORD")
	}
	if c.Postgres.Database == "" {
		missing = append(missing, "POSTGRES_DB")
	}
	if c.Telegram.BotToken == "" {
		missing = append(missing, "TELEGRAM_BOT_TOKEN")
	}
	if c.Telegram.ChatID == "" {
		missing = append(missing, "TELEGRAM_CHAT_ID")
	}
	if c.Auth.TokenSecret == "" {
		missing = append(missing, "AUTH_TOKEN_SECRET")
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required env vars: %s", strings.Join(missing, ", "))
	}
	if c.Postgres.MaxConns <= 0 {
		return errors.New("POSTGRES_MAX_CONNS must be > 0")
	}
	if len(c.Auth.TokenSecret) < crypto.MinTokenSecretLength {
		return fmt.Errorf("AUTH_TOKEN_SECRET must be at least %d characters", crypto.MinTokenSecretLength)
	}
	if c.Auth.PasswordHashCost < crypto.MinPasswordCost || c.Auth.PasswordHashCost > crypto.MaxPasswordCost {
		return fmt.Errorf("PASSWORD_HASH_COST must be in [%d, %d]", crypto.MinPasswordCost, crypto.MaxPasswordCost)
	}
	return nil
}

func (p PostgresConfig) DSN() string {
	u := url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(p.User, p.Password),
		Host:   net.JoinHostPort(p.Host, p.Port),
		Path:   "/" + p.Database,
	}
	q := u.Query()
	q.Set("sslmode", p.SSLMode)
	u.RawQuery = q.Encode()
	return u.String()
}

func getString(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func getDuration(key string, def time.Duration) (time.Duration, error) {
	raw, ok := os.LookupEnv(key)
	if !ok || raw == "" {
		return def, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("invalid %s=%q: %w", key, raw, err)
	}
	return d, nil
}

func getInt32(key string, def int32) (int32, error) {
	raw, ok := os.LookupEnv(key)
	if !ok || raw == "" {
		return def, nil
	}
	v, err := strconv.ParseInt(raw, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("invalid %s=%q: %w", key, raw, err)
	}
	return int32(v), nil
}

func getInt(key string, def int) (int, error) {
	raw, ok := os.LookupEnv(key)
	if !ok || raw == "" {
		return def, nil
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("invalid %s=%q: %w", key, raw, err)
	}
	return v, nil
}
