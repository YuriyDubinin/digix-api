// Package docker общается с Docker Engine API напрямую по unix-сокету через
// stdlib net/http (без тяжёлого docker/docker SDK). Отдаёт максимально подробный
// список контейнеров для админ-консоли (вкладка Containers).
//
// Все размеры — в байтах, времена — ISO 8601 UTC. Работает по best-effort:
// если демон недоступен — Available=false + Reason, без падения.
package docker

import "time"

// ───────────────────────── Выходные (публичные) типы ─────────────────────────

// ContainersInfo — корневой ответ /api/containers.
type ContainersInfo struct {
	Available   bool        `json:"available"`
	Reason      string      `json:"reason,omitempty"`
	CollectedAt time.Time   `json:"collected_at"`
	Engine      *EngineInfo `json:"engine,omitempty"`
	Count       int         `json:"count"`
	Containers  []Container `json:"containers"`
	// Errors — частичные ошибки (например, отдельный inspect не удался).
	Errors []string `json:"errors,omitempty"`
}

// EngineInfo — сведения о самом демоне Docker.
type EngineInfo struct {
	Version           string `json:"version"`
	APIVersion        string `json:"api_version"`
	GitCommit         string `json:"git_commit,omitempty"`
	GoVersion         string `json:"go_version,omitempty"`
	Name              string `json:"name"`            // hostname демона
	ID                string `json:"id,omitempty"`
	OperatingSystem   string `json:"operating_system"`
	OSType            string `json:"os_type"`
	Architecture      string `json:"architecture"`
	KernelVersion     string `json:"kernel_version"`
	StorageDriver     string `json:"storage_driver"`
	CgroupVersion     string `json:"cgroup_version,omitempty"`
	MemoryTotalBytes  int64  `json:"memory_total_bytes"`
	NCPU              int    `json:"ncpu"`
	ContainersTotal   int    `json:"containers_total"`
	ContainersRunning int    `json:"containers_running"`
	ContainersPaused  int    `json:"containers_paused"`
	ContainersStopped int    `json:"containers_stopped"`
	ImagesTotal       int    `json:"images_total"`
}

// Container — подробные данные о контейнере (list + inspect объединены).
type Container struct {
	ID              string            `json:"id"`
	ShortID         string            `json:"short_id"`
	Name            string            `json:"name"`
	Image           string            `json:"image"`
	ImageID         string            `json:"image_id"`
	Command         string            `json:"command,omitempty"`
	CreatedAt       time.Time         `json:"created_at"`
	State           string            `json:"state"`  // running|exited|paused|created|...
	Status          string            `json:"status"` // "Up 3 hours", "Exited (0) 2 days ago"
	Health          string            `json:"health,omitempty"`
	HealthFailing   int               `json:"health_failing_streak,omitempty"`
	Running         bool              `json:"running"`
	Paused          bool              `json:"paused"`
	Restarting      bool              `json:"restarting"`
	Dead            bool              `json:"dead"`
	OOMKilled       bool              `json:"oom_killed"`
	ExitCode        int               `json:"exit_code"`
	PID             int               `json:"pid,omitempty"`
	RestartCount    int               `json:"restart_count"`
	StartedAt       *time.Time        `json:"started_at,omitempty"`
	FinishedAt      *time.Time        `json:"finished_at,omitempty"`
	Platform        string            `json:"platform,omitempty"`
	LogPath         string            `json:"log_path,omitempty"`
	RestartPolicy   string            `json:"restart_policy,omitempty"`
	NetworkMode     string            `json:"network_mode,omitempty"`
	Privileged      bool              `json:"privileged"`
	User            string            `json:"user,omitempty"`
	WorkingDir      string            `json:"working_dir,omitempty"`
	Entrypoint      []string          `json:"entrypoint,omitempty"`
	Cmd             []string          `json:"cmd,omitempty"`
	Env             []string          `json:"env,omitempty"` // ВНИМАНИЕ: может содержать секреты
	Labels          map[string]string `json:"labels,omitempty"`
	Ports           []Port            `json:"ports,omitempty"`
	Mounts          []Mount           `json:"mounts,omitempty"`
	Networks        []Network         `json:"networks,omitempty"`
	Limits          ResourceLimits    `json:"limits"`
	SizeRwBytes     int64             `json:"size_rw_bytes,omitempty"`
	SizeRootFsBytes int64             `json:"size_root_fs_bytes,omitempty"`
}

type Port struct {
	IP          string `json:"ip,omitempty"`
	PrivatePort int    `json:"private_port"`
	PublicPort  int    `json:"public_port,omitempty"`
	Type        string `json:"type"` // tcp|udp
}

type Mount struct {
	Type        string `json:"type"` // volume|bind|tmpfs
	Name        string `json:"name,omitempty"`
	Source      string `json:"source"`
	Destination string `json:"destination"`
	Mode        string `json:"mode,omitempty"`
	RW          bool   `json:"rw"`
}

type Network struct {
	Name       string `json:"name"`
	IPAddress  string `json:"ip_address,omitempty"`
	Gateway    string `json:"gateway,omitempty"`
	MacAddress string `json:"mac_address,omitempty"`
	NetworkID  string `json:"network_id,omitempty"`
}

