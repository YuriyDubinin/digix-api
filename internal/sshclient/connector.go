// Package sshclient подключается к удалённым серверам по SSH. Сначала пробует
// аутентификацию нашим ключом приложения (если он уже в authorized_keys сервера),
// при неудаче — по паролю. После входа проверяет сессию выполнением команды
// и best-effort собирает базовые факты о сервере.
package sshclient

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

const (
	dialTimeout    = 8 * time.Second
	sessionTimeout = 6 * time.Second
)

// Статусы. Влезают в servers.last_status VARCHAR(20).
const (
	StatusOK          = "OK"
	StatusAuthFailed  = "AUTH_FAILED"
	StatusUnreachable = "UNREACHABLE"
	StatusTimeout     = "TIMEOUT"
	StatusError       = "ERROR"
)

// KeyProvider отдаёт SSH-signer ключа приложения. Реализуется *sshkey.Manager.
type KeyProvider interface {
	Signer() (ssh.Signer, error)
}

// Target — параметры подключения.
type Target struct {
	Host     string
	Port     int
	User     string
	Password string
}

// Facts — базовая информация, собранная с сервера после входа.
type Facts struct {
	Hostname      string
	OS            string
	KernelVersion string
	Arch          string
	CPUCores      int
	// PublicIP — публичный IP, который видит сам сервер (через api.ipify.org
	// или dig opendns). Пусто, если у сервера нет внешнего интернета и оба
	// способа не сработали. Используется в сервис-слое для resolve страны.
	PublicIP string
}

// Result — итог операции.
type Result struct {
	Connected bool
	Method    string // "publickey" | "password" | ""
	Status    string // OK | AUTH_FAILED | UNREACHABLE | TIMEOUT | ERROR
	Message   string
	Facts     *Facts // только для Connect, best-effort
}

type Connector struct {
	keys KeyProvider
}

func NewConnector(keys KeyProvider) *Connector {
	return &Connector{keys: keys}
}

// Dial открывает SSH-соединение (ключ → пароль). Возвращает живой *ssh.Client
// — caller обязан его закрыть. На неуспех возвращает (nil, "", failResult)
// с осмысленным статусом.
//
// Используется операциями, которым нужно несколько SSH-сессий подряд
// (например, сбор расширенного снимка системы /api/servers/remote/system/main).
func (c *Connector) Dial(ctx context.Context, t Target) (*ssh.Client, string, Result) {
	return c.establish(ctx, t)
}

// Connect: вход (ключ → пароль), проверка сессии, сбор фактов.
func (c *Connector) Connect(ctx context.Context, t Target) Result {
	client, method, failRes := c.establish(ctx, t)
	if client == nil {
		return failRes
	}
	defer client.Close()

	// «Пинг» сессии: убеждаемся, что реально можем выполнять команды.
	if err := verifySession(client); err != nil {
		return Result{Connected: false, Method: method, Status: StatusError, Message: "session check failed: " + err.Error()}
	}

	res := Result{Connected: true, Method: method, Status: StatusOK, Message: "connected via " + method}
	if facts, err := collectFacts(client); err == nil {
		res.Facts = facts // best-effort: ошибка сбора фактов не валит коннект
	}
	return res
}

// InstallResult — итог установки публичного ключа на удалённый сервер.
type InstallResult struct {
	Connected        bool   // удалось залогиниться по паролю
	AlreadyInstalled bool   // ключ уже был в authorized_keys
	Installed        bool   // ключ дописали в authorized_keys
	Verified         bool   // повторный коннект по ключу прошёл (значит точно работает)
	Status           string // OK | AUTH_FAILED | UNREACHABLE | TIMEOUT | ERROR
	Message          string
}

