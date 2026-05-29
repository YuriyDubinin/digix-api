// Package remotedocker собирает данные о Docker-контейнерах удалённого сервера
// через SSH (без HTTP-туннеля на сокет демона). Использует CLI `docker` на самом
// удалённом хосте: один сводный скрипт `docker version` + `docker info` +
// `docker inspect --size $(docker ps -a -q)`.
//
// Возвращает *docker.ContainersInfo — точно тот же тип, что и локальный
// /api/system/containers. Это позволяет фронту использовать одни и те же
// компоненты для рендера и локальных, и удалённых контейнеров.
//
// Принципы:
//   - Available=false с понятным Reason, если docker не установлен/не отвечает.
//   - Best-effort: если version/info не отработали — engine=nil, но контейнеры
//     при этом могут собраться.
//   - Никаких изменений на удалённом сервере (только read-only команды).
package remotedocker

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/YuriyDubinin/dijex-api/internal/docker"
)

// Collector — сборщик контейнеров с удалённого сервера. Stateless,
// безопасен для конкурентного использования.
type Collector struct{}

func NewCollector() *Collector { return &Collector{} }

// Collect выполняет один сводный SSH-скрипт и парсит вывод. Никогда не
// возвращает ошибку — недоступность Docker выражается через Available=false.
func (c *Collector) Collect(ctx context.Context, client *ssh.Client) *docker.ContainersInfo {
	out := &docker.ContainersInfo{
		CollectedAt: time.Now().UTC(),
		Containers:  []docker.Container{},
	}

	// Сводный скрипт. Разделители маркерами, чтобы не зависеть от порядка/пустоты блоков.
	// printf '\n===<NAME>===\n' с ведущим \n — на случай, если предыдущая
	// команда (docker inspect, docker info) выдала JSON без trailing newline.
	// Основная защита — splitTrailingMarker в shell.go.
	const script = `printf '===WHICH===\n'
command -v docker 2>/dev/null
printf '\n===VERSION===\n'
docker version --format '{{json .Server}}' 2>/dev/null
printf '\n===INFO===\n'
docker info --format '{{json .}}' 2>/dev/null
printf '\n===INSPECT===\n'
ids=$(docker ps -a -q --no-trunc 2>/dev/null)
if [ -n "$ids" ]; then
  docker inspect --size $ids 2>/dev/null
else
  printf '[]'
fi
printf '\n===END===\n'
`
	output, err := runSSH(ctx, client, script)
	if err != nil && output == "" {
		out.Available = false
		out.Reason = "ssh command failed: " + err.Error()
		return out
	}
	blocks := splitByMarkers(output, []string{
		"===WHICH===", "===VERSION===", "===INFO===", "===INSPECT===", "===END===",
	})

	if strings.TrimSpace(blocks["===WHICH==="]) == "" {
		out.Available = false
		out.Reason = "docker CLI not found on remote host"
		return out
	}

	// Если оба обработчика — version и info — пустые, значит демон, скорее всего,
	// не запущен / недоступен текущему пользователю.
	verRaw := strings.TrimSpace(blocks["===VERSION==="])
	infoRaw := strings.TrimSpace(blocks["===INFO==="])
	if verRaw == "" && infoRaw == "" {
		out.Available = false
		out.Reason = "docker daemon unreachable (version/info empty — check permissions or daemon status)"
		return out
	}
	out.Available = true

	out.Engine = parseEngine(verRaw, infoRaw, &out.Errors)

	inspectRaw := strings.TrimSpace(blocks["===INSPECT==="])
	if inspectRaw != "" && inspectRaw != "[]" {
		containers, perr := parseInspectArray(inspectRaw)
		if perr != nil {
			out.Errors = append(out.Errors, "parse inspect: "+perr.Error())
		} else {
			out.Containers = containers
			out.Count = len(containers)
		}
	}

	// Стабильная сортировка как в локальном collector'е: running сверху, затем по имени.
	sort.SliceStable(out.Containers, func(a, b int) bool {
		if out.Containers[a].Running != out.Containers[b].Running {
			return out.Containers[a].Running
		}
		return out.Containers[a].Name < out.Containers[b].Name
	})

	return out
}

// ──────────────────────── parse engine ─────────────────────────

