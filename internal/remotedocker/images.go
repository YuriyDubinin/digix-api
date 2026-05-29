package remotedocker

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/YuriyDubinin/dijex-api/internal/docker"
)

// CollectImages собирает список образов с удалённого сервера через SSH.
// Возвращает *docker.ImagesInfo — точно тот же тип, что и локальный
// /api/system/images. Фронт переиспользует один компонент.
//
// Команды (один сводный скрипт):
//   - docker version --format '{{json .Server}}' — версия движка
//   - docker info --format '{{json .}}' — общая инфа (счётчики)
//   - docker image inspect $(docker image ls -q --no-trunc) — полные данные
//     по каждому образу (с точными размерами в байтах, в отличие от CLI-формата)
//   - docker ps -a --format '{{.ImageID}}' — для подсчёта Containers на образ
//
// Принципы:
//   - Available=false с Reason, если docker не установлен/не отвечает.
//   - Best-effort: если ps упал — Containers=0, но образы соберутся.
func (c *Collector) CollectImages(ctx context.Context, client *ssh.Client) *docker.ImagesInfo {
	out := &docker.ImagesInfo{
		CollectedAt: time.Now().UTC(),
		Images:      []docker.Image{},
	}

	const script = `printf '===WHICH===\n'
command -v docker 2>/dev/null
printf '\n===VERSION===\n'
docker version --format '{{json .Server}}' 2>/dev/null
printf '\n===INFO===\n'
docker info --format '{{json .}}' 2>/dev/null
printf '\n===IMAGES===\n'
ids=$(docker image ls -q --no-trunc 2>/dev/null)
if [ -n "$ids" ]; then
  docker image inspect $ids 2>/dev/null
else
  printf '[]'
fi
printf '\n===PS_IMAGEIDS===\n'
docker ps -a --no-trunc --format '{{.ImageID}}' 2>/dev/null
printf '\n===END===\n'
`
	output, err := runSSH(ctx, client, script)
	if err != nil && output == "" {
		out.Available = false
		out.Reason = "ssh command failed: " + err.Error()
		return out
	}
	blocks := splitByMarkers(output, []string{
		"===WHICH===", "===VERSION===", "===INFO===", "===IMAGES===", "===PS_IMAGEIDS===", "===END===",
	})

	if strings.TrimSpace(blocks["===WHICH==="]) == "" {
		out.Available = false
		out.Reason = "docker CLI not found on remote host"
		return out
	}

	verRaw := strings.TrimSpace(blocks["===VERSION==="])
	infoRaw := strings.TrimSpace(blocks["===INFO==="])
	if verRaw == "" && infoRaw == "" {
		out.Available = false
		out.Reason = "docker daemon unreachable (version/info empty — check permissions or daemon status)"
		return out
	}
	out.Available = true

	out.Engine = parseEngine(verRaw, infoRaw, &out.Errors)

	// Считаем использования образов: ID контейнера → ID образа.
	containerCounts := countContainerImages(blocks["===PS_IMAGEIDS==="])

	imagesRaw := strings.TrimSpace(blocks["===IMAGES==="])
	if imagesRaw != "" && imagesRaw != "[]" {
		imgs, perr := parseImageInspectArray(imagesRaw, containerCounts)
		if perr != nil {
			out.Errors = append(out.Errors, "parse image inspect: "+perr.Error())
		} else {
			out.Images = imgs
			out.Count = len(imgs)
		}
	}

	// Та же сортировка, что и в локальном collector'е: с тегами выше «висячих».
	sort.SliceStable(out.Images, func(a, b int) bool {
		if out.Images[a].Dangling != out.Images[b].Dangling {
			return !out.Images[a].Dangling
		}
		return imageSortKey(out.Images[a]) < imageSortKey(out.Images[b])
	})

	return out
}

// apiImageInspect — формат одного объекта в массиве `docker image inspect`.
type apiImageInspect struct {
	ID          string   `json:"Id"`     // "sha256:..."
	Parent      string   `json:"Parent"` // "sha256:..." или ""
	RepoTags    []string `json:"RepoTags"`
	RepoDigests []string `json:"RepoDigests"`
	Created     string   `json:"Created"` // RFC3339(Nano)
	Size        int64    `json:"Size"`
	Config      struct {
		Labels map[string]string `json:"Labels"`
	} `json:"Config"`
}

func parseImageInspectArray(raw string, counts map[string]int) ([]docker.Image, error) {
	var items []apiImageInspect
	if err := json.Unmarshal([]byte(raw), &items); err != nil {
		return nil, err
	}
	out := make([]docker.Image, 0, len(items))
	for _, it := range items {
		created, _ := time.Parse(time.RFC3339Nano, it.Created)
		out = append(out, docker.BuildImage(
			it.ID,
			it.Parent,
			it.RepoTags,
			it.RepoDigests,
			created.UTC(),
			it.Size,
			0, // SharedSize не доступен через image inspect — это поле есть только в Engine API /images/json
			it.Config.Labels,
			counts[it.ID],
		))
	}
	return out, nil
}

// countContainerImages читает вывод `docker ps -a --format '{{.ImageID}}'` и
// возвращает map "sha256:abc..." → сколько контейнеров используют этот образ.
//
// CLI ID может быть как с префиксом "sha256:", так и без; нормализуем к
// формату с префиксом — именно так возвращает image inspect (.Id).
func countContainerImages(text string) map[string]int {
	out := make(map[string]int)
	if text == "" {
		return out
	}
	for _, line := range strings.Split(text, "\n") {
		id := strings.TrimSpace(line)
		if id == "" {
			continue
		}
		if !strings.HasPrefix(id, "sha256:") {
			id = "sha256:" + id
		}
		out[id]++
	}
	return out
}

// imageSortKey — стабильный ключ сортировки: первый тег, иначе первый digest, иначе ID.
// Дублирует логику из docker/collector.go, потому что та функция приватная.
func imageSortKey(img docker.Image) string {
	if len(img.RepoTags) > 0 {
		return img.RepoTags[0]
	}
	if len(img.RepoDigests) > 0 {
		return img.RepoDigests[0]
	}
	return img.ID
}