// InstallPublicKey заходит на сервер ТОЛЬКО ПО ПАРОЛЮ (ключа на сервере ещё нет —
// в этом весь смысл), идемпотентно добавляет publicKey в ~/.ssh/authorized_keys,
// затем разрывает соединение и проверяет, что вход по нашему ключу теперь работает.
//
// publicKey — строка вида "ssh-ed25519 AAAA... comment" (как из sshkey.Manager.Check).
func (c *Connector) InstallPublicKey(ctx context.Context, t Target, publicKey string) InstallResult {
	publicKey = strings.TrimSpace(publicKey)
	if publicKey == "" {
		return InstallResult{Status: StatusError, Message: "public key is empty"}
	}
	if t.Password == "" {
		return InstallResult{Status: StatusAuthFailed, Message: "password is required to install ssh key"}
	}

	user := t.User
	if user == "" {
		user = "root"
	}
	addr := net.JoinHostPort(t.Host, strconv.Itoa(t.Port))

	// 1) Логин по паролю (force).
	client, err := dial(ctx, addr, user, []ssh.AuthMethod{ssh.Password(t.Password)})
	if err != nil {
		r := classify(err)
		return InstallResult{Status: r.Status, Message: r.Message}
	}

	// 2) Установка ключа через sh-скрипт. Идемпотентно: если ключ уже там — не добавляет.
	res := InstallResult{Connected: true}
	out, ierr := runInstallScript(client, publicKey)
	_ = client.Close()
	if ierr != nil {
		return InstallResult{Connected: true, Status: StatusError, Message: "install command failed: " + ierr.Error()}
	}
	switch {
	case strings.Contains(out, "ALREADY_INSTALLED"):
		res.AlreadyInstalled = true
	case strings.Contains(out, "INSTALLED"):
		res.Installed = true
	default:
		return InstallResult{Connected: true, Status: StatusError, Message: "unexpected install output: " + out}
	}

	// 3) Верификация: переподключаемся ТОЛЬКО ПО КЛЮЧУ. Если проходит —
	// ключ реально работает, можно безопасно ставить флаг в БД.
	res.Verified = c.verifyKeyAuth(ctx, addr, user)

	res.Status = StatusOK
	switch {
	case res.AlreadyInstalled && res.Verified:
		res.Message = "key was already installed and verified"
	case res.AlreadyInstalled && !res.Verified:
		res.Message = "key was already in authorized_keys but key auth still fails (check remote sshd config or ~/.ssh permissions)"
	case res.Installed && res.Verified:
		res.Message = "key installed and verified"
	case res.Installed && !res.Verified:
		res.Message = "key appended to authorized_keys but key auth verification failed"
	}
	return res
}

// verifyKeyAuth пытается зайти на сервер только по приватному ключу из KeyProvider.
func (c *Connector) verifyKeyAuth(ctx context.Context, addr, user string) bool {
	if c.keys == nil {
		return false
	}
	signer, err := c.keys.Signer()
	if err != nil || signer == nil {
		return false
	}
	client, err := dial(ctx, addr, user, []ssh.AuthMethod{ssh.PublicKeys(signer)})
	if err != nil {
		return false
	}
	defer client.Close()
	return verifySession(client) == nil
}

// runInstallScript выполняет скрипт идемпотентной установки публичного ключа.
// Тело ключа (algo+base64) — это первые два поля строки; именно по ним проверяем
// дубликат, чтобы разные комментарии у одного и того же ключа не плодили строки.
func runInstallScript(client *ssh.Client, publicKey string) (string, error) {
	parts := strings.Fields(publicKey)
	if len(parts) < 2 {
		return "", fmt.Errorf("public key does not look like an authorized_keys line")
	}
	keyBody := parts[0] + " " + parts[1]

	script := fmt.Sprintf(`set -e
umask 077
mkdir -p "$HOME/.ssh"
chmod 700 "$HOME/.ssh"
touch "$HOME/.ssh/authorized_keys"
chmod 600 "$HOME/.ssh/authorized_keys"
if grep -qF -- '%s' "$HOME/.ssh/authorized_keys"; then
    printf 'ALREADY_INSTALLED\n'
else
    printf '%%s\n' '%s' >> "$HOME/.ssh/authorized_keys"
    printf 'INSTALLED\n'
fi
`, keyBody, publicKey)

	sess, err := client.NewSession()
	if err != nil {
		return "", err
	}
	defer sess.Close()
	out, err := sess.CombinedOutput(script)
	return strings.TrimSpace(string(out)), err
}

// Ping: лёгкая проверка SSH-соединения (вход + проверочная команда).
func (c *Connector) Ping(ctx context.Context, t Target) Result {
	client, method, failRes := c.establish(ctx, t)
	if client == nil {
		return failRes
	}
	defer client.Close()

	if err := verifySession(client); err != nil {
		return Result{Connected: false, Method: method, Status: StatusError, Message: "session check failed: " + err.Error()}
	}
	return Result{Connected: true, Method: method, Status: StatusOK, Message: "ssh connection alive via " + method}
}

