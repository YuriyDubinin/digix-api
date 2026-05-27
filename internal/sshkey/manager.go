// Package sshkey управляет SSH-ключом приложения: проверяет наличие ключа в
// стандартном месте и при отсутствии генерирует Ed25519-пару. Этот ключ —
// «идентичность» сервиса для подключения к удалённым серверам: публичную часть
// раскладывают в authorized_keys целевых серверов, приватная остаётся здесь.
package sshkey

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

const defaultKeyName = "id_ed25519"

// KeyInfo — сведения о ключе. Приватный ключ здесь НЕ присутствует сознательно.
type KeyInfo struct {
	Exists         bool   // существует ли файл приватного ключа
	Valid          bool   // содержит ли файл валидный, читаемый ключ
	Created        bool   // true только если ключ был только что создан этим вызовом
	Message        string // человекочитаемое пояснение состояния
	Type           string
	PublicKey      string // строка authorized_keys: "ssh-ed25519 AAAA... comment"
	Fingerprint    string // "SHA256:..."
	PrivateKeyPath string
	PublicKeyPath  string
	CreatedAt      *time.Time
}

// Manager инкапсулирует пути к ключу. Конкурентно-безопасен (mutex на Create).
type Manager struct {
	privPath string
	pubPath  string
	comment  string
	mu       sync.Mutex
}

// NewManager. keyPath — путь к приватному ключу; пусто → ~/.ssh/id_ed25519
// (с откатом на /root/.ssh/id_ed25519, если HOME не определён, например в
// контейнере под root). Никогда не падает на этапе конструирования.
func NewManager(keyPath string) *Manager {
	keyPath = strings.TrimSpace(keyPath)
	if keyPath == "" {
		keyPath = defaultKeyPath()
	}
	hostname, _ := os.Hostname()
	comment := "dijex-api"
	if hostname != "" {
		comment = "dijex-api@" + hostname
	}
	return &Manager{
		privPath: keyPath,
		pubPath:  keyPath + ".pub",
		comment:  comment,
	}
}

func defaultKeyPath() string {
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, ".ssh", defaultKeyName)
	}
	return filepath.Join("/root", ".ssh", defaultKeyName)
}

// Check проверяет: (1) существует ли файл ключа и (2) содержит ли он валидный,
// читаемый приватный ключ. В любом случае возвращает понятный ответ (Message),
// без ошибки на «нет файла» / «файл битый».
func (m *Manager) Check(_ context.Context) (KeyInfo, error) {
	info := KeyInfo{PrivateKeyPath: m.privPath, PublicKeyPath: m.pubPath}

	st, err := os.Stat(m.privPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			info.Exists = false
			info.Valid = false
			info.Message = "ssh key file not found at " + m.privPath
			return info, nil
		}
		return info, fmt.Errorf("sshkey: stat private key: %w", err)
	}
	info.Exists = true
	t := st.ModTime().UTC()
	info.CreatedAt = &t

	// Файл есть — проверяем, что в нём действительно валидный ключ (парсится).
	signer, perr := m.Signer()
	if perr != nil {
		info.Valid = false
		info.Message = "key file exists but contains no valid/readable private key (empty, corrupt or passphrase-protected)"
		return info, nil
	}

	pk := signer.PublicKey()
	info.Valid = true
	info.Type = pk.Type()
	info.PublicKey = strings.TrimSuffix(string(ssh.MarshalAuthorizedKey(pk)), "\n") + " " + m.comment
	info.Fingerprint = ssh.FingerprintSHA256(pk)
	info.Message = "ssh key present and valid"
	return info, nil
}

// Create создаёт ключ, если его нет. Идемпотентен: существующий ключ НЕ
// перезаписывается (вернётся с Created=false), чтобы не сломать уже розданную
// в authorized_keys публичную часть.
func (m *Manager) Create(ctx context.Context) (KeyInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if existing, err := m.Check(ctx); err == nil && existing.Exists {
		existing.Created = false
		return existing, nil
	}

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return KeyInfo{}, fmt.Errorf("sshkey: generate key: %w", err)
	}

	pemBlock, err := ssh.MarshalPrivateKey(priv, m.comment)
	if err != nil {
		return KeyInfo{}, fmt.Errorf("sshkey: marshal private key: %w", err)
	}
	privPEM := pem.EncodeToMemory(pemBlock)

	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		return KeyInfo{}, fmt.Errorf("sshkey: build public key: %w", err)
	}
	pubLine := strings.TrimSuffix(string(ssh.MarshalAuthorizedKey(sshPub)), "\n") + " " + m.comment

	if err := os.MkdirAll(filepath.Dir(m.privPath), 0o700); err != nil {
		return KeyInfo{}, fmt.Errorf("sshkey: create .ssh dir: %w", err)
	}
	if err := os.WriteFile(m.privPath, privPEM, 0o600); err != nil {
		return KeyInfo{}, fmt.Errorf("sshkey: write private key: %w", err)
	}
	if err := os.WriteFile(m.pubPath, []byte(pubLine+"\n"), 0o644); err != nil {
		return KeyInfo{}, fmt.Errorf("sshkey: write public key: %w", err)
	}

	now := time.Now().UTC()
	return KeyInfo{
		Exists:         true,
		Valid:          true,
		Created:        true,
		Message:        "ssh key created",
		Type:           sshPub.Type(),
		PublicKey:      pubLine,
		Fingerprint:    ssh.FingerprintSHA256(sshPub),
		PrivateKeyPath: m.privPath,
		PublicKeyPath:  m.pubPath,
		CreatedAt:      &now,
	}, nil
}

// DeleteResult — итог удаления ключа.
type DeleteResult struct {
	Deleted        bool // был ли удалён файл (false — файла не было)
	PrivateKeyPath string
	PublicKeyPath  string
}

// Delete удаляет файл приватного ключа (и .pub — best-effort).
// Если приватного ключа нет — Deleted=false (handler отдаст 404), без ошибки.
func (m *Manager) Delete(_ context.Context) (DeleteResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	res := DeleteResult{PrivateKeyPath: m.privPath, PublicKeyPath: m.pubPath}

	if _, err := os.Stat(m.privPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return res, nil // нечего удалять
		}
		return res, fmt.Errorf("sshkey: stat private key: %w", err)
	}

	if err := os.Remove(m.privPath); err != nil {
		return res, fmt.Errorf("sshkey: remove private key: %w", err)
	}
	// Публичный ключ удаляем best-effort: приватный (секрет) уже удалён.
	_ = os.Remove(m.pubPath)

	res.Deleted = true
	return res, nil
}

// Signer загружает приватный ключ и возвращает ssh.Signer для аутентификации
// при подключении к серверам. Ошибка, если ключа нет или он не парсится.
func (m *Manager) Signer() (ssh.Signer, error) {
	data, err := os.ReadFile(m.privPath)
	if err != nil {
		return nil, fmt.Errorf("sshkey: read private key: %w", err)
	}
	signer, err := ssh.ParsePrivateKey(data)
	if err != nil {
		return nil, fmt.Errorf("sshkey: parse private key: %w", err)
	}
	return signer, nil
}

