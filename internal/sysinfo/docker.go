package sysinfo

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"time"
)

// defaultComposeBinPath — типичный путь к плагину docker compose на Linux-хостах
// (Debian/Ubuntu/RHEL). На некоторых сборках лежит в /usr/local/lib/docker/cli-plugins/.
// Чтобы видеть ХОСТОВУЮ версию из контейнера, этот бинарь нужно bind-mount'ить:
//   -v /usr/libexec/docker/cli-plugins/docker-compose:/usr/libexec/docker/cli-plugins/docker-compose:ro
const defaultComposeBinPath = "/usr/libexec/docker/cli-plugins/docker-compose"

// dockerVersionProvider — узкий контракт получения версии Docker Engine.
// Реализуется *docker.Collector. nil допустим — тогда engine не собирается.
type dockerVersionProvider interface {
	Version(ctx context.Context) (engine, apiVersion string, err error)
}

// collectDocker заполняет секцию docker: версии Engine и Compose.
// Compose тянем через CLI (`docker compose version --short`) best-effort —
// если бинаря нет в контейнере, поле остаётся пустым.
func (c *Collector) collectDocker(ctx context.Context) DockerVersions {
	out := DockerVersions{}

	// Engine: коротко через Docker API (если провайдер задан).
	if c.dockerVersion != nil {
		vctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		if engine, api, err := c.dockerVersion.Version(vctx); err == nil {
			out.Engine = engine
			out.EngineAPI = api
		}
	}

	// Compose: best-effort shell-out на docker CLI.
	out.Compose = detectComposeVersion(ctx)
	return out
}

// detectComposeVersion возвращает версию Docker Compose, установленного НА ХОСТЕ.
//
// Стратегия (по приоритету):
//  1. Если задан COMPOSE_BIN_PATH или по дефолтному пути доступен бинарь
//     compose, прокинутый bind-mount'ом с хоста → исполняем его (это ХОСТОВЫЙ
//     бинарь, версия гарантированно с хоста).
//  2. Иначе пробуем `docker compose version --short` через CLI, найденный в
//     контейнере (если установлен) — но это даст КОНТЕЙНЕРНУЮ версию.
//     Используется только если хостовый bind-mount не настроен.
//  3. Не вышло — пустая строка (поле в JSON будет omitempty).
func detectComposeVersion(ctx context.Context) string {
	cctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	// 1) Бинарь хоста, проброшенный в контейнер (приоритет).
	binPath := strings.TrimSpace(os.Getenv("COMPOSE_BIN_PATH"))
	if binPath == "" {
		binPath = defaultComposeBinPath
	}
	if v := composeVersionFromBinary(cctx, binPath); v != "" {
		return v
	}

	// 2) Fallback через docker CLI (если он установлен В КОНТЕЙНЕРЕ).
	if path, err := exec.LookPath("docker"); err == nil {
		data, err := exec.CommandContext(cctx, path, "compose", "version", "--short").Output()
		if err == nil {
			return strings.TrimSpace(string(data))
		}
	}
	return ""
}

// composeVersionFromBinary вызывает compose-плагин напрямую (без обёртки `docker`).
// Плагин поддерживает `<plugin> version --short` и возвращает чистую версию.
func composeVersionFromBinary(ctx context.Context, binPath string) string {
	if binPath == "" {
		return ""
	}
	info, err := os.Stat(binPath)
	if err != nil || info.IsDir() {
		return ""
	}
	data, err := exec.CommandContext(ctx, binPath, "version", "--short").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}
