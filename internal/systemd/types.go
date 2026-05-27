// Package systemd собирает список системных сервисов (systemd unit'ов типа
// .service) через D-Bus API systemd для отображения в админ-консоли.
//
// ВАЖНО: работает только на Linux с systemd. Внутри Alpine-контейнера systemd
// отсутствует (там OpenRC), поэтому сервис общается с systemd ХОСТА — для этого
// в контейнер нужно примонтировать сокет systemd (см. README/.env).
//
// Все размеры — в байтах, времена — ISO 8601 UTC, длительности — в секундах.
// Работает по best-effort: если systemd недоступен — Available=false + Reason.
package systemd

import (
	"context"
	"time"
)

// Collector собирает данные о сервисах. Создаётся один раз в main.go.
// Конкретная реализация сбора — в collect_linux.go / collect_other.go.
type Collector struct{}

func NewCollector() *Collector {
	return &Collector{}
}

// Collect возвращает снимок состояния сервисов. Никогда не возвращает ошибку:
// недоступность systemd выражается через ServicesInfo.Available=false.
func (c *Collector) Collect(ctx context.Context) *ServicesInfo {
	out := &ServicesInfo{
		CollectedAt: time.Now().UTC(),
		Services:    []Service{},
	}
	c.gather(ctx, out) // платформозависимая реализация
	return out
}

// ───────────────────────── Выходные (публичные) типы ─────────────────────────

// ServicesInfo — корневой ответ /api/services.
type ServicesInfo struct {
	Available   bool         `json:"available"`
	Reason      string       `json:"reason,omitempty"`
	CollectedAt time.Time    `json:"collected_at"`
	Manager     *ManagerInfo `json:"manager,omitempty"`
	Count       int          `json:"count"`
	Services    []Service    `json:"services"`
	Errors      []string     `json:"errors,omitempty"`
}

// ManagerInfo — сведения о самом systemd.
type ManagerInfo struct {
	Version      string `json:"version"`
	Architecture string `json:"architecture,omitempty"`
	FailedUnits  int    `json:"failed_units"`
	TotalNames   int    `json:"total_names,omitempty"`
}

// Service — подробные данные об одном сервисе (.service unit).
type Service struct {
	Name          string `json:"name"`        // "nginx.service"
	Description   string `json:"description"`
	LoadState     string `json:"load_state"`  // loaded | not-found | masked | error
	ActiveState   string `json:"active_state"` // active | inactive | failed | activating | deactivating
	SubState      string `json:"sub_state"`    // running | dead | exited | failed | start | ...
	UnitFileState string `json:"unit_file_state,omitempty"` // enabled | disabled | static | masked | ...
	Type          string `json:"type,omitempty"`            // simple | forking | oneshot | notify | dbus
	Enabled       bool   `json:"enabled"`                   // производное от unit_file_state

	MainPID int `json:"main_pid,omitempty"`

	MemoryCurrentBytes int64 `json:"memory_current_bytes,omitempty"` // -1 = не учитывается
	MemoryPeakBytes    int64 `json:"memory_peak_bytes,omitempty"`
	CPUUsageNSec       int64 `json:"cpu_usage_nsec,omitempty"`
	TasksCurrent       int64 `json:"tasks_current,omitempty"`
	TasksMax           int64 `json:"tasks_max,omitempty"` // -1 = без лимита (infinity)

	NRestarts int    `json:"n_restarts"`
	Result    string `json:"result,omitempty"` // success | exit-code | signal | timeout | ...

	ActiveEnterAt   *time.Time `json:"active_enter_at,omitempty"`
	ExecMainStartAt *time.Time `json:"exec_main_start_at,omitempty"`
	UptimeSeconds   float64    `json:"uptime_seconds,omitempty"`

	FragmentPath string `json:"fragment_path,omitempty"` // путь к .service-файлу
	User         string `json:"user,omitempty"`
	Group        string `json:"group,omitempty"`
}
