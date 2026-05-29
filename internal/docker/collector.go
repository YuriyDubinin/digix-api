package docker

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"
)

// inspectConcurrency — сколько inspect-запросов выполняем параллельно.
// Ограничиваем, чтобы не завалить демон при большом числе контейнеров.
const inspectConcurrency = 8

// Collector собирает данные о контейнерах. Создаётся один раз в main.go.
// Stateless относительно запросов — безопасен для конкурентного использования.
type Collector struct {
	client *client
}

func NewCollector(host string) *Collector {
	return &Collector{client: newClient(host)}
}

// Collect возвращает снимок состояния Docker. Никогда не возвращает ошибку:
// недоступность демона выражается через ContainersInfo.Available=false.
func (c *Collector) Collect(ctx context.Context) *ContainersInfo {
	out := &ContainersInfo{
		CollectedAt: time.Now().UTC(),
		Containers:  []Container{},
	}

	// Если демон не отвечает на ping — дальше не идём.
	if err := c.client.ping(ctx); err != nil {
		out.Available = false
		out.Reason = "Docker daemon unreachable at " + c.client.socketPath + ": " + err.Error()
		return out
	}
	out.Available = true

	// Engine info (version + info) — параллельно, не критично при сбое.
	out.Engine = c.collectEngine(ctx, out)

	items, err := c.client.listContainers(ctx)
	if err != nil {
		out.Errors = append(out.Errors, "list containers: "+err.Error())
		return out
	}

	containers := make([]Container, len(items))
	var errMu sync.Mutex

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(inspectConcurrency)

	for i := range items {
		i := i
		item := items[i]
		g.Go(func() error {
			ins, ierr := c.client.inspectContainer(gctx, item.ID)
			if ierr != nil {
				// Inspect одного контейнера упал — кладём в errors,
				// строим контейнер из данных list (что есть).
				errMu.Lock()
				out.Errors = append(out.Errors, "inspect "+shortID(item.ID)+": "+ierr.Error())
				errMu.Unlock()
				containers[i] = containerFromList(item)
				return nil
			}
			containers[i] = mergeContainer(item, ins)
			return nil
		})
	}
	_ = g.Wait() // ошибок не возвращаем — все собраны в out.Errors

	// Стабильная сортировка: запущенные сверху, затем по имени.
	sort.SliceStable(containers, func(a, b int) bool {
		if containers[a].Running != containers[b].Running {
			return containers[a].Running
		}
		return containers[a].Name < containers[b].Name
	})

	out.Containers = containers
	out.Count = len(containers)
	return out
}

// CollectImages возвращает снимок образов на хосте. Никогда не возвращает
// ошибку: недоступность демона выражается через ImagesInfo.Available=false.
// Структура повторяет Collect (containers) — те же engine + errors-поля.
func (c *Collector) CollectImages(ctx context.Context) *ImagesInfo {
	out := &ImagesInfo{
		CollectedAt: time.Now().UTC(),
		Images:      []Image{},
	}

	if err := c.client.ping(ctx); err != nil {
		out.Available = false
		out.Reason = "Docker daemon unreachable at " + c.client.socketPath + ": " + err.Error()
		return out
	}
	out.Available = true

	engine, engineErrs := c.collectEngineRaw(ctx)
	out.Engine = engine
	out.Errors = append(out.Errors, engineErrs...)

	items, err := c.client.listImages(ctx)
	if err != nil {
		out.Errors = append(out.Errors, "list images: "+err.Error())
		return out
	}

	images := make([]Image, 0, len(items))
	for _, it := range items {
		images = append(images, BuildImage(it.ID, it.ParentID, it.RepoTags, it.RepoDigests,
			time.Unix(it.Created, 0).UTC(), it.Size, it.SharedSize, it.Labels, it.Containers))
	}

	// Сортировка: образы с тегами выше «висячих», затем по первому тегу/digest.
	sort.SliceStable(images, func(a, b int) bool {
		if images[a].Dangling != images[b].Dangling {
			return !images[a].Dangling
		}
		return imageSortKey(images[a]) < imageSortKey(images[b])
	})

	out.Images = images
	out.Count = len(images)
	return out
}

