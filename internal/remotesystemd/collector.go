// Package remotesystemd собирает systemd .service unit'ы удалённого сервера
// через SSH — без D-Bus, используя CLI `systemctl`. Возвращает тот же тип
// *systemd.ServicesInfo, что и локальный /api/system/services, чтобы фронт
// рендерил оба эндпоинта одними компонентами.
//
// Команды:
//   - systemctl --version → ManagerInfo.Version + Architecture (из uname -m)
//   - systemctl is-system-running --no-pager → опционально, для проверки доступности
//   - systemctl list-units --type=service --all --no-pager --no-legend --plain
//     → список с LoadState/ActiveState/SubState/Description
//   - systemctl show <unit1> <unit2> ... --no-pager --property=...
//     → массовый запрос всех нужных свойств одним вызовом
//
// Принципы:
//   - Available=false с Reason, если systemctl отсутствует или D-Bus недоступен.
//   - Best-effort: пропавший unit / упавший show — в ServicesInfo.Errors,
//     остальные собираются как есть.
//   - Никаких изменений на удалённом сервере (только read-only команды).
package remotesystemd

import (
	"context"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/YuriyDubinin/dijex-api/internal/systemd"
)

// Collector — сборщик systemd-сервисов с удалённого хоста.
type Collector struct{}

func NewCollector() *Collector { return &Collector{} }

// Collect выполняет несколько read-only systemctl-запросов через SSH.
// Никогда не возвращает ошибку — недоступность systemd выражается через Available=false.
func (c *Collector) Collect(ctx context.Context, client *ssh.Client) *systemd.ServicesInfo {
	out := &systemd.ServicesInfo{
		CollectedAt: time.Now().UTC(),
		Services:    []systemd.Service{},
	}

	// Шаг 1: проверка наличия systemctl + список unit'ов одним SSH-вызовом.
	// printf '\n===<NAME>===\n' с ведущим \n — на случай, если предыдущая
	// команда выдала вывод без trailing newline (defense-in-depth; основная
	// защита — splitTrailingMarker в shell.go).
	const listScript = `printf '===WHICH===\n'
command -v systemctl 2>/dev/null
printf '\n===VERSION===\n'
systemctl --version 2>/dev/null
printf '\n===ARCH===\n'
uname -m 2>/dev/null
printf '\n===LIST===\n'
systemctl list-units --type=service --all --no-pager --no-legend --plain 2>/dev/null
printf '\n===END===\n'
`
	listRaw, err := runSSH(ctx, client, listScript)
	if err != nil && listRaw == "" {
		out.Available = false
		out.Reason = "ssh command failed: " + err.Error()
		return out
	}
	blocks := splitByMarkers(listRaw, []string{
		"===WHICH===", "===VERSION===", "===ARCH===", "===LIST===", "===END===",
	})

	if strings.TrimSpace(blocks["===WHICH==="]) == "" {
		out.Available = false
		out.Reason = "systemctl not found on remote host (non-systemd OS?)"
		return out
	}
	out.Available = true

	out.Manager = parseManager(blocks["===VERSION==="], blocks["===ARCH==="])

	// Парсим список — там же берём имена для следующего шага.
	services := parseListUnits(blocks["===LIST==="])
	if len(services) == 0 {
		// systemctl есть, но units нет — возможно, отказ D-Bus. Пишем в errors.
		out.Errors = append(out.Errors, "list units: empty result (D-Bus unreachable?)")
		return out
	}

	names := make([]string, 0, len(services))
	for _, s := range services {
		names = append(names, s.Name)
	}

	// Шаг 2: массовый show по всем сервисам. Свойства указываем явно — это
	// сильно сокращает вывод (десятки полей на unit вместо сотен).
	const showProps = "Names,Description,LoadState,ActiveState,SubState,UnitFileState,Type," +
		"MainPID,Result,User,Group,FragmentPath,ActiveEnterTimestamp,ExecMainStartTimestamp," +
		"NRestarts,MemoryCurrent,MemoryPeak,CPUUsageNSec,TasksCurrent,TasksMax"

	// Аргумент-список может быть очень длинным; делим на батчи, чтобы не упереться
	// в ARG_MAX (на Linux обычно 128 КБ). 200 unit'ов в батче — с большим запасом.
	const batchSize = 200
	for start := 0; start < len(names); start += batchSize {
		end := start + batchSize
		if end > len(names) {
			end = len(names)
		}
		batch := names[start:end]
		args := strings.Join(quoteAll(batch), " ")
		cmd := "systemctl show --no-pager --property=" + showProps + " " + args + " 2>/dev/null"
		raw, serr := runSSH(ctx, client, cmd)
		if serr != nil && raw == "" {
			out.Errors = append(out.Errors, "systemctl show: "+serr.Error())
			continue
		}
		enrichFromShow(raw, services)
	}

	// Сортировка: failed/activating сверху, потом active, потом всё остальное.
	sort.SliceStable(services, func(a, b int) bool {
		ra, rb := stateRank(services[a].ActiveState), stateRank(services[b].ActiveState)
		if ra != rb {
			return ra < rb
		}
		return services[a].Name < services[b].Name
	})

	out.Services = services
	out.Count = len(services)
	return out
}