// apiVersion / apiInfo — внутренние представления, повторяют JSON Docker Engine.
type apiVersion struct {
	Version       string `json:"Version"`
	APIVersion    string `json:"ApiVersion"`
	GitCommit     string `json:"GitCommit"`
	GoVersion     string `json:"GoVersion"`
	KernelVersion string `json:"KernelVersion"`
	Os            string `json:"Os"`
	Arch          string `json:"Arch"`
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

func parseEngine(verRaw, infoRaw string, errs *[]string) *docker.EngineInfo {
	var ver apiVersion
	var info apiInfo
	if verRaw != "" {
		if err := json.Unmarshal([]byte(verRaw), &ver); err != nil {
			*errs = append(*errs, "parse version: "+err.Error())
		}
	}
	if infoRaw != "" {
		if err := json.Unmarshal([]byte(infoRaw), &info); err != nil {
			*errs = append(*errs, "parse info: "+err.Error())
		}
	}
	if ver.Version == "" && info.ServerVersion == "" {
		return nil
	}

	e := &docker.EngineInfo{
		Version:           ver.Version,
		APIVersion:        ver.APIVersion,
		GitCommit:         ver.GitCommit,
		GoVersion:         ver.GoVersion,
		ID:                info.ID,
		Name:              info.Name,
		OperatingSystem:   info.OperatingSystem,
		OSType:            info.OSType,
		Architecture:      info.Architecture,
		KernelVersion:     info.KernelVersion,
		StorageDriver:     info.Driver,
		CgroupVersion:     info.CgroupVersion,
		MemoryTotalBytes:  info.MemTotal,
		NCPU:              info.NCPU,
		ContainersTotal:   info.Containers,
		ContainersRunning: info.ContainersRunning,
		ContainersPaused:  info.ContainersPaused,
		ContainersStopped: info.ContainersStopped,
		ImagesTotal:       info.Images,
	}
	if e.Version == "" {
		e.Version = info.ServerVersion
	}
	if e.KernelVersion == "" {
		e.KernelVersion = ver.KernelVersion
	}
	return e
}

// ──────────────────────── parse inspect ─────────────────────────

// apiInspect — структура одного объекта в массиве `docker inspect`. Повторяет
// поля Docker Engine API (PascalCase). Берём только то, что нужно публичному типу.
type apiInspect struct {
	ID              string             `json:"Id"`
	Created         string             `json:"Created"`
	Path            string             `json:"Path"`
	Args            []string           `json:"Args"`
	Name            string             `json:"Name"`
	Image           string             `json:"Image"` // image ID (sha256:...)
	RestartCount    int                `json:"RestartCount"`
	Platform        string             `json:"Platform"`
	LogPath         string             `json:"LogPath"`
	SizeRw          int64              `json:"SizeRw"`
	SizeRootFs      int64              `json:"SizeRootFs"`
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
		Name string `json:"Name"`
	} `json:"RestartPolicy"`
	Memory      int64  `json:"Memory"`
	NanoCpus    int64  `json:"NanoCpus"`
	CPUShares   int64  `json:"CpuShares"`
	NetworkMode string `json:"NetworkMode"`
	Privileged  bool   `json:"Privileged"`
}

type apiConfig struct {
	User       string            `json:"User"`
	Env        []string          `json:"Env"`
	Cmd        []string          `json:"Cmd"`
	Entrypoint []string          `json:"Entrypoint"`
	Image      string            `json:"Image"` // "nginx:latest"
	WorkingDir string            `json:"WorkingDir"`
	Labels     map[string]string `json:"Labels"`
}

type apiNetworkSettings struct {
	Networks map[string]apiEndpoint    `json:"Networks"`
	Ports    map[string][]apiPortBind  `json:"Ports"` // "80/tcp" -> [{HostIp, HostPort}]
}

type apiEndpoint struct {
	IPAddress  string `json:"IPAddress"`
	Gateway    string `json:"Gateway"`
	MacAddress string `json:"MacAddress"`
	NetworkID  string `json:"NetworkID"`
}

type apiPortBind struct {
	HostIP   string `json:"HostIp"`
	HostPort string `json:"HostPort"`
}

type apiMount struct {
	Type        string `json:"Type"`
	Name        string `json:"Name"`
	Source      string `json:"Source"`
	Destination string `json:"Destination"`
	Mode        string `json:"Mode"`
	RW          bool   `json:"RW"`
}