// BuildImage — фабрика публичного типа Image из «сырых» полей. Используется
// как локальным коллектором, так и пакетом remotedocker (через SSH), чтобы
// JSON-контракт совпал байт-в-байт.
func BuildImage(id, parentID string, repoTags, repoDigests []string, created time.Time,
	sizeBytes, sharedSize int64, labels map[string]string, containers int) Image {
	tags := filterRepoTags(repoTags)
	if containers < 0 {
		containers = 0
	}
	if sharedSize < 0 {
		sharedSize = 0
	}
	return Image{
		ID:          id,
		ShortID:     shortID(id),
		ParentID:    shortID(parentID),
		RepoTags:    tags,
		RepoDigests: repoDigests,
		Created:     created,
		SizeBytes:   sizeBytes,
		SharedSize:  sharedSize,
		Labels:      labels,
		Containers:  containers,
		Dangling:    len(tags) == 0,
	}
}

// filterRepoTags выкидывает Docker-плейсхолдер "<none>:<none>" — он встречается
// у dangling-образов и засоряет UI.
func filterRepoTags(tags []string) []string {
	if len(tags) == 0 {
		return nil
	}
	out := make([]string, 0, len(tags))
	for _, t := range tags {
		if t == "" || t == "<none>:<none>" || t == "<none>" {
			continue
		}
		out = append(out, t)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// imageSortKey — стабильный ключ для сортировки: первый тег, иначе первый digest, иначе ID.
func imageSortKey(img Image) string {
	if len(img.RepoTags) > 0 {
		return img.RepoTags[0]
	}
	if len(img.RepoDigests) > 0 {
		return img.RepoDigests[0]
	}
	return img.ID
}

// Version — лёгкий запрос к Docker Engine: возвращает версию демона и версию API.
// Используется в sysinfo для секции docker в /api/system/main.
// Возвращает ошибку, если демон недоступен.
func (c *Collector) Version(ctx context.Context) (engineVersion, apiVersion string, err error) {
	v, verr := c.client.version(ctx)
	if verr != nil {
		return "", "", verr
	}
	return v.Version, v.APIVersion, nil
}

func (c *Collector) collectEngine(ctx context.Context, out *ContainersInfo) *EngineInfo {
	engine, errs := c.collectEngineRaw(ctx)
	out.Errors = append(out.Errors, errs...)
	return engine
}

// collectEngineRaw — версия без mutation параметра. Возвращает engine и
// список ошибок отдельно — caller сам решает, куда их складывать.
func (c *Collector) collectEngineRaw(ctx context.Context) (*EngineInfo, []string) {
	var (
		ver  *apiVersion
		info *apiInfo
	)
	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		v, err := c.client.version(gctx)
		if err == nil {
			ver = v
		}
		return nil
	})
	g.Go(func() error {
		i, err := c.client.info(gctx)
		if err == nil {
			info = i
		}
		return nil
	})
	_ = g.Wait()

	if ver == nil && info == nil {
		return nil, []string{"engine info unavailable"}
	}

	e := &EngineInfo{}
	if ver != nil {
		e.Version = ver.Version
		e.APIVersion = ver.APIVersion
		e.GitCommit = ver.GitCommit
		e.GoVersion = ver.GoVersion
	}
	if info != nil {
		e.ID = info.ID
		e.Name = info.Name
		e.OperatingSystem = info.OperatingSystem
		e.OSType = info.OSType
		e.Architecture = info.Architecture
		e.KernelVersion = info.KernelVersion
		e.StorageDriver = info.Driver
		e.CgroupVersion = info.CgroupVersion
		e.MemoryTotalBytes = info.MemTotal
		e.NCPU = info.NCPU
		e.ContainersTotal = info.Containers
		e.ContainersRunning = info.ContainersRunning
		e.ContainersPaused = info.ContainersPaused
		e.ContainersStopped = info.ContainersStopped
		e.ImagesTotal = info.Images
		if e.Version == "" {
			e.Version = info.ServerVersion
		}
	}
	return e, nil
}

