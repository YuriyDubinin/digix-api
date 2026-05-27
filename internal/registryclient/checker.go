// Package registryclient проверяет подключение к Docker Registry (V2 API).
// Делает ping /v2/ и, при необходимости, проходит auth-handshake (Bearer-токен
// или Basic), чтобы убедиться, что креды валидны и реестр доступен.
//
// Не тащит docker SDK — общение по обычному HTTP через stdlib.
package registryclient

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

// Статусы результата проверки. Влезают в registries.last_status VARCHAR(20).
const (
	StatusOK          = "OK"          // реестр доступен (и авторизация прошла, если были креды)
	StatusAuthFailed  = "AUTH_FAILED" // реестр доступен, но логин/пароль не подошли
	StatusUnreachable = "UNREACHABLE" // не достучались (сеть/таймаут/DNS)
	StatusTLSError    = "TLS_ERROR"   // проблема с TLS-сертификатом
	StatusError       = "ERROR"       // прочая ошибка (неожиданный ответ реестра)
)

// Target — что проверяем. Username/Password пустые → анонимный доступ.
// Для DockerHub Username — это идентификатор аккаунта (email или Docker ID).
type Target struct {
	Type     string // DOCKERHUB → логин через Hub API; иначе → Registry V2
	URL      string
	Username string
	Password string
	Insecure bool // разрешить self-signed / пропустить проверку TLS
}

func (t Target) hasCreds() bool { return t.Username != "" || t.Password != "" }

// Result — итог проверки.
type Result struct {
	Connected     bool
	Authenticated bool   // была ли пройдена авторизация (false для анонимного)
	Status        string // один из Status*
	Message       string
	APIVersion    string // из заголовка Docker-Distribution-Api-Version
}

type Checker struct {
	secure   *http.Client
	insecure *http.Client
}

func NewChecker() *Checker {
	mk := func(skipTLS bool) *http.Client {
		return &http.Client{
			Transport: &http.Transport{
				DialContext:         (&net.Dialer{Timeout: 5 * time.Second}).DialContext,
				TLSHandshakeTimeout: 5 * time.Second,
				TLSClientConfig:     &tls.Config{InsecureSkipVerify: skipTLS}, //nolint:gosec // осознанно для insecure-реестров
				DisableKeepAlives:   true,
			},
		}
	}
	return &Checker{secure: mk(false), insecure: mk(true)}
}

func (c *Checker) client(insecure bool) *http.Client {
	if insecure {
		return c.insecure
	}
	return c.secure
}

// Check проверяет подключение. Для DockerHub — логин в аккаунт через Hub API
// (та же авторизация, что нужна для листинга образов). Для остальных — Registry V2.
func (c *Checker) Check(ctx context.Context, t Target) Result {
	if strings.ToUpper(strings.TrimSpace(t.Type)) == "DOCKERHUB" {
		return c.checkDockerHub(ctx, t)
	}
	return c.checkV2(ctx, t)
}

// checkDockerHub проверяет креды аккаунта Docker Hub через /v2/users/login.
func (c *Checker) checkDockerHub(ctx context.Context, t Target) Result {
	cl := c.client(t.Insecure)

	// Без кредов аккаунт проверить нельзя — проверяем лишь доступность Hub.
	if t.Username == "" || t.Password == "" {
		resp, err := c.get(ctx, cl, hubAPIBase+"/v2/", nil)
		if err != nil {
			return classifyError(err)
		}
		drain(resp)
		return Result{Connected: true, Authenticated: false, Status: StatusOK, Message: "docker hub reachable (no credentials)"}
	}

	if _, err := dockerHubLogin(ctx, cl, t.Username, t.Password); err != nil {
		switch {
		case errors.Is(err, ErrListAuth):
			return Result{Connected: false, Authenticated: false, Status: StatusAuthFailed, Message: "docker hub login failed: invalid credentials"}
		case errors.Is(err, ErrListUnreachable):
			return Result{Connected: false, Status: StatusUnreachable, Message: err.Error()}
		default:
			return Result{Connected: false, Status: StatusError, Message: err.Error()}
		}
	}
	return Result{Connected: true, Authenticated: true, Status: StatusOK, Message: "docker hub account authenticated"}
}

