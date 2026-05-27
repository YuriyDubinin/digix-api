package dto

import (
	"time"

	"github.com/google/uuid"

	"github.com/YuriyDubinin/dijex-api/internal/service"
)

// CreateRegistryHTTPRequest — тело POST /api/registries.
type CreateRegistryHTTPRequest struct {
	Name      string `json:"name"      validate:"required,min=2,max=100"`
	Type      string `json:"type"      validate:"required,oneof=DOCKERHUB GHCR GITLAB HARBOR ECR GENERIC"`
	URL       string `json:"url"       validate:"required,max=500"`
	Username  string `json:"username"  validate:"omitempty,max=255"`
	Password  string `json:"password"  validate:"omitempty,max=255"`
	Email     string `json:"email"     validate:"omitempty,email,max=255"`
	Namespace string `json:"namespace" validate:"omitempty,max=255"`
	IsDefault bool   `json:"is_default"`
	Insecure  bool   `json:"insecure"`
}

func (r CreateRegistryHTTPRequest) ToServiceInput() service.CreateRegistryInput {
	return service.CreateRegistryInput{
		Name:      r.Name,
		Type:      r.Type,
		URL:       r.URL,
		Username:  r.Username,
		Password:  r.Password,
		Email:     r.Email,
		Namespace: r.Namespace,
		IsDefault: r.IsDefault,
		Insecure:  r.Insecure,
	}
}

// CreateRegistryHTTPResponse — ответ создания. Пароль НЕ возвращается
// (даже зашифрованный): has_credentials лишь сообщает, что он сохранён.
type CreateRegistryHTTPResponse struct {
	ID             uuid.UUID `json:"id"`
	Name           string    `json:"name"`
	Type           string    `json:"type"`
	URL            string    `json:"url"`
	Username       string    `json:"username,omitempty"`
	Email          string    `json:"email,omitempty"`
	Namespace      string    `json:"namespace,omitempty"`
	IsDefault      bool      `json:"is_default"`
	IsActive       bool      `json:"is_active"`
	Insecure       bool      `json:"insecure"`
	HasCredentials bool      `json:"has_credentials"`
	CreatedAt      time.Time `json:"created_at"`
}

func FromCreateRegistryOutput(o *service.CreateRegistryOutput) CreateRegistryHTTPResponse {
	return CreateRegistryHTTPResponse{
		ID:             o.ID,
		Name:           o.Name,
		Type:           o.Type,
		URL:            o.URL,
		Username:       o.Username,
		Email:          o.Email,
		Namespace:      o.Namespace,
		IsDefault:      o.IsDefault,
		IsActive:       o.IsActive,
		Insecure:       o.Insecure,
		HasCredentials: o.HasCredentials,
		CreatedAt:      o.CreatedAt,
	}
}

// ───────────────────────── Update (PUT) ─────────────────────────

// UpdateRegistryHTTPRequest — тело PUT /api/registries/update. Полное обновление.
// Password — указатель: отсутствует → оставить текущий; "" → очистить; значение → задать новый.
type UpdateRegistryHTTPRequest struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"      validate:"required,min=2,max=100"`
	Type      string    `json:"type"      validate:"required,oneof=DOCKERHUB GHCR GITLAB HARBOR ECR GENERIC"`
	URL       string    `json:"url"       validate:"required,max=500"`
	Username  string    `json:"username"  validate:"omitempty,max=255"`
	Password  *string   `json:"password"  validate:"omitempty,max=255"`
	Email     string    `json:"email"     validate:"required,email,max=255"`
	Namespace string    `json:"namespace" validate:"omitempty,max=255"`
	IsDefault bool      `json:"is_default"`
	IsActive  bool      `json:"is_active"`
	Insecure  bool      `json:"insecure"`
}

func (r UpdateRegistryHTTPRequest) ToServiceInput() service.UpdateRegistryInput {
	return service.UpdateRegistryInput{
		ID:        r.ID,
		Name:      r.Name,
		Type:      r.Type,
		URL:       r.URL,
		Username:  r.Username,
		Password:  r.Password,
		Email:     r.Email,
		Namespace: r.Namespace,
		IsDefault: r.IsDefault,
		IsActive:  r.IsActive,
		Insecure:  r.Insecure,
	}
}