type ResourceLimits struct {
	MemoryBytes int64 `json:"memory_bytes"` // 0 = без лимита
	NanoCPUs    int64 `json:"nano_cpus"`    // 0 = без лимита; /1e9 = число CPU
	CPUShares   int64 `json:"cpu_shares"`
}

// ───────────────────── Internal-типы парсинга Docker API ─────────────────────
// Описываем только те поля, что нам нужны. Имена JSON — как в Engine API (PascalCase).

type apiVersion struct {
	Version       string `json:"Version"`
	APIVersion    string `json:"ApiVersion"`
	Os            string `json:"Os"`
	Arch          string `json:"Arch"`
	KernelVersion string `json:"KernelVersion"`
	GoVersion     string `json:"GoVersion"`
	GitCommit     string `json:"GitCommit"`
}

type apiInfo struct {
	ID                string `json:"ID"`
	Containers        int    `json:"Containers"`
	ContainersRunning int    `json:"ContainersRunning"`
	ContainersPaused  int    `json:"ContainersPaused"`
	ContainersStopped int    `json:"ContainersStopped"`
	Images            int    `json:"Images"`
	Driver            string `json:"Driver"`
	MemTotal          int64  `json:"MemTotal"`
	NCPU              int    `json:"NCPU"`
	Name              string `json:"Name"`
	ServerVersion     string `json:"ServerVersion"`
	OperatingSystem   string `json:"OperatingSystem"`
	OSType            string `json:"OSType"`
	Architecture      string `json:"Architecture"`
	KernelVersion     string `json:"KernelVersion"`
	CgroupVersion     string `json:"CgroupVersion"`
}

type apiListItem struct {
	ID         string            `json:"Id"`
	Names      []string          `json:"Names"`
	Image      string            `json:"Image"`
	ImageID    string            `json:"ImageID"`
	Command    string            `json:"Command"`
	Created    int64             `json:"Created"`
	Ports      []apiPort         `json:"Ports"`
	SizeRw     int64             `json:"SizeRw"`
	SizeRootFs int64             `json:"SizeRootFs"`
	Labels     map[string]string `json:"Labels"`
	State      string            `json:"State"`
	Status     string            `json:"Status"`
}

type apiPort struct {
	IP          string `json:"IP"`
	PrivatePort int    `json:"PrivatePort"`
	PublicPort  int    `json:"PublicPort"`
	Type        string `json:"Type"`
}

type apiInspect struct {
	ID              string             `json:"Id"`
	Created         string             `json:"Created"`
	Path            string             `json:"Path"`
	Args            []string           `json:"Args"`
	Name            string             `json:"Name"`
	Image           string             `json:"Image"`
	RestartCount    int                `json:"RestartCount"`
	Platform        string             `json:"Platform"`
	LogPath         string             `json:"LogPath"`
	State           apiState           `json:"State"`
	HostConfig      apiHostConfig      `json:"HostConfig"`
	Config          apiConfig          `json:"Config"`
	NetworkSettings apiNetworkSettings `json:"NetworkSettings"`
	Mounts          []apiMount         `json:"Mounts"`
}

type apiState struct {
	Status     string     `json:"Status"`
	Running    bool       `json:"Running"`
	Paused     bool       `json:"Paused"`
	Restarting bool       `json:"Restarting"`
	OOMKilled  bool       `json:"OOMKilled"`
	Dead       bool       `json:"Dead"`
	Pid        int        `json:"Pid"`
	ExitCode   int        `json:"ExitCode"`
	StartedAt  string     `json:"StartedAt"`
	FinishedAt string     `json:"FinishedAt"`
	Health     *apiHealth `json:"Health"`
}

type apiHealth struct {
	Status        string `json:"Status"`
	FailingStreak int    `json:"FailingStreak"`
}

type apiHostConfig struct {
	RestartPolicy struct {
		Name              string `json:"Name"`
		MaximumRetryCount int    `json:"MaximumRetryCount"`
	} `json:"RestartPolicy"`
	Memory      int64  `json:"Memory"`
	NanoCpus    int64  `json:"NanoCpus"`
	CPUShares   int64  `json:"CpuShares"`
	NetworkMode string `json:"NetworkMode"`
	Privileged  bool   `json:"Privileged"`
}

type apiConfig struct {
	Hostname   string            `json:"Hostname"`
	User       string            `json:"User"`
	Env        []string          `json:"Env"`
	Cmd        []string          `json:"Cmd"`
	Entrypoint []string          `json:"Entrypoint"`
	Image      string            `json:"Image"`
	WorkingDir string            `json:"WorkingDir"`
	Labels     map[string]string `json:"Labels"`
}

type apiNetworkSettings struct {
	Networks map[string]apiEndpoint `json:"Networks"`
}

type apiEndpoint struct {
	IPAddress  string `json:"IPAddress"`
	Gateway    string `json:"Gateway"`
	MacAddress string `json:"MacAddress"`
	NetworkID  string `json:"NetworkID"`
}

type apiMount struct {
	Type        string `json:"Type"`
	Name        string `json:"Name"`
	Source      string `json:"Source"`
	Destination string `json:"Destination"`
	Mode        string `json:"Mode"`
	RW          bool   `json:"RW"`
}
