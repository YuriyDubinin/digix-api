package registryclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"

	"golang.org/x/sync/errgroup"
)

const (
	hubAPIBase          = "https://hub.docker.com"
	maxReposToEnrich    = 100 // потолок на число репозиториев в одном ответе
	tagFetchConcurrency = 6   // параллелизм при дозагрузке тегов
	maxTagsPerImage     = 100 // потолок тегов на образ
)

// Ошибки листинга. Сервис/handler маппят их на коды ответа.
var (
	ErrListUnreachable = errors.New("registry unreachable")
	ErrListAuth        = errors.New("registry authentication failed")
	ErrListUnsupported = errors.New("image listing not supported for this registry")
)

// ListTarget — что листим. Namespace для DockerHub: username/организация.
type ListTarget struct {
	Type      string
	URL       string
	Username  string
	Password  string
	Insecure  bool
	Namespace string
}

// ImageList — результат листинга.
type ImageList struct {
	Source    string  // "hub_api" | "registry_v2"
	Namespace string  // фактически использованный namespace (для DockerHub)
	Total     int     // общее число образов по данным реестра (может быть > len(Images))
	Images    []Image
}

// Image — образ (репозиторий) с тегами и метаданными.
// Поля-указатели заполняются только для DockerHub (omitempty в JSON).
type Image struct {
	Name        string
	Tags        []string
	TagCount    int
	Description string
	IsPrivate   *bool
	PullCount   *int64
	StarCount   *int64
	LastUpdated string
}

// ListImages выбирает поток листинга по типу реестра.
func (c *Checker) ListImages(ctx context.Context, t ListTarget) (ImageList, error) {
	switch strings.ToUpper(strings.TrimSpace(t.Type)) {
	case "DOCKERHUB":
		return c.listDockerHub(ctx, t)
	default:
		return c.listV2(ctx, t)
	}
}

// ───────────────────────── DockerHub (Hub API) ─────────────────────────

func (c *Checker) listDockerHub(ctx context.Context, t ListTarget) (ImageList, error) {
	cl := c.client(t.Insecure)

	// namespace определяет сервис (namespace записи / username / Docker ID).
	// На t.Username не опираемся: там логин аккаунта (часто email), не namespace.
	namespace := strings.TrimSpace(t.Namespace)
	if namespace == "" {
		return ImageList{}, fmt.Errorf("%w: namespace required for DockerHub", ErrListUnsupported)
	}

	// Логин (если есть креды) — нужен для приватных репозиториев.
	var jwt string
	if t.Username != "" && t.Password != "" {
		token, err := dockerHubLogin(ctx, cl, t.Username, t.Password)
		if err != nil {
			return ImageList{}, err
		}
		jwt = token
	}

	reposURL := fmt.Sprintf("%s/v2/repositories/%s/?page_size=%d", hubAPIBase, url.PathEscape(namespace), maxReposToEnrich)
	total, repos, err := dockerHubGetRepos(ctx, cl, reposURL, jwt)
	if err != nil {
		return ImageList{}, err
	}

	// Дозагрузка тегов по каждому репозиторию (параллельно, с лимитом).
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(tagFetchConcurrency)
	var mu sync.Mutex
	for i := range repos {
		i := i
		repo := repos[i].Name
		g.Go(func() error {
			tagsURL := fmt.Sprintf("%s/v2/repositories/%s/%s/tags/?page_size=%d",
				hubAPIBase, url.PathEscape(namespace), url.PathEscape(repo), maxTagsPerImage)
			tags := dockerHubGetTags(gctx, cl, tagsURL, jwt) // best-effort
			mu.Lock()
			repos[i].Tags = tags
			repos[i].TagCount = len(tags)
			mu.Unlock()
			return nil
		})
	}
	_ = g.Wait()

	return ImageList{Source: "hub_api", Namespace: namespace, Total: total, Images: repos}, nil
}

func dockerHubLogin(ctx context.Context, cl *http.Client, user, pass string) (string, error) {
	body, _ := json.Marshal(map[string]string{"username": user, "password": pass})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, hubAPIBase+"/v2/users/login", strings.NewReader(string(body)))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := cl.Do(req)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrListUnreachable, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return "", fmt.Errorf("%w: invalid Docker Hub credentials", ErrListAuth)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%w: login returned %d", ErrListAuth, resp.StatusCode)
	}

	var payload struct {
		Token string `json:"token"`
	}
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if err := json.Unmarshal(raw, &payload); err != nil || payload.Token == "" {
		return "", fmt.Errorf("%w: empty token from login", ErrListAuth)
	}
	return payload.Token, nil
}

