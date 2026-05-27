package service

import (
	"time"

	"github.com/google/uuid"
)

// CreateServerInput — данные создания сервера. Секреты — в открытом виде,
// шифруются в сервисе. Пустые поля допустимы (детали можно задать позже).
type CreateServerInput struct {
	Name                 string
	Host                 string
	Port                 int
	Protocol             string
	Username             string
	AuthMethod           string
	Password             string
	PrivateKey           string
	PrivateKeyPassphrase string
	Description          string
	Environment          string
	Provider             string
	Location             string
	Tags                 []string
}

// UpdateServerInput — полное обновление. Секреты — указатели:
//   nil   → оставить текущий;
//   ""    → очистить;
//   "..." → задать новый (шифруется).
type UpdateServerInput struct {
	ID                   uuid.UUID
	Name                 string
	Host                 string
	Port                 int
	Protocol             string
	Username             string
	AuthMethod           string
	Password             *string
	PrivateKey           *string
	PrivateKeyPassphrase *string
	Description          string
	Environment          string
	Provider             string
	Location             string
	Tags                 []string
	IsActive             bool
}

type ListServersInput struct {
	Page        int
	PageSize    int
	Environment string
	Protocol    string
	AuthMethod  string
	IsActive    *bool
	Search      string
	SortBy      string
	Order       string
}

// ServerView — представление сервера наружу. Секреты НЕ отдаются — только
// флаги has_password / has_private_key.
type ServerView struct {
	ID         uuid.UUID
	Name       string
	Host       string
	Port       int
	Protocol   string
	Username   string
	AuthMethod string

	Description string
	Environment string
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

	HasPassword   bool
	HasPrivateKey bool

	IsActive      bool
	LastCheckedAt *time.Time
	LastStatus    string

	CreatedAt time.Time
	UpdatedAt time.Time
}

type ListServersOutput struct {
	Items      []ServerView
	Pagination Pagination
}

type DeleteServerOutput struct {
	ID        uuid.UUID
	DeletedAt time.Time
}

type RemoteConnectOutput struct {
	ID        uuid.UUID
	Connected bool
	Method    string // publickey | password
	Status    string // OK | AUTH_FAILED | UNREACHABLE | TIMEOUT | ERROR
	Message   string
	CheckedAt time.Time

	// Факты (если собрались при успешном подключении).
	RemoteHostname string
	OS             string
	KernelVersion  string
	Arch           string
	CPUCores       *int
}

type RemotePingOutput struct {
	ID        uuid.UUID
	Connected bool
	Method    string
	Status    string
	Message   string
	IsActive  bool
	CheckedAt time.Time
}
