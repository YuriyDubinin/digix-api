package domain

import (
	"time"

	"github.com/google/uuid"
)

// Server — подключение к серверу + базовая информация о нём (таблица servers).
// Секреты (PasswordEncrypted/PrivateKeyEncrypted/PrivateKeyPassphraseEncrypted)
// хранятся как шифртекст AES-GCM. Поля-факты (OS/CPU/Memory/...) заполняются
// после успешного подключения.
type Server struct {
	ID         uuid.UUID
	Name       string
	Host       string
	Port       int
	Protocol   string // ENUM server_protocol
	Username   string
	AuthMethod string // ENUM server_auth_method

	PasswordEncrypted             string
	PrivateKeyEncrypted           string
	PrivateKeyPassphraseEncrypted string

	Description string
	Environment string // ENUM server_environment
	Provider    string
	Location    string
	Tags        []string

	OS               string
	OSVersion        string
	Arch             string
	KernelVersion    string
	RemoteHostname   string
	CPUCores         *int
	MemoryTotalBytes *int64
	DiskTotalBytes   *int64

	IsActive        bool
	SSHKeyInstalled bool // наш SSH-ключ приложения добавлен в authorized_keys этого сервера
	LastCheckedAt   *time.Time
	LastStatus      string
	LastError       string

	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt *time.Time
}

// Значения ENUM server_protocol.
const (
	ServerProtocolSSH   = "SSH"
	ServerProtocolWinRM = "WINRM"
	ServerProtocolRDP   = "RDP"
)

// Значения ENUM server_auth_method.
const (
	ServerAuthPassword   = "PASSWORD"
	ServerAuthPrivateKey = "PRIVATE_KEY"
	ServerAuthAgent      = "AGENT"
)

// Значения ENUM server_environment.
const (
	ServerEnvProduction  = "PRODUCTION"
	ServerEnvStaging     = "STAGING"
	ServerEnvDevelopment = "DEVELOPMENT"
	ServerEnvTesting     = "TESTING"
	ServerEnvOther       = "OTHER"
)

func IsValidServerProtocol(s string) bool {
	switch s {
	case ServerProtocolSSH, ServerProtocolWinRM, ServerProtocolRDP:
		return true
	default:
		return false
	}
}

func IsValidServerAuthMethod(s string) bool {
	switch s {
	case ServerAuthPassword, ServerAuthPrivateKey, ServerAuthAgent:
		return true
	default:
		return false
	}
}

func IsValidServerEnvironment(s string) bool {
	switch s {
	case ServerEnvProduction, ServerEnvStaging, ServerEnvDevelopment, ServerEnvTesting, ServerEnvOther:
		return true
	default:
		return false
	}
}

// ServerFacts — факты о сервере, собранные при подключении.
type ServerFacts struct {
	OS             string
	OSVersion      string
	Arch           string
	KernelVersion  string
	RemoteHostname string
	CPUCores       *int
}

// ServerListFilter — параметры выборки списка серверов.
type ServerListFilter struct {
	Environment string
	Protocol    string
	AuthMethod  string
	IsActive    *bool
	Search      string // ILIKE по name ИЛИ host

	Limit    int
	Offset   int
	SortBy   string
	SortDesc bool
}