func parseInspectArray(raw string) ([]docker.Container, error) {
	var items []apiInspect
	if err := json.Unmarshal([]byte(raw), &items); err != nil {
		return nil, err
	}
	now := time.Now()
	out := make([]docker.Container, 0, len(items))
	for _, it := range items {
		out = append(out, buildContainer(it, now))
	}
	return out, nil
}

// buildContainer строит публичный docker.Container из inspect-данных.
// Status-строка («Up 3 hours», «Exited (0) 2 days ago») генерируется здесь,
// потому что Docker Engine API её не возвращает (это делает CLI).
func buildContainer(in apiInspect, now time.Time) docker.Container {
	c := docker.Container{
		ID:              in.ID,
		ShortID:         shortID(in.ID),
		Name:            cleanName(in.Name),
		Image:           in.Config.Image,
		ImageID:         in.Image,
		Command:         buildCommand(in.Path, in.Args),
		CreatedAt:       parseDockerTime(in.Created).valueOr(time.Time{}),
		State:           in.State.Status,
		Status:          buildStatusString(in.State, now),
		Running:         in.State.Running,
		Paused:          in.State.Paused,
		Restarting:      in.State.Restarting,
		Dead:            in.State.Dead,
		OOMKilled:       in.State.OOMKilled,
		ExitCode:        in.State.ExitCode,
		PID:             in.State.Pid,
		RestartCount:    in.RestartCount,
		Platform:        in.Platform,
		LogPath:         in.LogPath,
		RestartPolicy:   in.HostConfig.RestartPolicy.Name,
		NetworkMode:     in.HostConfig.NetworkMode,
		Privileged:      in.HostConfig.Privileged,
		User:            in.Config.User,
		WorkingDir:      in.Config.WorkingDir,
		Entrypoint:      in.Config.Entrypoint,
		Cmd:             in.Config.Cmd,
		Env:             in.Config.Env,
		Labels:          in.Config.Labels,
		SizeRwBytes:     in.SizeRw,
		SizeRootFsBytes: in.SizeRootFs,
		Limits: docker.ResourceLimits{
			MemoryBytes: in.HostConfig.Memory,
			NanoCPUs:    in.HostConfig.NanoCpus,
			CPUShares:   in.HostConfig.CPUShares,
		},
	}
	c.StartedAt = parseDockerTime(in.State.StartedAt).ptr()
	c.FinishedAt = parseDockerTime(in.State.FinishedAt).ptr()
	if in.State.Health != nil {
		c.Health = in.State.Health.Status
		c.HealthFailing = in.State.Health.FailingStreak
	}

	// Ports: ключи карты — "<port>/<proto>", значение — список биндингов.
	portKeys := make([]string, 0, len(in.NetworkSettings.Ports))
	for k := range in.NetworkSettings.Ports {
		portKeys = append(portKeys, k)
	}
	sort.Strings(portKeys)
	for _, key := range portKeys {
		privPort, proto := parsePortKey(key)
		if privPort == 0 {
			continue
		}
		binds := in.NetworkSettings.Ports[key]
		if len(binds) == 0 {
			// Порт объявлен (EXPOSE), но не опубликован.
			c.Ports = append(c.Ports, docker.Port{PrivatePort: privPort, Type: proto})
			continue
		}
		for _, b := range binds {
			pub, _ := strconv.Atoi(b.HostPort)
			c.Ports = append(c.Ports, docker.Port{
				IP:          b.HostIP,
				PrivatePort: privPort,
				PublicPort:  pub,
				Type:        proto,
			})
		}
	}

	// Mounts
	for _, m := range in.Mounts {
		c.Mounts = append(c.Mounts, docker.Mount{
			Type: m.Type, Name: m.Name, Source: m.Source,
			Destination: m.Destination, Mode: m.Mode, RW: m.RW,
		})
	}

	// Networks (стабильный порядок).
	netNames := make([]string, 0, len(in.NetworkSettings.Networks))
	for n := range in.NetworkSettings.Networks {
		netNames = append(netNames, n)
	}
	sort.Strings(netNames)
	for _, n := range netNames {
		ep := in.NetworkSettings.Networks[n]
		c.Networks = append(c.Networks, docker.Network{
			Name: n, IPAddress: ep.IPAddress, Gateway: ep.Gateway,
			MacAddress: ep.MacAddress, NetworkID: shortID(ep.NetworkID),
		})
	}

	return c
}

