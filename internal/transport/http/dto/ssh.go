package dto

import (
	"time"

	"github.com/YuriyDubinin/dijex-api/internal/sshkey"
)

// SSHKeyResponse — ответ check/create. Приватный ключ не отдаётся никогда.
// public_key — готовая строка для вставки в ~/.ssh/authorized_keys целевого сервера.
type SSHKeyResponse struct {
	Exists         bool       `json:"exists"`
	Valid          bool       `json:"valid"`
	Created        bool       `json:"created"`
	Message        string     `json:"message"`
	Type           string     `json:"type,omitempty"`
	PublicKey      string     `json:"public_key,omitempty"`
	Fingerprint    string     `json:"fingerprint,omitempty"`
	PrivateKeyPath string     `json:"private_key_path"`
	PublicKeyPath  string     `json:"public_key_path"`
	CreatedAt      *time.Time `json:"created_at,omitempty"`
}

// SSHKeyDeleteResponse — ответ удаления ключа.
type SSHKeyDeleteResponse struct {
	Status         string `json:"status"` // "DELETED"
	PrivateKeyPath string `json:"private_key_path"`
	PublicKeyPath  string `json:"public_key_path"`
}

func FromSSHKeyInfo(k sshkey.KeyInfo) SSHKeyResponse {
	return SSHKeyResponse{
		Exists:         k.Exists,
		Valid:          k.Valid,
		Created:        k.Created,
		Message:        k.Message,
		Type:           k.Type,
		PublicKey:      k.PublicKey,
		Fingerprint:    k.Fingerprint,
		PrivateKeyPath: k.PrivateKeyPath,
		PublicKeyPath:  k.PublicKeyPath,
		CreatedAt:      k.CreatedAt,
	}
}
