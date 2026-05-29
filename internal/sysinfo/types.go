// Package sysinfo собирает максимально подробную информацию о машине, на которой
// запущен сервис: ОС, железо, сеть, диски, процесс, Go-runtime, состояние БД.
// Используется защищённым эндпоинтом /api/system для отрисовки в админ-консоли.
//
// Принципы:
//   - все сборщики работают по best-effort: ошибка одной секции не валит ответ,
//     попадает в SystemInfo.Errors;
//   - размеры — всегда в байтах (форматирование на стороне клиента);
//   - времена — ISO 8601 через time.Time, длительности — секунды для людей,
//     наносекунды для высокоточных метрик (GC pause и т.п.);
//   - имена полей JSON — snake_case, единообразно.
package sysinfo

import "time"

// SystemInfo — агрегированный снимок состояния машины.
type SystemInfo struct {
	CollectedAt          time.Time `json:"collected_at"`
	CollectionDurationMS int64     `json:"collection_duration_ms"`

	App      AppInfo        `json:"app"`
	Host     HostInfo       `json:"host"`
	CPU      CPUInfo        `json:"cpu"`
	Memory   MemoryInfo     `json:"memory"`
	Disks    DisksInfo      `json:"disks"`
	Network  NetworkInfo    `json:"network"`
	Process  ProcessInfo    `json:"process"`
	Database DatabaseInfo   `json:"database"`
	Docker   DockerVersions `json:"docker"`

	// Errors — секции, которые не удалось собрать (нет прав, не поддерживается ОС
	// и т.п.). Каждая запись помечена путём (host.virtualization,
	// disks.io_counters и т.п.) и текстом ошибки.
	Errors []SectionError `json:"errors,omitempty"`
}

type SectionError struct {
	Section string `json:"section"`
	Message string `json:"message"`
}

// ───────────────────────────── App ─────────────────────────────

type AppInfo struct {
	Name          string    `json:"name"`
	Env           string    `json:"env"`
	Version       string    `json:"version"`
	StartedAt     time.Time `json:"started_at"`
	UptimeSeconds float64   `json:"uptime_seconds"`
	HTTPPort      string    `json:"http_port"`
}

// ───────────────────────────── Host ─────────────────────────────

type HostInfo struct {
	Hostname             string    `json:"hostname"`
	FQDN                 string    `json:"fqdn,omitempty"`
	PrimaryIP            string    `json:"primary_ip,omitempty"` // основной исходящий IP машины/контейнера
	PublicIP             string    `json:"public_ip,omitempty"`  // публичный (внешний) IP сервера
	CountryCode          string    `json:"country_code,omitempty"` // ISO 3166-1 alpha-2 по public_ip
	Country              string    `json:"country,omitempty"`      // английское имя страны
	OS                   string    `json:"os"`               // "linux", "darwin", "windows"
	Platform             string    `json:"platform"`         // "ubuntu", "macOS"
	PlatformFamily       string    `json:"platform_family"`  // "debian"
	PlatformVersion      string    `json:"platform_version"` // "22.04"
	KernelVersion        string    `json:"kernel_version"`
	KernelArch           string    `json:"kernel_arch"` // "x86_64"
	VirtualizationSystem string    `json:"virtualization_system,omitempty"`
	VirtualizationRole   string    `json:"virtualization_role,omitempty"` // "guest"|"host"
	BootTime             time.Time `json:"boot_time"`
	UptimeSeconds        uint64    `json:"uptime_seconds"`
	HostID               string    `json:"host_id,omitempty"`
	Timezone             string    `json:"timezone"`
}

// ───────────────────────────── CPU ─────────────────────────────

