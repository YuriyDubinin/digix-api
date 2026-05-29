package dto

import (
	"time"

	"github.com/google/uuid"

	"github.com/YuriyDubinin/dijex-api/internal/docker"
	"github.com/YuriyDubinin/dijex-api/internal/remoteinfo"
	"github.com/YuriyDubinin/dijex-api/internal/service"
	"github.com/YuriyDubinin/dijex-api/internal/systemd"
)

// ───────────────────────── Create ─────────────────────────

type CreateServerHTTPRequest struct {
	Name                 string   `json:"name"      validate:"required,min=2,max=100"`
	Host                 string   `json:"host"      validate:"required,max=255"`
	Port                 int      `json:"port"      validate:"omitempty,min=1,max=65535"`
	Protocol             string   `json:"protocol"  validate:"omitempty,oneof=SSH WINRM RDP"`
	Username             string   `json:"username"  validate:"omitempty,max=255"`
	AuthMethod           string   `json:"auth_method" validate:"omitempty,oneof=PASSWORD PRIVATE_KEY AGENT"`
	Password             string   `json:"password"`
	PrivateKey           string   `json:"private_key"`
	PrivateKeyPassphrase string   `json:"private_key_passphrase"`
	Description          string   `json:"description"`
	Environment          string   `json:"environment" validate:"omitempty,oneof=PRODUCTION STAGING DEVELOPMENT TESTING OTHER"`
	Provider             string   `json:"provider"  validate:"omitempty,max=100"`
	Location             string   `json:"location"  validate:"omitempty,max=100"`
	Tags                 []string `json:"tags"`
}

func (r CreateServerHTTPRequest) ToServiceInput() service.CreateServerInput {
	return service.CreateServerInput{
		Name:                 r.Name,
		Host:                 r.Host,
		Port:                 r.Port,
		Protocol:             r.Protocol,
		Username:             r.Username,
		AuthMethod:           r.AuthMethod,
		Password:             r.Password,
		PrivateKey:           r.PrivateKey,
		PrivateKeyPassphrase: r.PrivateKeyPassphrase,
		Description:          r.Description,
		Environment:          r.Environment,
		Provider:             r.Provider,
		Location:             r.Location,
		Tags:                 r.Tags,
	}
}

// ───────────────────────── Update ─────────────────────────

type UpdateServerHTTPRequest struct {
	ID                   uuid.UUID `json:"id"`
	Name                 string    `json:"name"      validate:"required,min=2,max=100"`
	Host                 string    `json:"host"      validate:"required,max=255"`
	Port                 int       `json:"port"      validate:"omitempty,min=1,max=65535"`
	Protocol             string    `json:"protocol"  validate:"omitempty,oneof=SSH WINRM RDP"`
	Username             string    `json:"username"  validate:"omitempty,max=255"`
	AuthMethod           string    `json:"auth_method" validate:"omitempty,oneof=PASSWORD PRIVATE_KEY AGENT"`
	Password             *string   `json:"password"`
	PrivateKey           *string   `json:"private_key"`
	PrivateKeyPassphrase *string   `json:"private_key_passphrase"`
	Description          string    `json:"description"`
	Environment          string    `json:"environment" validate:"omitempty,oneof=PRODUCTION STAGING DEVELOPMENT TESTING OTHER"`
	Provider             string    `json:"provider"  validate:"omitempty,max=100"`
	Location             string    `json:"location"  validate:"omitempty,max=100"`
	Tags                 []string  `json:"tags"`
	IsActive             bool      `json:"is_active"`
}

func (r UpdateServerHTTPRequest) ToServiceInput() service.UpdateServerInput {
	return service.UpdateServerInput{
		ID:                   r.ID,
		Name:                 r.Name,
		Host:                 r.Host,
		Port:                 r.Port,
		Protocol:             r.Protocol,
		Username:             r.Username,
		AuthMethod:           r.AuthMethod,
		Password:             r.Password,
		PrivateKey:           r.PrivateKey,
		PrivateKeyPassphrase: r.PrivateKeyPassphrase,
		Description:          r.Description,
		Environment:          r.Environment,
		Provider:             r.Provider,
		Location:             r.Location,
		Tags:                 r.Tags,
		IsActive:             r.IsActive,
	}
}

