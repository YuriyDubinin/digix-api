package handler

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/YuriyDubinin/dijex-api/internal/sshkey"
	mw "github.com/YuriyDubinin/dijex-api/internal/transport/http/middleware"
	"github.com/YuriyDubinin/dijex-api/internal/transport/http/dto"
	"github.com/YuriyDubinin/dijex-api/internal/transport/http/response"
)

// SSHKeyManager — узкий контракт хендлера. Реализуется *sshkey.Manager.
type SSHKeyManager interface {
	Check(ctx context.Context) (sshkey.KeyInfo, error)
	Create(ctx context.Context) (sshkey.KeyInfo, error)
	Delete(ctx context.Context) (sshkey.DeleteResult, error)
}

type SSHHandler struct {
	manager SSHKeyManager
	logger  *slog.Logger
}

func NewSSHHandler(m SSHKeyManager, logger *slog.Logger) *SSHHandler {
	return &SSHHandler{manager: m, logger: logger}
}

// Check — GET /api/system/ssh/check. Проверяет наличие файла ключа И что в нём
// валидный ключ. Любой негативный исход — ошибка:
//   - файла нет                        → 404 SSH_KEY_NOT_FOUND
//   - файл есть, но ключа нет/невалиден → 422 SSH_KEY_INVALID
//   - валидный ключ                     → 200 с данными ключа
func (h *SSHHandler) Check(w http.ResponseWriter, r *http.Request) {
	info, err := h.manager.Check(r.Context())
	if err != nil {
		h.logger.Error("ssh key check", "err", err, "request_id", mw.RequestIDFromContext(r.Context()))
		response.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error")
		return
	}
	if !info.Exists {
		response.WriteError(w, http.StatusNotFound, "SSH_KEY_NOT_FOUND", info.Message)
		return
	}
	if !info.Valid {
		response.WriteError(w, http.StatusUnprocessableEntity, "SSH_KEY_INVALID", info.Message)
		return
	}
	response.WriteJSON(w, http.StatusOK, dto.FromSSHKeyInfo(info))
}

// Get — GET /api/system/ssh/get. Отдаёт SSH-ключ (публичную часть), если он
// найден в стандартном месте; иначе — 404 с понятным кодом.
// Приватный ключ не возвращается никогда.
//
// Ответы:
//   - 200                   — ключ найден, тело — данные ключа (public_key, fingerprint, ...)
//   - 404 SSH_KEY_NOT_FOUND — ключа нет в стандартном месте
//   - 500 INTERNAL_ERROR    — ошибка чтения
func (h *SSHHandler) Get(w http.ResponseWriter, r *http.Request) {
	info, err := h.manager.Check(r.Context())
	if err != nil {
		h.logger.Error("ssh key get", "err", err, "request_id", mw.RequestIDFromContext(r.Context()))
		response.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error")
		return
	}
	if !info.Exists {
		response.WriteError(w, http.StatusNotFound, "SSH_KEY_NOT_FOUND", "ssh key not found in the standard location")
		return
	}
	response.WriteJSON(w, http.StatusOK, dto.FromSSHKeyInfo(info))
}

// Delete — DELETE /api/system/ssh/delete. Удаляет файл ключа (и .pub).
//
// Ответы:
//   - 200                   — ключ удалён
//   - 404 SSH_KEY_NOT_FOUND — удалять нечего (файла нет)
//   - 500 INTERNAL_ERROR    — ошибка удаления (например, права)
func (h *SSHHandler) Delete(w http.ResponseWriter, r *http.Request) {
	res, err := h.manager.Delete(r.Context())
	if err != nil {
		h.logger.Error("ssh key delete", "err", err, "request_id", mw.RequestIDFromContext(r.Context()))
		response.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error")
		return
	}
	if !res.Deleted {
		response.WriteError(w, http.StatusNotFound, "SSH_KEY_NOT_FOUND", "no ssh key to delete")
		return
	}
	response.WriteJSON(w, http.StatusOK, dto.SSHKeyDeleteResponse{
		Status:         "DELETED",
		PrivateKeyPath: res.PrivateKeyPath,
		PublicKeyPath:  res.PublicKeyPath,
	})
}

// Create — POST /api/system/ssh/create. Создаёт ключ, если его нет (идемпотентно).
// 201 — если ключ создан сейчас; 200 — если уже существовал.
func (h *SSHHandler) Create(w http.ResponseWriter, r *http.Request) {
	info, err := h.manager.Create(r.Context())
	if err != nil {
		h.logger.Error("ssh key create", "err", err, "request_id", mw.RequestIDFromContext(r.Context()))
		response.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error")
		return
	}
	status := http.StatusOK
	if info.Created {
		status = http.StatusCreated
	}
	response.WriteJSON(w, status, dto.FromSSHKeyInfo(info))
}