// ──────────────────────── parsers ─────────────────────────

// parseManager берёт первую строку `systemctl --version` ("systemd 252 (252.5-2ubuntu3)")
// и архитектуру из `uname -m`.
func parseManager(verRaw, archRaw string) *systemd.ManagerInfo {
	verRaw = strings.TrimSpace(verRaw)
	if verRaw == "" {
		return nil
	}
	// Первая строка: "systemd 252 (252.5-2ubuntu3)" → "252".
	first := verRaw
	if i := strings.IndexByte(verRaw, '\n'); i >= 0 {
		first = strings.TrimSpace(verRaw[:i])
	}
	version := ""
	if f := strings.Fields(first); len(f) >= 2 {
		version = f[1]
	}
	if version == "" {
		return nil
	}
	return &systemd.ManagerInfo{
		Version:      version,
		Architecture: strings.TrimSpace(archRaw),
	}
}

// parseListUnits разбирает вывод `systemctl list-units ... --plain --no-legend`.
// Формат каждой строки (поля разделены любым количеством пробелов):
//
//	<UNIT> <LOAD> <ACTIVE> <SUB> <DESCRIPTION (может содержать пробелы)>
func parseListUnits(text string) []systemd.Service {
	if text == "" {
		return nil
	}
	out := make([]systemd.Service, 0, 128)
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Удаляем ANSI-маркер «●» и подобные префиксы (на некоторых системах
		// list-units --plain всё равно вставляет статус-символ перед именем).
		line = strings.TrimLeft(line, "●○↻*x ")
		f := strings.Fields(line)
		if len(f) < 4 || !strings.HasSuffix(f[0], ".service") {
			continue
		}
		// LoadState=not-found / masked → описание может отсутствовать.
		descr := ""
		if len(f) >= 5 {
			descr = strings.Join(f[4:], " ")
		}
		out = append(out, systemd.Service{
			Name:        f[0],
			LoadState:   f[1],
			ActiveState: f[2],
			SubState:    f[3],
			Description: descr,
		})
	}
	return out
}

// enrichFromShow разбирает блоки `systemctl show` (по одному блоку на unit,
// разделены пустой строкой) и заполняет уже существующие записи в services.
func enrichFromShow(raw string, services []systemd.Service) {
	if raw == "" {
		return
	}
	// Индекс по имени для быстрого поиска.
	idx := make(map[string]int, len(services))
	for i := range services {
		idx[services[i].Name] = i
	}

	now := time.Now()
	for _, block := range strings.Split(raw, "\n\n") {
		kv := parseShowBlock(block)
		if len(kv) == 0 {
			continue
		}
		// Names = "foo.service bar.service" (alias'ы), реальный unit — первое имя.
		name := strings.Fields(kv["Names"])
		if len(name) == 0 {
			continue
		}
		i, ok := idx[name[0]]
		if !ok {
			continue
		}
		applyShowProps(&services[i], kv, now)
	}
}