// mergeContainer объединяет данные из list (size, ports, status) и inspect (всё остальное).
func mergeContainer(item apiListItem, ins *apiInspect) Container {
	c := Container{
		ID:              ins.ID,
		ShortID:         shortID(ins.ID),
		Name:            cleanName(ins.Name),
		Image:           ins.Config.Image,
		ImageID:         ins.Image,
		Command:         item.Command,
		CreatedAt:       time.Unix(item.Created, 0).UTC(),
		Status:          item.Status,
		RestartCount:    ins.RestartCount,
		Platform:        ins.Platform,
		LogPath:         ins.LogPath,
		RestartPolicy:   ins.HostConfig.RestartPolicy.Name,
		NetworkMode:     ins.HostConfig.NetworkMode,
		Privileged:      ins.HostConfig.Privileged,
		User:            ins.Config.User,
		WorkingDir:      ins.Config.WorkingDir,
		Entrypoint:      ins.Config.Entrypoint,
		Cmd:             ins.Config.Cmd,
		Env:             ins.Config.Env,
		Labels:          ins.Config.Labels,
		SizeRwBytes:     item.SizeRw,
		SizeRootFsBytes: item.SizeRootFs,
		Limits: ResourceLimits{
			MemoryBytes: ins.HostConfig.Memory,
			NanoCPUs:    ins.HostConfig.NanoCpus,
			CPUShares:   ins.HostConfig.CPUShares,
		},
	}

	// State
	st := ins.State
	c.State = st.Status
	c.Running = st.Running
	c.Paused = st.Paused
	c.Restarting = st.Restarting
	c.Dead = st.Dead
	c.OOMKilled = st.OOMKilled
	c.ExitCode = st.ExitCode
	c.PID = st.Pid
	c.StartedAt = parseDockerTime(st.StartedAt)
	c.FinishedAt = parseDockerTime(st.FinishedAt)
	if st.Health != nil {
		c.Health = st.Health.Status
		c.HealthFailing = st.Health.FailingStreak
	}

	// Ports — из list (плоский массив удобнее).
	for _, p := range item.Ports {
		c.Ports = append(c.Ports, Port{
			IP:          p.IP,
			PrivatePort: p.PrivatePort,
			PublicPort:  p.PublicPort,
			Type:        p.Type,
		})
	}

	// Mounts — из inspect.
	for _, m := range ins.Mounts {
		c.Mounts = append(c.Mounts, Mount{
			Type:        m.Type,
			Name:        m.Name,
			Source:      m.Source,
			Destination: m.Destination,
			Mode:        m.Mode,
			RW:          m.RW,
		})
	}

	// Networks — из inspect (map → отсортированный список для стабильного вывода).
	names := make([]string, 0, len(ins.NetworkSettings.Networks))
	for name := range ins.NetworkSettings.Networks {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		ep := ins.NetworkSettings.Networks[name]
		c.Networks = append(c.Networks, Network{
			Name:       name,
			IPAddress:  ep.IPAddress,
			Gateway:    ep.Gateway,
			MacAddress: ep.MacAddress,
			NetworkID:  shortID(ep.NetworkID),
		})
	}

	return c
}

// containerFromList строит минимальный Container, когда inspect не удался.
func containerFromList(item apiListItem) Container {
	c := Container{
		ID:              item.ID,
		ShortID:         shortID(item.ID),
		Name:            firstName(item.Names),
		Image:           item.Image,
		ImageID:         item.ImageID,
		Command:         item.Command,
		CreatedAt:       time.Unix(item.Created, 0).UTC(),
		State:           item.State,
		Status:          item.Status,
		Running:         item.State == "running",
		Labels:          item.Labels,
		SizeRwBytes:     item.SizeRw,
		SizeRootFsBytes: item.SizeRootFs,
	}
	for _, p := range item.Ports {
		c.Ports = append(c.Ports, Port{
			IP: p.IP, PrivatePort: p.PrivatePort, PublicPort: p.PublicPort, Type: p.Type,
		})
	}
	return c
}

// ───────────────────────────── helpers ─────────────────────────────

func shortID(id string) string {
	id = strings.TrimPrefix(id, "sha256:")
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

func cleanName(name string) string {
	return strings.TrimPrefix(name, "/")
}

func firstName(names []string) string {
	if len(names) == 0 {
		return ""
	}
	return cleanName(names[0])
}

// parseDockerTime парсит RFC3339Nano. Docker для «нулевого» времени отдаёт
// "0001-01-01T00:00:00Z" — такое возвращаем как nil.
func parseDockerTime(s string) *time.Time {
	if s == "" || strings.HasPrefix(s, "0001-01-01") {
		return nil
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return nil
	}
	t = t.UTC()
	return &t
}