type CPUInfo struct {
	ModelName           string    `json:"model_name"`
	Vendor              string    `json:"vendor"`
	Family              string    `json:"family"`
	Model               string    `json:"model"`
	Stepping            int32     `json:"stepping"`
	PhysicalCores       int       `json:"physical_cores"`
	LogicalCores        int       `json:"logical_cores"`
	MHz                 float64   `json:"mhz"`
	CacheSizeKB         int32     `json:"cache_size_kb"`
	Flags               []string  `json:"flags,omitempty"`
	UsagePercent        float64   `json:"usage_percent"`
	PerCoreUsagePercent []float64 `json:"per_core_usage_percent"`
	LoadAvg1            float64   `json:"load_avg_1"`
	LoadAvg5            float64   `json:"load_avg_5"`
	LoadAvg15           float64   `json:"load_avg_15"`
}

// ───────────────────────────── Memory ─────────────────────────────

type MemoryInfo struct {
	Virtual VirtualMemory `json:"virtual"`
	Swap    SwapMemory    `json:"swap"`
}

type VirtualMemory struct {
	TotalBytes     uint64  `json:"total_bytes"`
	AvailableBytes uint64  `json:"available_bytes"`
	UsedBytes      uint64  `json:"used_bytes"`
	FreeBytes      uint64  `json:"free_bytes"`
	CachedBytes    uint64  `json:"cached_bytes"`
	BuffersBytes   uint64  `json:"buffers_bytes"`
	SharedBytes    uint64  `json:"shared_bytes"`
	SlabBytes      uint64  `json:"slab_bytes"`
	UsedPercent    float64 `json:"used_percent"`
}

type SwapMemory struct {
	TotalBytes  uint64  `json:"total_bytes"`
	UsedBytes   uint64  `json:"used_bytes"`
	FreeBytes   uint64  `json:"free_bytes"`
	UsedPercent float64 `json:"used_percent"`
}

// ───────────────────────────── Disks ─────────────────────────────

type DisksInfo struct {
	// Usage — сводка по физическому диску сервера (корневая ФС "/").
	// Отвечает на «сколько всего места и сколько свободно». Внутри контейнера
	// "/" — это overlay поверх физического диска хоста, поэтому значения
	// отражают реальный диск сервера, а не RAM.
	Usage      DiskUsageSummary          `json:"usage"`
	Partitions []DiskPartition           `json:"partitions"`
	IOCounters map[string]DiskIOCounters `json:"io_counters,omitempty"`
}

type DiskUsageSummary struct {
	Path        string  `json:"path"`
	Fstype      string  `json:"fstype,omitempty"`
	TotalBytes  uint64  `json:"total_bytes"`
	UsedBytes   uint64  `json:"used_bytes"`
	FreeBytes   uint64  `json:"free_bytes"`
	UsedPercent float64 `json:"used_percent"`
	InodesTotal uint64  `json:"inodes_total"`
	InodesUsed  uint64  `json:"inodes_used"`
	InodesFree  uint64  `json:"inodes_free"`
}

type DiskPartition struct {
	Device      string  `json:"device"`
	Mountpoint  string  `json:"mountpoint"`
	Fstype      string  `json:"fstype"`
	Opts        string  `json:"opts,omitempty"`
	TotalBytes  uint64  `json:"total_bytes"`
	UsedBytes   uint64  `json:"used_bytes"`
	FreeBytes   uint64  `json:"free_bytes"`
	UsedPercent float64 `json:"used_percent"`
	InodesTotal uint64  `json:"inodes_total"`
	InodesUsed  uint64  `json:"inodes_used"`
	InodesFree  uint64  `json:"inodes_free"`
}

type DiskIOCounters struct {
	ReadCount  uint64 `json:"read_count"`
	WriteCount uint64 `json:"write_count"`
	ReadBytes  uint64 `json:"read_bytes"`
	WriteBytes uint64 `json:"write_bytes"`
	ReadTime   uint64 `json:"read_time_ms"`
	WriteTime  uint64 `json:"write_time_ms"`
	IoTime     uint64 `json:"io_time_ms"`
}

// ───────────────────────────── Network ─────────────────────────────

type NetworkInfo struct {
	Interfaces       []NetInterface  `json:"interfaces"`
	IOCounters       []NetIOCounters `json:"io_counters,omitempty"`
	ConnectionsCount int             `json:"connections_count"`
}

