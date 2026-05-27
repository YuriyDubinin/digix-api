package domain

import (
	"time"

	"github.com/google/uuid"
)

// Registry — подключение к Docker registry (строка таблицы registries).
// PasswordEncrypted хранит ШИФРТЕКСТ (AES-GCM), не открытый пароль/токен.
type Registry struct {
	ID                uuid.UUID
	Name              string
	Type              string // ENUM registry_type
	URL               string
	Username          string
	PasswordEncrypted string
	Email             string
	Namespace         string
	IsDefault         bool
	IsActive          bool
	Insecure          bool
	LastCheckedAt     *time.Time
	LastStatus        string
	LastError         string
	CreatedAt         time.Time
	UpdatedAt         time.Time
	DeletedAt         *time.Time
}

// Значения ENUM registry_type из миграции 000004.
const (
	RegistryTypeDockerHub = "DOCKERHUB"
	RegistryTypeGHCR      = "GHCR"
	RegistryTypeGitLab    = "GITLAB"
	RegistryTypeHarbor    = "HARBOR"
	RegistryTypeECR       = "ECR"
	RegistryTypeGeneric   = "GENERIC"
)

func IsValidRegistryType(t string) bool {
	switch t {
	case RegistryTypeDockerHub, RegistryTypeGHCR, RegistryTypeGitLab,
		RegistryTypeHarbor, RegistryTypeECR, RegistryTypeGeneric:
		return true
	default:
		return false
	}
}

// RegistryListFilter — параметры выборки списка registry.
// Указатели (*bool) означают «фильтр не задан», если nil.
// Limit/Offset рассчитываются service-слоем из page/page_size.
type RegistryListFilter struct {
	Type      string // "" = любой
	IsActive  *bool  // nil = любой
	IsDefault *bool  // nil = любой
	Search    string // ILIKE по name; "" = без поиска

	Limit    int
	Offset   int
	SortBy   string // имя поля; репозиторий валидирует по whitelist
	SortDesc bool
}