func dockerHubGetRepos(ctx context.Context, cl *http.Client, u, jwt string) (int, []Image, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return 0, nil, err
	}
	if jwt != "" {
		req.Header.Set("Authorization", "JWT "+jwt)
	}

	resp, err := cl.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("%w: %v", ErrListUnreachable, err)
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return 0, nil, fmt.Errorf("%w: cannot list repositories (status %d)", ErrListAuth, resp.StatusCode)
	case resp.StatusCode == http.StatusNotFound:
		// Namespace не существует / приватный без прав — отдаём пустой список.
		return 0, []Image{}, nil
	case resp.StatusCode != http.StatusOK:
		return 0, nil, fmt.Errorf("%w: repositories returned %d", ErrListUnreachable, resp.StatusCode)
	}

	var payload struct {
		Count   int `json:"count"`
		Results []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			IsPrivate   bool   `json:"is_private"`
			StarCount   int64  `json:"star_count"`
			PullCount   int64  `json:"pull_count"`
			LastUpdated string `json:"last_updated"`
		} `json:"results"`
	}
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err := json.Unmarshal(raw, &payload); err != nil {
		return 0, nil, fmt.Errorf("decode repositories: %w", err)
	}

	images := make([]Image, 0, len(payload.Results))
	for _, r := range payload.Results {
		isPriv := r.IsPrivate
		star := r.StarCount
		pull := r.PullCount
		images = append(images, Image{
			Name:        r.Name,
			Description: r.Description,
			IsPrivate:   &isPriv,
			StarCount:   &star,
			PullCount:   &pull,
			LastUpdated: r.LastUpdated,
		})
	}
	return payload.Count, images, nil
}

func dockerHubGetTags(ctx context.Context, cl *http.Client, u, jwt string) []string {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil
	}
	if jwt != "" {
		req.Header.Set("Authorization", "JWT "+jwt)
	}
	resp, err := cl.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	var payload struct {
		Results []struct {
			Name string `json:"name"`
		} `json:"results"`
	}
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil
	}
	tags := make([]string, 0, len(payload.Results))
	for _, t := range payload.Results {
		tags = append(tags, t.Name)
	}
	return tags
}

// ───────────────────────── Generic Registry V2 ─────────────────────────

func (c *Checker) listV2(ctx context.Context, t ListTarget) (ImageList, error) {
	cl := c.client(t.Insecure)
	base := strings.TrimRight(strings.TrimSpace(t.URL), "/")

	resp, err := c.authedGet(ctx, cl, base+"/v2/_catalog?n="+fmt.Sprint(maxReposToEnrich), t)
	if err != nil {
		return ImageList{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			return ImageList{}, fmt.Errorf("%w: catalog returned %d", ErrListAuth, resp.StatusCode)
		}
		return ImageList{}, fmt.Errorf("%w: catalog returned %d (registry may not support _catalog)", ErrListUnsupported, resp.StatusCode)
	}

	var payload struct {
		Repositories []string `json:"repositories"`
	}
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err := json.Unmarshal(raw, &payload); err != nil {
		return ImageList{}, fmt.Errorf("decode catalog: %w", err)
	}

	repos := payload.Repositories
	if len(repos) > maxReposToEnrich {
		repos = repos[:maxReposToEnrich]
	}

	images := make([]Image, len(repos))
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(tagFetchConcurrency)
	for i := range repos {
		i := i
		name := repos[i]
		g.Go(func() error {
			tags := c.v2Tags(gctx, cl, base, name, t)
			images[i] = Image{Name: name, Tags: tags, TagCount: len(tags)}
			return nil
		})
	}
	_ = g.Wait()

	sort.Slice(images, func(a, b int) bool { return images[a].Name < images[b].Name })
	return ImageList{Source: "registry_v2", Namespace: t.Namespace, Total: len(payload.Repositories), Images: images}, nil
}

func (c *Checker) v2Tags(ctx context.Context, cl *http.Client, base, repo string, t ListTarget) []string {
	resp, err := c.authedGet(ctx, cl, base+"/v2/"+repo+"/tags/list", t)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	var payload struct {
		Tags []string `json:"tags"`
	}
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil
	}
	if len(payload.Tags) > maxTagsPerImage {
		payload.Tags = payload.Tags[:maxTagsPerImage]
	}
	return payload.Tags
}

// authedGet делает GET и, если реестр ответил 401, проходит auth-handshake
// (Bearer-токен под scope из Www-Authenticate, либо Basic), затем повторяет запрос.
func (c *Checker) authedGet(ctx context.Context, cl *http.Client, u string, t ListTarget) (*http.Response, error) {
	resp, err := c.get(ctx, cl, u, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrListUnreachable, err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		return resp, nil
	}

	wwwAuth := resp.Header.Get("Www-Authenticate")
	drain(resp)
	scheme, params := parseWWWAuthenticate(wwwAuth)

	switch strings.ToLower(scheme) {
	case "bearer":
		token, err := c.fetchToken(ctx, cl, params, Target{Username: t.Username, Password: t.Password})
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrListAuth, err)
		}
		resp2, err := c.get(ctx, cl, u, map[string]string{"Authorization": "Bearer " + token})
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrListUnreachable, err)
		}
		return resp2, nil
	case "basic":
		resp2, err := c.getBasic(ctx, cl, u, t.Username, t.Password)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrListUnreachable, err)
		}
		return resp2, nil
	default:
		return nil, fmt.Errorf("%w: unsupported auth scheme %q", ErrListAuth, scheme)
	}
}