type NetInterface struct {
	Name         string      `json:"name"`
	HardwareAddr string      `json:"hardware_addr,omitempty"`
	MTU          int         `json:"mtu"`
	Flags        []string    `json:"flags,omitempty"`
	Addresses    []NetAddr   `json:"addresses,omitempty"`
}

type NetAddr struct {
	Addr   string `json:"addr"`
	Family string `json:"family"` // "ipv4" | "ipv6"
}

type NetIOCounters struct {
	Name        string `json:"name"`
	BytesSent   uint64 `json:"bytes_sent"`
	BytesRecv   uint64 `json:"bytes_recv"`
	PacketsSent uint64 `json:"packets_sent"`
	PacketsRecv uint64 `json:"packets_recv"`
	ErrIn       uint64 `json:"err_in"`
	ErrOut      uint64 `json:"err_out"`
	DropIn      uint64 `json:"drop_in"`
	DropOut     uint64 `json:"drop_out"`
}

// ───────────────────────────── Process ─────────────────────────────

type ProcessInfo struct {
	PID            int32     `json:"pid"`
	PPID           int32     `json:"ppid"`
	Name           string    `json:"name"`
	Exe            string    `json:"exe,omitempty"`
	Cmdline        string    `json:"cmdline,omitempty"`
	Cwd            string    `json:"cwd,omitempty"`
	Username       string    `json:"username,omitempty"`
	StartedAt      time.Time `json:"started_at"`
	UptimeSeconds  float64   `json:"uptime_seconds"`
	MemoryRSSBytes uint64    `json:"memory_rss_bytes"`
	MemoryVMSBytes uint64    `json:"memory_vms_bytes"`
	MemoryPercent  float32   `json:"memory_percent"`
	CPUPercent     float64   `json:"cpu_percent"`
	NumThreads     int32     `json:"num_threads"`
	NumFDs         int32     `json:"num_fds,omitempty"`
	Nice           int32     `json:"nice"`
}

// ───────────────────────────── Docker ─────────────────────────────

// DockerVersions — версии Docker-стека на сервере. Пустые поля = недоступно.
//
//   - Engine / EngineAPI — версия демона (Docker Engine API).
//   - Compose — версия плагина docker compose. Берётся через `docker compose
//     version --short`, требует наличия Docker CLI в контейнере. Если CLI
//     отсутствует — поле пустое.
type DockerVersions struct {
	Engine    string `json:"engine,omitempty"`
	EngineAPI string `json:"engine_api,omitempty"`
	Compose   string `json:"compose,omitempty"`
}

// ───────────────────────────── Database ─────────────────────────────

type DatabaseInfo struct {
	Reachable     bool         `json:"reachable"`
	PingLatencyMS float64      `json:"ping_latency_ms"`
	Version       string       `json:"version,omitempty"`
	Pool          DBPoolStats  `json:"pool"`
	Server        DBServerInfo `json:"server"`
}

type DBPoolStats struct {
	MaxConns               int32 `json:"max_conns"`
	TotalConns             int32 `json:"total_conns"`
	IdleConns              int32 `json:"idle_conns"`
	AcquiredConns          int32 `json:"acquired_conns"`
	ConstructingConns      int32 `json:"constructing_conns"`
	AcquireCount           int64 `json:"acquire_count"`
	AcquireDurationNs      int64 `json:"acquire_duration_ns"`
	EmptyAcquireCount      int64 `json:"empty_acquire_count"`
	CanceledAcquireCount   int64 `json:"canceled_acquire_count"`
}

type DBServerInfo struct {
	CurrentDatabase    string `json:"current_database,omitempty"`
	DatabaseSizeBytes  int64  `json:"database_size_bytes"`
	ActiveConnections  int    `json:"active_connections"`
	MaxConnections     int    `json:"max_connections"`
	ServerStartedAt    string `json:"server_started_at,omitempty"`
}
