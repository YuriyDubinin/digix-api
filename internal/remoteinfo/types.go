// Package remoteinfo собирает подробный снимок удалённого сервера через SSH:
// host / cpu / memory / disks / network / docker. Структуры специально
// повторяют JSON-контракт пакета sysinfo (для /api/system/main), чтобы
// фронтенд использовал одни и те же компоненты рендера.
//
// Принципы:
//   - всё работает по best-effort: ошибка одной секции пишется в Errors,
//     остальные секции отдаются как есть;
//   - команды читаются из /proc и /etc, плюс `ip -j addr` для интерфейсов
//     и `df -B1 -P` для дисков; для гипервизора — `systemd-detect-virt`;
//   - размеры — всегда в байтах, времена — UTC time.Time, длительности — секунды;
//   - имена JSON-полей — snake_case, единообразно с sysinfo.
package remoteinfo

import "time"

// RemoteSystemInfo — снимок состояния удалённого сервера.
type RemoteSystemInfo struct {
	CollectedAt          time.Time `json:"collected_at"`
	CollectionDurationMS int64     `json:"collection_duration_ms"`

	Connection ConnectionInfo `json:"connection"`
	Host       HostInfo       `json:"host"`
	CPU        CPUInfo        `json:"cpu"`
	Memory     MemoryInfo     `json:"memory"`
	Disks      DisksInfo      `json:"disks"`
	Network    NetworkInfo    `json:"network"`
	Docker     DockerVersions `json:"docker"`

	Errors []SectionError `json:"errors,omitempty"`
}

// ConnectionInfo — как и куда мы подключились. Помогает фронту показать
// «по какому каналу собран снимок».
type ConnectionInfo struct {
	Host      string `json:"host"`
	Port      int    `json:"port"`
	User      string `json:"user,omitempty"`
	Method    string `json:"method"`               // publickey | password
	LatencyMS int64  `json:"latency_ms,omitempty"` // время на TCP+SSH-handshake
}

type SectionError struct {
	Section string `json:"section"`
	Message string `json:"message"`
}

// ─────────────────────────── Host ───────────────────────────
//
// Поля повторяют sysinfo.HostInfo. Если по SSH не удалось определить какое-то
// поле — оно остаётся пустым (omitempty там же, где у sysinfo).

type HostInfo struct {
	Hostname             string    `json:"hostname"`
	FQDN                 string    `json:"fqdn,omitempty"`
	PrimaryIP            string    `json:"primary_ip,omitempty"`
	PublicIP             string    `json:"public_ip,omitempty"`
	CountryCode          string    `json:"country_code,omitempty"`
	Country              string    `json:"country,omitempty"`
	OS                   string    `json:"os"`               // "linux"
	Platform             string    `json:"platform"`         // "ubuntu"
	PlatformFamily       string    `json:"platform_family"`  // "debian"
	PlatformVersion      string    `json:"platform_version"` // "22.04"
	KernelVersion        string    `json:"kernel_version"`
	KernelArch           string    `json:"kernel_arch"`
	VirtualizationSystem string    `json:"virtualization_system,omitempty"`
	VirtualizationRole   string    `json:"virtualization_role,omitempty"`
	BootTime             time.Time `json:"boot_time"`
	UptimeSeconds        uint64    `json:"uptime_seconds"`
	HostID               string    `json:"host_id,omitempty"`
	Timezone             string    `json:"timezone"`
}

// ─────────────────────────── CPU ───────────────────────────

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

// ─────────────────────────── Memory ───────────────────────────

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

// ─────────────────────────── Disks ───────────────────────────

type DisksInfo struct {
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

// ─────────────────────────── Network ───────────────────────────

type NetworkInfo struct {
	Interfaces       []NetInterface  `json:"interfaces"`
	IOCounters       []NetIOCounters `json:"io_counters,omitempty"`
	ConnectionsCount int             `json:"connections_count"`
}

type NetInterface struct {
	Name         string    `json:"name"`
	HardwareAddr string    `json:"hardware_addr,omitempty"`
	MTU          int       `json:"mtu"`
	Flags        []string  `json:"flags,omitempty"`
	Addresses    []NetAddr `json:"addresses,omitempty"`
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

// ─────────────────────────── Docker ───────────────────────────

type DockerVersions struct {
	Engine    string `json:"engine,omitempty"`
	EngineAPI string `json:"engine_api,omitempty"`
	Compose   string `json:"compose,omitempty"`
}