// checkV2 выполняет ping /v2/ и, если требуется, auth-handshake (для не-DockerHub).
func (c *Checker) checkV2(ctx context.Context, t Target) Result {
	cl := c.client(t.Insecure)
	base := strings.TrimRight(strings.TrimSpace(t.URL), "/")
	pingURL := base + "/v2/"

	resp, err := c.get(ctx, cl, pingURL, nil)
	if err != nil {
		return classifyError(err)
	}
	apiVer := resp.Header.Get("Docker-Distribution-Api-Version")
	drain(resp)

	switch resp.StatusCode {
	case http.StatusOK:
		return Result{Connected: true, Authenticated: t.hasCreds(), Status: StatusOK, APIVersion: apiVer, Message: "registry reachable"}
	case http.StatusUnauthorized:
		return c.handleAuth(ctx, cl, pingURL, resp.Header.Get("Www-Authenticate"), t, apiVer)
	default:
		return Result{Connected: false, Status: StatusError, Message: fmt.Sprintf("unexpected status %d from %s", resp.StatusCode, pingURL)}
	}
}

// handleAuth разбирает заголовок Www-Authenticate и проходит соответствующий поток.
func (c *Checker) handleAuth(ctx context.Context, cl *http.Client, pingURL, wwwAuth string, t Target, apiVer string) Result {
	scheme, params := parseWWWAuthenticate(wwwAuth)

	switch strings.ToLower(scheme) {
	case "bearer":
		token, err := c.fetchToken(ctx, cl, params, t)
		if err != nil {
			return Result{Connected: false, Status: StatusAuthFailed, Message: "token request failed: " + err.Error()}
		}
		resp, err := c.get(ctx, cl, pingURL, map[string]string{"Authorization": "Bearer " + token})
		if err != nil {
			return classifyError(err)
		}
		drain(resp)
		if resp.StatusCode == http.StatusOK {
			return Result{Connected: true, Authenticated: t.hasCreds(), Status: StatusOK, APIVersion: apiVer, Message: "registry reachable"}
		}
		return Result{Connected: false, Status: StatusAuthFailed, Message: fmt.Sprintf("authentication rejected (status %d)", resp.StatusCode)}

	case "basic":
		if !t.hasCreds() {
			return Result{Connected: false, Status: StatusAuthFailed, Message: "registry requires credentials (none provided)"}
		}
		resp, err := c.getBasic(ctx, cl, pingURL, t.Username, t.Password)
		if err != nil {
			return classifyError(err)
		}
		drain(resp)
		if resp.StatusCode == http.StatusOK {
			return Result{Connected: true, Authenticated: true, Status: StatusOK, APIVersion: apiVer, Message: "registry reachable"}
		}
		return Result{Connected: false, Status: StatusAuthFailed, Message: fmt.Sprintf("authentication rejected (status %d)", resp.StatusCode)}

	default:
		return Result{Connected: false, Status: StatusAuthFailed, Message: "unsupported auth scheme: " + scheme}
	}
}