type UpdateRegistryHTTPResponse struct {
	ID             uuid.UUID `json:"id"`
	Name           string    `json:"name"`
	Type           string    `json:"type"`
	URL            string    `json:"url"`
	Username       string    `json:"username,omitempty"`
	Email          string    `json:"email,omitempty"`
	Namespace      string    `json:"namespace,omitempty"`
	IsDefault      bool      `json:"is_default"`
	IsActive       bool      `json:"is_active"`
	Insecure       bool      `json:"insecure"`
	HasCredentials bool      `json:"has_credentials"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

func FromUpdateRegistryOutput(o *service.UpdateRegistryOutput) UpdateRegistryHTTPResponse {
	return UpdateRegistryHTTPResponse{
		ID:             o.ID,
		Name:           o.Name,
		Type:           o.Type,
		URL:            o.URL,
		Username:       o.Username,
		Email:          o.Email,
		Namespace:      o.Namespace,
		IsDefault:      o.IsDefault,
		IsActive:       o.IsActive,
		Insecure:       o.Insecure,
		HasCredentials: o.HasCredentials,
		CreatedAt:      o.CreatedAt,
		UpdatedAt:      o.UpdatedAt,
	}
}

// ───────────────────────── Delete (soft) ─────────────────────────

type DeleteRegistryHTTPRequest struct {
	ID uuid.UUID `json:"id"`
}

type DeleteRegistryHTTPResponse struct {
	ID        uuid.UUID `json:"id"`
	Status    string    `json:"status"` // "DELETED"
	DeletedAt time.Time `json:"deleted_at"`
}

func FromDeleteRegistryOutput(o *service.DeleteRegistryOutput) DeleteRegistryHTTPResponse {
	return DeleteRegistryHTTPResponse{
		ID:        o.ID,
		Status:    "DELETED",
		DeletedAt: o.DeletedAt,
	}
}

// ───────────────────────── Connect (test) ─────────────────────────

type ConnectRegistryHTTPRequest struct {
	ID uuid.UUID `json:"id"`
}

type ConnectRegistryHTTPResponse struct {
	ID            uuid.UUID `json:"id"`
	Connected     bool      `json:"connected"`
	Authenticated bool      `json:"authenticated"`
	Status        string    `json:"status"` // OK | AUTH_FAILED | UNREACHABLE | TLS_ERROR | ERROR
	Message       string    `json:"message"`
	APIVersion    string    `json:"api_version,omitempty"`
	IsActive      bool      `json:"is_active"`
	CheckedAt     time.Time `json:"checked_at"`
}

func FromConnectRegistryOutput(o *service.ConnectRegistryOutput) ConnectRegistryHTTPResponse {
	return ConnectRegistryHTTPResponse{
		ID:            o.ID,
		Connected:     o.Connected,
		Authenticated: o.Authenticated,
		Status:        o.Status,
		Message:       o.Message,
		APIVersion:    o.APIVersion,
		IsActive:      o.IsActive,
		CheckedAt:     o.CheckedAt,
	}
}

// ───────────────────────── Ping (test by params) ─────────────────────────

// PingRegistryHTTPRequest — health-check сохранённого registry по id.
type PingRegistryHTTPRequest struct {
	ID uuid.UUID `json:"id"`
}

type PingRegistryHTTPResponse struct {
	ID            uuid.UUID `json:"id"`
	Connected     bool      `json:"connected"`
	Authenticated bool      `json:"authenticated"`
	Status        string    `json:"status"` // OK | AUTH_FAILED | UNREACHABLE | TLS_ERROR | ERROR
	Message       string    `json:"message"`
	APIVersion    string    `json:"api_version,omitempty"`
	IsActive      bool      `json:"is_active"`
	CheckedAt     time.Time `json:"checked_at"`
}

func FromPingRegistryOutput(o *service.PingRegistryOutput) PingRegistryHTTPResponse {
	return PingRegistryHTTPResponse{
		ID:            o.ID,
		Connected:     o.Connected,
		Authenticated: o.Authenticated,
		Status:        o.Status,
		Message:       o.Message,
		APIVersion:    o.APIVersion,
		IsActive:      o.IsActive,
		CheckedAt:     o.CheckedAt,
	}
}

// ───────────────────────── Images ─────────────────────────

type ListRegistryImagesHTTPRequest struct {
	ID        uuid.UUID `json:"id"`
	Namespace string    `json:"namespace"`
}

type RegistryImageResponse struct {
	Name        string   `json:"name"`
	Tags        []string `json:"tags"`
	TagCount    int      `json:"tag_count"`
	Description string   `json:"description,omitempty"`
	IsPrivate   *bool    `json:"is_private,omitempty"`
	PullCount   *int64   `json:"pull_count,omitempty"`
	StarCount   *int64   `json:"star_count,omitempty"`
	LastUpdated string   `json:"last_updated,omitempty"`
}

type ListRegistryImagesHTTPResponse struct {
	RegistryID uuid.UUID               `json:"registry_id"`
	Type       string                  `json:"type"`
	Source     string                  `json:"source"` // hub_api | registry_v2
	Namespace  string                  `json:"namespace,omitempty"`
	Total      int                     `json:"total"`
	Count      int                     `json:"count"`
	Images     []RegistryImageResponse `json:"images"`
}

func FromListRegistryImagesOutput(o *service.ListRegistryImagesOutput) ListRegistryImagesHTTPResponse {
	images := make([]RegistryImageResponse, 0, len(o.Images))
	for _, img := range o.Images {
		images = append(images, RegistryImageResponse{
			Name:        img.Name,
			Tags:        img.Tags,
			TagCount:    img.TagCount,
			Description: img.Description,
			IsPrivate:   img.IsPrivate,
			PullCount:   img.PullCount,
			StarCount:   img.StarCount,
			LastUpdated: img.LastUpdated,
		})
	}
	return ListRegistryImagesHTTPResponse{
		RegistryID: o.RegistryID,
		Type:       o.Type,
		Source:     o.Source,
		Namespace:  o.Namespace,
		Total:      o.Total,
		Count:      len(images),
		Images:     images,
	}
}

// ───────────────────────── List ─────────────────────────

type RegistryListItemResponse struct {
	ID             uuid.UUID  `json:"id"`
	Name           string     `json:"name"`
	Type           string     `json:"type"`
	URL            string     `json:"url"`
	Username       string     `json:"username,omitempty"`
	Email          string     `json:"email,omitempty"`
	Namespace      string     `json:"namespace,omitempty"`
	IsDefault      bool       `json:"is_default"`
	IsActive       bool       `json:"is_active"`
	Insecure       bool       `json:"insecure"`
	HasCredentials bool       `json:"has_credentials"`
	LastCheckedAt  *time.Time `json:"last_checked_at,omitempty"`
	LastStatus     string     `json:"last_status,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

type PaginationResponse struct {
	Page       int `json:"page"`
	PageSize   int `json:"page_size"`
	Total      int `json:"total"`
	TotalPages int `json:"total_pages"`
}

type ListRegistriesHTTPResponse struct {
	Items      []RegistryListItemResponse `json:"items"`
	Pagination PaginationResponse         `json:"pagination"`
}

func FromListRegistriesOutput(o *service.ListRegistriesOutput) ListRegistriesHTTPResponse {
	items := make([]RegistryListItemResponse, 0, len(o.Items))
	for _, it := range o.Items {
		items = append(items, RegistryListItemResponse{
			ID:             it.ID,
			Name:           it.Name,
			Type:           it.Type,
			URL:            it.URL,
			Username:       it.Username,
			Email:          it.Email,
			Namespace:      it.Namespace,
			IsDefault:      it.IsDefault,
			IsActive:       it.IsActive,
			Insecure:       it.Insecure,
			HasCredentials: it.HasCredentials,
			LastCheckedAt:  it.LastCheckedAt,
			LastStatus:     it.LastStatus,
			CreatedAt:      it.CreatedAt,
			UpdatedAt:      it.UpdatedAt,
		})
	}
	return ListRegistriesHTTPResponse{
		Items: items,
		Pagination: PaginationResponse{
			Page:       o.Pagination.Page,
			PageSize:   o.Pagination.PageSize,
			Total:      o.Pagination.Total,
			TotalPages: o.Pagination.TotalPages,
		},
	}
}
