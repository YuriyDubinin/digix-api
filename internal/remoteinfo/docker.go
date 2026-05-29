package remoteinfo

import (
	"context"
	"encoding/json"
	"strings"

	"golang.org/x/crypto/ssh"
)

// collectDocker best-effort собирает версии docker engine и docker compose
// на удалённом сервере. Если docker не установлен — секция остаётся пустой
// (это не ошибка). Не требует прав root, если пользователь в группе `docker`.
func (c *Collector) collectDocker(ctx context.Context, client *ssh.Client) DockerVersions {
	// printf '\n===<NAME>===\n' с ведущим \n — единый защитный стиль (см. host.go).
	const script = `printf '===ENGINE===\n'
docker version --format '{{json .Server}}' 2>/dev/null
printf '\n===COMPOSE===\n'
docker compose version --short 2>/dev/null || docker-compose version --short 2>/dev/null
printf '\n===END===\n'
`
	out, err := runOutput(ctx, client, script)
	if err != nil && out == "" {
		return DockerVersions{}
	}
	blocks := splitByMarkers(out, []string{
		"===ENGINE===", "===COMPOSE===", "===END===",
	})

	versions := DockerVersions{
		Compose: firstLine(blocks["===COMPOSE==="]),
	}

	// {"Platform":{"Name":"Docker Engine - Community"},
	//  "Components":[{"Name":"Engine","Version":"27.1.1","Details":{"ApiVersion":"1.46",...}},...],
	//  "Version":"27.1.1","ApiVersion":"1.46",...}
	var srv struct {
		Version    string `json:"Version"`
		APIVersion string `json:"ApiVersion"`
		Components []struct {
			Name    string `json:"Name"`
			Version string `json:"Version"`
			Details struct {
				APIVersion string `json:"ApiVersion"`
			} `json:"Details"`
		} `json:"Components"`
	}
	engineRaw := strings.TrimSpace(blocks["===ENGINE==="])
	if engineRaw != "" {
		_ = json.Unmarshal([]byte(engineRaw), &srv)
	}

	versions.Engine = srv.Version
	versions.EngineAPI = srv.APIVersion
	// Если на верхнем уровне нет — берём из первого Component=Engine.
	if versions.Engine == "" || versions.EngineAPI == "" {
		for _, comp := range srv.Components {
			if !strings.EqualFold(comp.Name, "Engine") {
				continue
			}
			if versions.Engine == "" {
				versions.Engine = comp.Version
			}
			if versions.EngineAPI == "" {
				versions.EngineAPI = comp.Details.APIVersion
			}
			break
		}
	}

	return versions
}