// fetchToken запрашивает Bearer-токен у realm. Если заданы креды — basic-аутентификация
// (иначе анонимный токен, как у DockerHub для публичных операций).
func (c *Checker) fetchToken(ctx context.Context, cl *http.Client, params map[string]string, t Target) (string, error) {
	realm := params["realm"]
	if realm == "" {
		return "", errors.New("no realm in Www-Authenticate")
	}

	u := realm
	q := []string{}
	if s := params["service"]; s != "" {
		q = append(q, "service="+urlQueryEscape(s))
	}
	if s := params["scope"]; s != "" {
		q = append(q, "scope="+urlQueryEscape(s))
	}
	if len(q) > 0 {
		sep := "?"
		if strings.Contains(realm, "?") {
			sep = "&"
		}
		u = realm + sep + strings.Join(q, "&")
	}

	var resp *http.Response
	var err error
	if t.hasCreds() {
		resp, err = c.getBasic(ctx, cl, u, t.Username, t.Password)
	} else {
		resp, err = c.get(ctx, cl, u, nil)
	}
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("token endpoint returned %d", resp.StatusCode)
	}

	var payload struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", fmt.Errorf("decode token response: %w", err)
	}
	if payload.Token != "" {
		return payload.Token, nil
	}
	if payload.AccessToken != "" {
		return payload.AccessToken, nil
	}
	return "", errors.New("empty token in response")
}

// ───────────────────────────── helpers ─────────────────────────────

func (c *Checker) get(ctx context.Context, cl *http.Client, url string, headers map[string]string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	return cl.Do(req)
}

func (c *Checker) getBasic(ctx context.Context, cl *http.Client, url, user, pass string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(user, pass)
	return cl.Do(req)
}

// drain дочитывает и закрывает тело, чтобы соединение можно было переиспользовать/закрыть.
func drain(resp *http.Response) {
	if resp == nil || resp.Body == nil {
		return
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	_ = resp.Body.Close()
}

func classifyError(err error) Result {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return Result{Connected: false, Status: StatusUnreachable, Message: "connection timed out"}
	}
	var (
		ua   x509.UnknownAuthorityError
		ci   x509.CertificateInvalidError
		host x509.HostnameError
	)
	if errors.As(err, &ua) || errors.As(err, &ci) || errors.As(err, &host) {
		return Result{Connected: false, Status: StatusTLSError, Message: "TLS certificate error: " + err.Error()}
	}
	if strings.Contains(err.Error(), "x509") || strings.Contains(err.Error(), "tls:") || strings.Contains(err.Error(), "certificate") {
		return Result{Connected: false, Status: StatusTLSError, Message: err.Error()}
	}
	return Result{Connected: false, Status: StatusUnreachable, Message: err.Error()}
}

// parseWWWAuthenticate разбирает заголовок вида:
//
//	Bearer realm="https://auth.docker.io/token",service="registry.docker.io",scope="..."
//
// Возвращает схему ("Bearer"/"Basic") и map параметров.
func parseWWWAuthenticate(header string) (scheme string, params map[string]string) {
	params = map[string]string{}
	header = strings.TrimSpace(header)
	if header == "" {
		return "", params
	}

	parts := strings.SplitN(header, " ", 2)
	scheme = parts[0]
	if len(parts) < 2 {
		return scheme, params
	}

	// Разбираем key="value", key="value", ...
	for _, kv := range splitParams(parts[1]) {
		eq := strings.IndexByte(kv, '=')
		if eq < 0 {
			continue
		}
		key := strings.TrimSpace(kv[:eq])
		val := strings.TrimSpace(kv[eq+1:])
		val = strings.Trim(val, `"`)
		params[key] = val
	}
	return scheme, params
}

// splitParams делит строку параметров по запятым, не разрезая внутри кавычек.
func splitParams(s string) []string {
	var out []string
	var b strings.Builder
	inQuotes := false
	for _, r := range s {
		switch r {
		case '"':
			inQuotes = !inQuotes
			b.WriteRune(r)
		case ',':
			if inQuotes {
				b.WriteRune(r)
			} else {
				out = append(out, b.String())
				b.Reset()
			}
		default:
			b.WriteRune(r)
		}
	}
	if b.Len() > 0 {
		out = append(out, b.String())
	}
	return out
}

// urlQueryEscape — минимальный escape для query-значений (пробелы и спецсимволы
// в scope/service встречаются редко, но подстрахуемся).
func urlQueryEscape(s string) string {
	repl := strings.NewReplacer(" ", "%20", "\"", "%22", "#", "%23", "&", "%26")
	return repl.Replace(s)
}
