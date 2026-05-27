//go:build linux

package systemd

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	sd "github.com/coreos/go-systemd/v22/dbus"
	"golang.org/x/sync/errgroup"
)

// propsConcurrency — сколько unit'ов опрашиваем параллельно. Вызовы идут по
// локальному сокету и быстрые, но ограничиваем нагрузку на systemd.
const propsConcurrency = 8

// gather подключается к systemd по D-Bus и собирает .service unit'ы.
func (c *Collector) gather(ctx context.Context, out *ServicesInfo) {
	conn, err := connect(ctx)
	if err != nil {
		out.Available = false
		out.Reason = "systemd unreachable: " + err.Error()
		return
	}
	defer conn.Close()
	out.Available = true

	out.Manager = collectManager(conn)

	units, err := conn.ListUnitsContext(ctx)
	if err != nil {
		out.Errors = append(out.Errors, "list units: "+err.Error())
		return
	}

	// Оставляем только .service unit'ы.
	services := make([]sd.UnitStatus, 0, len(units))
	for _, u := range units {
		if strings.HasSuffix(u.Name, ".service") {
			services = append(services, u)
		}
	}

	result := make([]Service, len(services))
	var errMu sync.Mutex

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(propsConcurrency)

	for i := range services {
		i := i
		u := services[i]
		g.Go(func() error {
			svc := baseService(u)
			enrich(gctx, conn, &svc, func(msg string) {
				errMu.Lock()
				out.Errors = append(out.Errors, msg)
				errMu.Unlock()
			})
			result[i] = svc
			return nil
		})
	}
	_ = g.Wait()

	// Сортировка: проблемные/активные сверху, затем по имени.
	sort.SliceStable(result, func(a, b int) bool {
		ra, rb := stateRank(result[a].ActiveState), stateRank(result[b].ActiveState)
		if ra != rb {
			return ra < rb
		}
		return result[a].Name < result[b].Name
	})

	out.Services = result
	out.Count = len(result)
}

// connect пытается подключиться сначала к приватному сокету systemd
// (/run/systemd/private, нужен root), затем — к системной шине D-Bus.
func connect(ctx context.Context) (*sd.Conn, error) {
	if conn, err := sd.NewSystemdConnectionContext(ctx); err == nil {
		return conn, nil
	}
	conn, err := sd.NewSystemConnectionContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("neither private socket nor system bus reachable: %w", err)
	}
	return conn, nil
}

func collectManager(conn *sd.Conn) *ManagerInfo {
	m := &ManagerInfo{}
	if v, err := conn.GetManagerProperty("Version"); err == nil {
		m.Version = unquote(v)
	}
	if v, err := conn.GetManagerProperty("Architecture"); err == nil {
		m.Architecture = unquote(v)
	}
	if v, err := conn.GetManagerProperty("NFailedUnits"); err == nil {
		m.FailedUnits = atoiSafe(v)
	}
	if v, err := conn.GetManagerProperty("NNames"); err == nil {
		m.TotalNames = atoiSafe(v)
	}
	if m.Version == "" {
		return nil
	}
	return m
}

// baseService заполняет поля из ListUnits (без дополнительных запросов).
func baseService(u sd.UnitStatus) Service {
	return Service{
		Name:        u.Name,
		Description: u.Description,
		LoadState:   u.LoadState,
		ActiveState: u.ActiveState,
		SubState:    u.SubState,
	}
}

// enrich дополняет сервис свойствами из Unit- и Service-интерфейсов systemd.
func enrich(ctx context.Context, conn *sd.Conn, svc *Service, addErr func(string)) {
	// Unit-level свойства.
	if up, err := conn.GetUnitPropertiesContext(ctx, svc.Name); err == nil {
		svc.UnitFileState = propString(up, "UnitFileState")
		svc.Enabled = strings.HasPrefix(svc.UnitFileState, "enabled")
		svc.FragmentPath = propString(up, "FragmentPath")
		if svc.Description == "" {
			svc.Description = propString(up, "Description")
		}
		if t := propTimestamp(up, "ActiveEnterTimestamp"); t != nil {
			svc.ActiveEnterAt = t
			svc.UptimeSeconds = time.Since(*t).Seconds()
		}
	} else {
		addErr("unit props " + svc.Name + ": " + err.Error())
	}

	// Service-type свойства.
	if sp, err := conn.GetUnitTypePropertiesContext(ctx, svc.Name, "Service"); err == nil {
		svc.Type = propString(sp, "Type")
		svc.Result = propString(sp, "Result")
		svc.User = propString(sp, "User")
		svc.Group = propString(sp, "Group")
		svc.MainPID = int(propUint(sp, "MainPID"))
		svc.NRestarts = int(propUint(sp, "NRestarts"))
		svc.MemoryCurrentBytes = unsetMax(propUint(sp, "MemoryCurrent"))
		svc.MemoryPeakBytes = unsetMax(propUint(sp, "MemoryPeak"))
		svc.CPUUsageNSec = unsetMax(propUint(sp, "CPUUsageNSec"))
		svc.TasksCurrent = unsetMax(propUint(sp, "TasksCurrent"))
		svc.TasksMax = unsetMax(propUint(sp, "TasksMax"))
		if t := propTimestamp(sp, "ExecMainStartTimestamp"); t != nil {
			svc.ExecMainStartAt = t
		}
	} else {
		addErr("service props " + svc.Name + ": " + err.Error())
	}
}

// ───────────────────────────── helpers ─────────────────────────────

func stateRank(active string) int {
	switch active {
	case "failed":
		return 0
	case "activating":
		return 1
	case "active":
		return 2
	case "deactivating":
		return 3
	case "inactive":
		return 4
	default:
		return 5
	}
}

func propString(m map[string]interface{}, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

// propUint достаёт числовое свойство (systemd отдаёт uint32/uint64/int*).
func propUint(m map[string]interface{}, key string) uint64 {
	switch v := m[key].(type) {
	case uint64:
		return v
	case uint32:
		return uint64(v)
	case int64:
		if v < 0 {
			return 0
		}
		return uint64(v)
	case int32:
		if v < 0 {
			return 0
		}
		return uint64(v)
	default:
		return 0
	}
}

// propTimestamp: systemd хранит время в микросекундах от Unix-эпохи (uint64).
// 0 → событие не наступало (nil).
func propTimestamp(m map[string]interface{}, key string) *time.Time {
	usec := propUint(m, key)
	if usec == 0 {
		return nil
	}
	t := time.UnixMicro(int64(usec)).UTC()
	return &t
}

// unsetMax: systemd использует MaxUint64 как «не задано/бесконечность».
// Преобразуем в -1, чтобы фронт отличал «не учитывается» от реального 0.
func unsetMax(v uint64) int64 {
	if v == math.MaxUint64 {
		return -1
	}
	return int64(v)
}

func unquote(s string) string {
	return strings.Trim(strings.TrimSpace(s), `"`)
}

func atoiSafe(s string) int {
	n, _ := strconv.Atoi(unquote(s))
	return n
}