// buildCommand склеивает path + args обратно в строку, как это делает Docker CLI
// для колонки "COMMAND". Не идеально (теряются кавычки), но достаточно для UI.
func buildCommand(path string, args []string) string {
	if path == "" {
		return ""
	}
	parts := append([]string{path}, args...)
	return strings.Join(parts, " ")
}

// buildStatusString собирает человекочитаемый Status: "Up 3 hours", "Exited (0) 2 days ago".
// Логика повторяет вывод `docker ps`.
func buildStatusString(st apiState, now time.Time) string {
	switch {
	case st.Restarting:
		return "Restarting (" + strconv.Itoa(st.ExitCode) + ")"
	case st.Running:
		started := parseDockerTime(st.StartedAt)
		if started.set {
			d := humanizeDuration(now.Sub(started.t))
			if st.Paused {
				return "Up " + d + " (Paused)"
			}
			if st.Health == nil {
				return "Up " + d
			}
			return "Up " + d + " (" + st.Health.Status + ")"
		}
		return "Up"
	case st.Status == "created":
		return "Created"
	case st.Dead:
		return "Dead"
	default:
		finished := parseDockerTime(st.FinishedAt)
		if finished.set {
			return fmt.Sprintf("Exited (%d) %s ago", st.ExitCode, humanizeDuration(now.Sub(finished.t)))
		}
		if st.Status != "" {
			return strings.Title(st.Status) //nolint:staticcheck
		}
		return ""
	}
}

// humanizeDuration — короткий формат «3 hours», «2 days», «5 minutes», аналог
// тому, что выводит `docker ps`. Не пытаемся имитировать формат до миллисекунды;
// цель — UI, а не точные значения.
func humanizeDuration(d time.Duration) string {
	if d < 0 {
		d = -d
	}
	switch {
	case d < time.Minute:
		return strconv.Itoa(int(d.Seconds())) + " seconds"
	case d < 2*time.Minute:
		return "1 minute"
	case d < time.Hour:
		return strconv.Itoa(int(d.Minutes())) + " minutes"
	case d < 2*time.Hour:
		return "1 hour"
	case d < 24*time.Hour:
		return strconv.Itoa(int(d.Hours())) + " hours"
	case d < 48*time.Hour:
		return "1 day"
	case d < 7*24*time.Hour:
		return strconv.Itoa(int(d.Hours()/24)) + " days"
	case d < 14*24*time.Hour:
		return "1 week"
	case d < 60*24*time.Hour:
		return strconv.Itoa(int(d.Hours()/(24*7))) + " weeks"
	case d < 365*24*time.Hour:
		return strconv.Itoa(int(d.Hours()/(24*30))) + " months"
	default:
		return strconv.Itoa(int(d.Hours()/(24*365))) + " years"
	}
}

// ─────────── helpers (parsing-сторона) ───────────

func shortID(id string) string {
	id = strings.TrimPrefix(id, "sha256:")
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

func cleanName(name string) string { return strings.TrimPrefix(name, "/") }

func parsePortKey(key string) (port int, proto string) {
	parts := strings.SplitN(key, "/", 2)
	if len(parts) != 2 {
		return 0, ""
	}
	port, _ = strconv.Atoi(parts[0])
	return port, parts[1]
}

// optionalTime — обёртка для парсинга dockerTime: позволяет различать «не задано»
// (set=false) и «реальный ноль времени», не делая аллокаций до момента превращения в *time.Time.
type optionalTime struct {
	t   time.Time
	set bool
}

func (o optionalTime) ptr() *time.Time {
	if !o.set {
		return nil
	}
	t := o.t
	return &t
}

func (o optionalTime) valueOr(def time.Time) time.Time {
	if !o.set {
		return def
	}
	return o.t
}

// parseDockerTime разбирает RFC3339Nano. Docker для «нулевого» времени отдаёт
// "0001-01-01T00:00:00Z" — такое возвращаем как unset.
func parseDockerTime(s string) optionalTime {
	if s == "" || strings.HasPrefix(s, "0001-01-01") {
		return optionalTime{}
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return optionalTime{}
	}
	return optionalTime{t: t.UTC(), set: true}
}