// ───────────────────────── Delete ─────────────────────────

type DeleteServerHTTPRequest struct {
	ID uuid.UUID `json:"id"`
}

type DeleteServerHTTPResponse struct {
	ID        uuid.UUID `json:"id"`
	Status    string    `json:"status"` // "DELETED"
	DeletedAt time.Time `json:"deleted_at"`
}

func FromDeleteServerOutput(o *service.DeleteServerOutput) DeleteServerHTTPResponse {
	return DeleteServerHTTPResponse{ID: o.ID, Status: "DELETED", DeletedAt: o.DeletedAt}
}

// ───────────────────────── Remote connect / ping ─────────────────────────

type RemoteServerHTTPRequest struct {
	ID uuid.UUID `json:"id"`
}

type RemoteConnectHTTPResponse struct {
	ID             uuid.UUID `json:"id"`
	Connected      bool      `json:"connected"`
	Method         string    `json:"method,omitempty"` // publickey | password
	Status         string    `json:"status"`           // OK | AUTH_FAILED | UNREACHABLE | TIMEOUT | ERROR
	Message        string    `json:"message"`
	RemoteHostname string    `json:"remote_hostname,omitempty"`
	OS             string    `json:"os,omitempty"`
	KernelVersion  string    `json:"kernel_version,omitempty"`
	Arch           string    `json:"arch,omitempty"`
	CPUCores       *int      `json:"cpu_cores,omitempty"`
	RemotePublicIP string    `json:"remote_public_ip,omitempty"`
	CountryCode    string    `json:"country_code,omitempty"`
	Country        string    `json:"country,omitempty"`
	CheckedAt      time.Time `json:"checked_at"`
}

func FromRemoteConnectOutput(o *service.RemoteConnectOutput) RemoteConnectHTTPResponse {
	return RemoteConnectHTTPResponse{
		ID:             o.ID,
		Connected:      o.Connected,
		Method:         o.Method,
		Status:         o.Status,
		Message:        o.Message,
		RemoteHostname: o.RemoteHostname,
		OS:             o.OS,
		KernelVersion:  o.KernelVersion,
		Arch:           o.Arch,
		CPUCores:       o.CPUCores,
		RemotePublicIP: o.RemotePublicIP,
		CountryCode:    o.CountryCode,
		Country:        o.Country,
		CheckedAt:      o.CheckedAt,
	}
}

type RemotePingHTTPResponse struct {
	ID        uuid.UUID `json:"id"`
	Connected bool      `json:"connected"`
	Method    string    `json:"method,omitempty"`
	Status    string    `json:"status"`
	Message   string    `json:"message"`
	IsActive  bool      `json:"is_active"`
	CheckedAt time.Time `json:"checked_at"`
}

func FromRemotePingOutput(o *service.RemotePingOutput) RemotePingHTTPResponse {
	return RemotePingHTTPResponse{
		ID:        o.ID,
		Connected: o.Connected,
		Method:    o.Method,
		Status:    o.Status,
		Message:   o.Message,
		IsActive:  o.IsActive,
		CheckedAt: o.CheckedAt,
	}
}

// ───────────────────────── Remote system info ─────────────────────────

// RemoteSystemInfoHTTPResponse — обёртка над снимком удалённой системы.
// Структура повторяет шаблон RemoteConnect-ответа (id/connected/status/...).
// Если SSH-коннект не удался, `system` отсутствует (omitempty), а status
// несёт причину (AUTH_FAILED, UNREACHABLE, TIMEOUT, ERROR).
//
// Сама секция `system` намеренно повторяет JSON-контракт `/api/system/main`:
// host/cpu/memory/disks/network/docker — те же поля, что и у локального хоста.
type RemoteSystemInfoHTTPResponse struct {
	ID        uuid.UUID                    `json:"id"`
	Connected bool                         `json:"connected"`
	Method    string                       `json:"method,omitempty"`
	Status    string                       `json:"status"`
	Message   string                       `json:"message"`
	CheckedAt time.Time                    `json:"checked_at"`
	System    *remoteinfo.RemoteSystemInfo `json:"system,omitempty"`
}

