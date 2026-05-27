// Package sshclient подключается к удалённым серверам по SSH. Сначала пробует
// аутентификацию нашим ключом приложения (если он уже в authorized_keys сервера),
// при неудаче — по паролю. После входа проверяет сессию выполнением команды
// и best-effort собирает базовые факты о сервере.
package sshclient

import (
	"context"
	"errors"
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

	out, err := sess.Output("hostname; uname -s; uname -r; uname -m; (nproc 2>/dev/null || echo 0)")
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
	return &Facts{
		Hostname:      get(0),
		OS:            get(1),
		KernelVersion: get(2),
		Arch:          get(3),
		CPUCores:      cores,
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