// parseShowBlock — `Key=Value` построчно (с возможными `=` в значении).
func parseShowBlock(block string) map[string]string {
	if block == "" {
		return nil
	}
	out := make(map[string]string, 24)
	for _, line := range strings.Split(block, "\n") {
		i := strings.IndexByte(line, '=')
		if i <= 0 {
			continue
		}
		out[strings.TrimSpace(line[:i])] = line[i+1:]
	}
	return out
}

func applyShowProps(s *systemd.Service, kv map[string]string, now time.Time) {
	if v := kv["Description"]; v != "" {
		s.Description = v
	}
	if v := kv["LoadState"]; v != "" {
		s.LoadState = v
	}
	if v := kv["ActiveState"]; v != "" {
		s.ActiveState = v
	}
	if v := kv["SubState"]; v != "" {
		s.SubState = v
	}
	if v := kv["UnitFileState"]; v != "" {
		s.UnitFileState = v
		s.Enabled = strings.HasPrefix(v, "enabled")
	}
	s.Type = kv["Type"]
	s.Result = kv["Result"]
	s.User = kv["User"]
	s.Group = kv["Group"]
	s.FragmentPath = kv["FragmentPath"]
	s.MainPID = atoiSafe(kv["MainPID"])
	s.NRestarts = atoiSafe(kv["NRestarts"])
	s.MemoryCurrentBytes = unsetMax(atouSafe(kv["MemoryCurrent"]))
	s.MemoryPeakBytes = unsetMax(atouSafe(kv["MemoryPeak"]))
	s.CPUUsageNSec = unsetMax(atouSafe(kv["CPUUsageNSec"]))
	s.TasksCurrent = unsetMax(atouSafe(kv["TasksCurrent"]))
	s.TasksMax = unsetMax(atouSafe(kv["TasksMax"]))

	if t := parseSystemdTimestamp(kv["ActiveEnterTimestamp"]); t != nil {
		s.ActiveEnterAt = t
		s.UptimeSeconds = now.Sub(*t).Seconds()
	}
	if t := parseSystemdTimestamp(kv["ExecMainStartTimestamp"]); t != nil {
		s.ExecMainStartAt = t
	}
}

// ─────────── helpers ───────────

// systemdTimestampFormats — варианты формата `ActiveEnterTimestamp`, который
// меняется в зависимости от локали ОС. В systemd с C-локалью обычно:
//
//	Wed 2026-05-22 09:14:01 UTC
//
// На некоторых дистрах с другим LC_TIME — без дня недели или без TZ.
var systemdTimestampFormats = []string{
	"Mon 2006-01-02 15:04:05 MST",
	"Mon 2006-01-02 15:04:05",
	"2006-01-02 15:04:05 MST",
	"2006-01-02 15:04:05",
}

func parseSystemdTimestamp(s string) *time.Time {
	s = strings.TrimSpace(s)
	if s == "" || s == "n/a" {
		return nil
	}
	for _, layout := range systemdTimestampFormats {
		if t, err := time.Parse(layout, s); err == nil {
			ut := t.UTC()
			return &ut
		}
	}
	return nil
}

// stateRank — порядок приоритета для UI: failed → activating → active → ...
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

func atoiSafe(s string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	return n
}

func atouSafe(s string) uint64 {
	n, _ := strconv.ParseUint(strings.TrimSpace(s), 10, 64)
	return n
}

// unsetMax: systemd использует MaxUint64 как «не задано/бесконечность» —
// преобразуем в -1, чтобы фронт отличал «не учитывается» от реального 0.
// Также «[not set]» строкой парсится как 0, что эквивалентно «не задано», но
// если строка точно совпадает с MaxUint64, переводим её именно в -1.
func unsetMax(v uint64) int64 {
	if v == math.MaxUint64 {
		return -1
	}
	return int64(v)
}

// quoteAll оборачивает каждое имя в одинарные кавычки, экранируя одинарную внутри.
// systemd unit-имена обычно не содержат пробелов/спецсимволов, но на всякий случай.
func quoteAll(items []string) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = "'" + strings.ReplaceAll(it, "'", `'\''`) + "'"
	}
	return out
}