func FromRemoteSystemInfoOutput(o *service.RemoteSystemInfoOutput) RemoteSystemInfoHTTPResponse {
	return RemoteSystemInfoHTTPResponse{
		ID:        o.ID,
		Connected: o.Connected,
		Method:    o.Method,
		Status:    o.Status,
		Message:   o.Message,
		CheckedAt: o.CheckedAt,
		System:    o.System,
	}
}

// ───────────────────────── Remote containers / services ─────────────────────────

// RemoteContainersHTTPResponse — обёртка над списком контейнеров удалённого
// сервера. Поле `containers` имеет ТУ ЖЕ структуру, что отдаёт /api/system/containers
// (тип *docker.ContainersInfo), — фронт переиспользует те же компоненты.
type RemoteContainersHTTPResponse struct {
	ID         uuid.UUID              `json:"id"`
	Connected  bool                   `json:"connected"`
	Method     string                 `json:"method,omitempty"`
	Status     string                 `json:"status"`
	Message    string                 `json:"message"`
	CheckedAt  time.Time              `json:"checked_at"`
	Containers *docker.ContainersInfo `json:"containers,omitempty"`
}

func FromRemoteContainersOutput(o *service.RemoteContainersOutput) RemoteContainersHTTPResponse {
	return RemoteContainersHTTPResponse{
		ID:         o.ID,
		Connected:  o.Connected,
		Method:     o.Method,
		Status:     o.Status,
		Message:    o.Message,
		CheckedAt:  o.CheckedAt,
		Containers: o.Containers,
	}
}

// RemoteServicesHTTPResponse — обёртка над списком systemd-сервисов удалённого
// сервера. Поле `services` — тот же тип *systemd.ServicesInfo, что и у /api/system/services.
type RemoteServicesHTTPResponse struct {
	ID        uuid.UUID             `json:"id"`
	Connected bool                  `json:"connected"`
	Method    string                `json:"method,omitempty"`
	Status    string                `json:"status"`
	Message   string                `json:"message"`
	CheckedAt time.Time             `json:"checked_at"`
	Services  *systemd.ServicesInfo `json:"services,omitempty"`
}

func FromRemoteServicesOutput(o *service.RemoteServicesOutput) RemoteServicesHTTPResponse {
	return RemoteServicesHTTPResponse{
		ID:        o.ID,
		Connected: o.Connected,
		Method:    o.Method,
		Status:    o.Status,
		Message:   o.Message,
		CheckedAt: o.CheckedAt,
		Services:  o.Services,
	}
}

// ───────────────────────── Install SSH key ─────────────────────────

// InstallSSHKeyHTTPRequest — тело запроса установки нашего публичного ключа
// приложения в authorized_keys выбранного сервера.
type InstallSSHKeyHTTPRequest struct {
	ID uuid.UUID `json:"id"`
}

// InstallSSHKeyHTTPResponse — итог установки ключа. Недоступность сервера
// или неверный пароль возвращаются в полях connected/status/message, а не
// HTTP-ошибкой. ssh_key_installed отражает текущий флаг в БД (true только при
// подтверждённой работе ключа).
type InstallSSHKeyHTTPResponse struct {
	ID               uuid.UUID `json:"id"`
	Connected        bool      `json:"connected"`
	AlreadyInstalled bool      `json:"already_installed"`
	Installed        bool      `json:"installed"`
	Verified         bool      `json:"verified"`
	SSHKeyInstalled  bool      `json:"ssh_key_installed"`
	Status           string    `json:"status"` // OK | AUTH_FAILED | UNREACHABLE | TIMEOUT | ERROR
	Message          string    `json:"message"`
	CheckedAt        time.Time `json:"checked_at"`
}

func FromInstallSSHKeyOutput(o *service.InstallSSHKeyOutput) InstallSSHKeyHTTPResponse {
	return InstallSSHKeyHTTPResponse{
		ID:               o.ID,
		Connected:        o.Connected,
		AlreadyInstalled: o.AlreadyInstalled,
		Installed:        o.Installed,
		Verified:         o.Verified,
		SSHKeyInstalled:  o.SSHKeyInstalled,
		Status:           o.Status,
		Message:          o.Message,
		CheckedAt:        o.CheckedAt,
	}
}

