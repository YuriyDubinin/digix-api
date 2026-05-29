package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// defaultSocketPath — путь по умолчанию, если DOCKER_HOST не задан/не unix.
const defaultSocketPath = "/var/run/docker.sock"

// client — тонкая обёртка над Docker Engine API по unix-сокету.
// Запросы идут без версии в пути (unversioned) — демон использует свою
// максимальную поддерживаемую версию, что исключает ошибки "client too new".
type client struct {
	http       *http.Client
	socketPath string
}

// newClient разбирает host вида "unix:///var/run/docker.sock" и собирает
// HTTP-клиент с unix-dialer'ом. TCP не поддерживается — для не-unix host'а
// откатываемся на сокет по умолчанию.
func newClient(host string) *client {
	socket := defaultSocketPath
	if strings.HasPrefix(host, "unix://") {
		if p := strings.TrimPrefix(host, "unix://"); p != "" {
			socket = p
		}
	}

	httpClient := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, "unix", socket)
			},
			DisableCompression: true,
		},
	}

	return &client{http: httpClient, socketPath: socket}
}

// getJSON выполняет GET к Engine API и декодирует JSON-ответ в out.
// Host в URL ("docker") игнорируется (соединение идёт по сокету), но обязан
// присутствовать для валидности URL.
func (c *client) getJSON(ctx context.Context, path string, query url.Values, out any) error {
	u := url.URL{Scheme: "http", Host: "docker", Path: path}
	if query != nil {
		u.RawQuery = query.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return fmt.Errorf("docker: build request %s: %w", path, err)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("docker: request %s: %w", path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("docker: %s returned %d: %s", path, resp.StatusCode, strings.TrimSpace(string(body)))
	}

	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("docker: decode %s: %w", path, err)
	}
	return nil
}

// ping проверяет доступность демона. /_ping отдаёт "OK" текстом, не JSON.
func (c *client) ping(ctx context.Context) error {
	pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	return c.getJSON(pingCtx, "/_ping", nil, nil)
}

func (c *client) version(ctx context.Context) (*apiVersion, error) {
	var v apiVersion
	if err := c.getJSON(ctx, "/version", nil, &v); err != nil {
		return nil, err
	}
	return &v, nil
}

func (c *client) info(ctx context.Context) (*apiInfo, error) {
	var i apiInfo
	if err := c.getJSON(ctx, "/info", nil, &i); err != nil {
		return nil, err
	}
	return &i, nil
}

func (c *client) listContainers(ctx context.Context) ([]apiListItem, error) {
	q := url.Values{}
	q.Set("all", "true")  // включая остановленные
	q.Set("size", "true") // SizeRw / SizeRootFs
	var items []apiListItem
	if err := c.getJSON(ctx, "/containers/json", q, &items); err != nil {
		return nil, err
	}
	return items, nil
}

func (c *client) inspectContainer(ctx context.Context, id string) (*apiInspect, error) {
	var ins apiInspect
	if err := c.getJSON(ctx, "/containers/"+id+"/json", nil, &ins); err != nil {
		return nil, err
	}
	return &ins, nil
}

// listImages вызывает Engine API /images/json?digests=1 — там сразу есть
// RepoDigests, Containers (счётчик использующих образ контейнеров) и
// SharedSize, поэтому отдельные inspect-запросы не нужны.
func (c *client) listImages(ctx context.Context) ([]apiImageItem, error) {
	q := url.Values{}
	q.Set("digests", "1")
	q.Set("all", "false") // промежуточные слои не показываем
	var items []apiImageItem
	if err := c.getJSON(ctx, "/images/json", q, &items); err != nil {
		return nil, err
	}
	return items, nil
}