// establish пробует аутентификацию ключом, затем паролем.
// Возвращает (client, method, _) при успехе или (nil, "", failResult).
func (c *Connector) establish(ctx context.Context, t Target) (*ssh.Client, string, Result) {
	user := t.User
	if user == "" {
		user = "root"
	}
	addr := net.JoinHostPort(t.Host, strconv.Itoa(t.Port))

	// 1) Аутентификация нашим ключом (если он уже на сервере).
	if c.keys != nil {
		if signer, err := c.keys.Signer(); err == nil && signer != nil {
			client, derr := dial(ctx, addr, user, []ssh.AuthMethod{ssh.PublicKeys(signer)})
			if derr == nil {
				return client, "publickey", Result{}
			}
			// Сетевая проблема — пароль не поможет, выходим сразу.
			if isNetworkError(derr) {
				return nil, "", classify(derr)
			}
			// Иначе ключ не принят — пробуем пароль.
		}
	}

	// 2) Аутентификация паролем.
	if t.Password != "" {
		client, derr := dial(ctx, addr, user, []ssh.AuthMethod{ssh.Password(t.Password)})
		if derr == nil {
			return client, "password", Result{}
		}
		return nil, "", classify(derr)
	}

	// Ключ не подошёл, пароля нет.
	return nil, "", Result{
		Connected: false,
		Status:    StatusAuthFailed,
		Message:   "key authentication failed and no password provided",
	}
}

func dial(ctx context.Context, addr, user string, auth []ssh.AuthMethod) (*ssh.Client, error) {
	cfg := &ssh.ClientConfig{
		User: user,
		Auth: auth,
		// Хост-ключ не верифицируем: это инструмент управления своими серверами.
		// TODO: при необходимости — TOFU (запоминать host key при первом коннекте).
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), //nolint:gosec
		Timeout:         dialTimeout,
	}

	d := net.Dialer{Timeout: dialTimeout}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}

	// Дедлайн на сам SSH-handshake.
	_ = conn.SetDeadline(time.Now().Add(dialTimeout))
	sshConn, chans, reqs, err := ssh.NewClientConn(conn, addr, cfg)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	_ = conn.SetDeadline(time.Time{}) // снять дедлайн с установленного соединения
	return ssh.NewClient(sshConn, chans, reqs), nil
}

func verifySession(client *ssh.Client) error {
	sess, err := client.NewSession()
	if err != nil {
		return err
	}
	defer sess.Close()
	return sess.Run("true")
}

func collectFacts(client *ssh.Client) (*Facts, error) {
	sess, err := client.NewSession()
	if err != nil {
		return nil, err
	}
	defer sess.Close()

	// Одной сессией собираем всё; для публичного IP пробуем curl, потом dig,
	// потом пустая строка — best-effort, не должен валить весь блок фактов.
	const script = `hostname
uname -s
uname -r
uname -m
(nproc 2>/dev/null || echo 0)
(curl -fsS --max-time 2 https://api.ipify.org 2>/dev/null \
  || dig +short +time=1 +tries=1 myip.opendns.com @resolver1.opendns.com 2>/dev/null \
  || echo "")`
	out, err := sess.Output(script)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	get := func(i int) string {
		if i < len(lines) {
			return strings.TrimSpace(lines[i])
		}
		return ""
	}
	cores, _ := strconv.Atoi(get(4))
	publicIP := get(5)
	// Валидация: пустая строка/мусор — оставляем пусто, дальше service просто не резолвит.
	if publicIP != "" && net.ParseIP(publicIP) == nil {
		publicIP = ""
	}
	return &Facts{
		Hostname:      get(0),
		OS:            get(1),
		KernelVersion: get(2),
		Arch:          get(3),
		CPUCores:      cores,
		PublicIP:      publicIP,
	}, nil
}

func isNetworkError(err error) bool {
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	s := err.Error()
	for _, m := range []string{"connection refused", "no such host", "no route to host", "network is unreachable", "i/o timeout"} {
		if strings.Contains(s, m) {
			return true
		}
	}
	return false
}

func classify(err error) Result {
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return Result{Connected: false, Status: StatusTimeout, Message: "connection timed out"}
	}
	s := err.Error()
	switch {
	case strings.Contains(s, "unable to authenticate"),
		strings.Contains(s, "no supported methods remain"),
		strings.Contains(s, "permission denied"),
		strings.Contains(s, "handshake failed"):
		return Result{Connected: false, Status: StatusAuthFailed, Message: "authentication failed"}
	case isNetworkError(err):
		return Result{Connected: false, Status: StatusUnreachable, Message: s}
	default:
		return Result{Connected: false, Status: StatusError, Message: s}
	}
}