// ───────────────────────── View / List ─────────────────────────

type ServerHTTPResponse struct {
	ID               uuid.UUID  `json:"id"`
	Name             string     `json:"name"`
	Host             string     `json:"host"`
	Port             int        `json:"port"`
	Protocol         string     `json:"protocol"`
	Username         string     `json:"username,omitempty"`
	AuthMethod       string     `json:"auth_method"`
	Description      string     `json:"description,omitempty"`
	Environment      string     `json:"environment"`
	Provider         string     `json:"provider,omitempty"`
	Location         string     `json:"location,omitempty"`
	Tags             []string   `json:"tags,omitempty"`
	OS               string     `json:"os,omitempty"`
	OSVersion        string     `json:"os_version,omitempty"`
	Arch             string     `json:"arch,omitempty"`
	KernelVersion    string     `json:"kernel_version,omitempty"`
	RemoteHostname   string     `json:"remote_hostname,omitempty"`
	CPUCores         *int       `json:"cpu_cores,omitempty"`
	MemoryTotalBytes *int64     `json:"memory_total_bytes,omitempty"`
	DiskTotalBytes   *int64     `json:"disk_total_bytes,omitempty"`
	RemotePublicIP   string     `json:"remote_public_ip,omitempty"`
	CountryCode      string     `json:"country_code,omitempty"`
	Country          string     `json:"country,omitempty"`
	HasPassword      bool       `json:"has_password"`
	HasPrivateKey    bool       `json:"has_private_key"`
	IsActive         bool       `json:"is_active"`
	SSHKeyInstalled  bool       `json:"ssh_key_installed"`
	LastCheckedAt    *time.Time `json:"last_checked_at,omitempty"`
	LastStatus       string     `json:"last_status,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

func FromServerView(v *service.ServerView) ServerHTTPResponse {
	return ServerHTTPResponse{
		ID:               v.ID,
		Name:             v.Name,
		Host:             v.Host,
		Port:             v.Port,
		Protocol:         v.Protocol,
		Username:         v.Username,
		AuthMethod:       v.AuthMethod,
		Description:      v.Description,
		Environment:      v.Environment,
		Provider:         v.Provider,
		Location:         v.Location,
		Tags:             v.Tags,
		OS:               v.OS,
		OSVersion:        v.OSVersion,
		Arch:             v.Arch,
		KernelVersion:    v.KernelVersion,
		RemoteHostname:   v.RemoteHostname,
		CPUCores:         v.CPUCores,
		MemoryTotalBytes: v.MemoryTotalBytes,
		DiskTotalBytes:   v.DiskTotalBytes,
		RemotePublicIP:   v.RemotePublicIP,
		CountryCode:      v.CountryCode,
		Country:          v.Country,
		HasPassword:      v.HasPassword,
		HasPrivateKey:    v.HasPrivateKey,
		IsActive:         v.IsActive,
		SSHKeyInstalled:  v.SSHKeyInstalled,
		LastCheckedAt:    v.LastCheckedAt,
		LastStatus:       v.LastStatus,
		CreatedAt:        v.CreatedAt,
		UpdatedAt:        v.UpdatedAt,
	}
}

type ListServersHTTPResponse struct {
	Items      []ServerHTTPResponse `json:"items"`
	Pagination PaginationResponse   `json:"pagination"`
}

func FromListServersOutput(o *service.ListServersOutput) ListServersHTTPResponse {
	items := make([]ServerHTTPResponse, 0, len(o.Items))
	for i := range o.Items {
		items = append(items, FromServerView(&o.Items[i]))
	}
	return ListServersHTTPResponse{
		Items: items,
		Pagination: PaginationResponse{
			Page:       o.Pagination.Page,
			PageSize:   o.Pagination.PageSize,
			Total:      o.Pagination.Total,
			TotalPages: o.Pagination.TotalPages,
		},
	}
}
