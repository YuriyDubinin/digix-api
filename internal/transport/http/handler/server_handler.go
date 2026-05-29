package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/google/uuid"

	"github.com/YuriyDubinin/dijex-api/internal/domain"
	"github.com/YuriyDubinin/dijex-api/internal/service"
	"github.com/YuriyDubinin/dijex-api/internal/transport/http/dto"
	mw "github.com/YuriyDubinin/dijex-api/internal/transport/http/middleware"
	"github.com/YuriyDubinin/dijex-api/internal/transport/http/response"
	"github.com/YuriyDubinin/dijex-api/pkg/validator"
)

// ServerService — узкий контракт хендлера. Реализуется *service.ServerService.
type ServerService interface {
	CreateServer(ctx context.Context, input service.CreateServerInput) (*service.ServerView, error)
	ListServers(ctx context.Context, input service.ListServersInput) (*service.ListServersOutput, error)
	UpdateServer(ctx context.Context, input service.UpdateServerInput) (*service.ServerView, error)
	DeleteServer(ctx context.Context, id uuid.UUID) (*service.DeleteServerOutput, error)
	RemoteConnect(ctx context.Context, id uuid.UUID) (*service.RemoteConnectOutput, error)
	RemotePing(ctx context.Context, id uuid.UUID) (*service.RemotePingOutput, error)
	InstallSSHKey(ctx context.Context, id uuid.UUID) (*service.InstallSSHKeyOutput, error)
}

type ServerHandler struct {
	service   ServerService
	validator *validator.Validator
	logger    *slog.Logger
}

func NewServerHandler(svc ServerService, v *validator.Validator, logger *slog.Logger) *ServerHandler {
	return &ServerHandler{service: svc, validator: v, logger: logger}
}

// Create — POST /api/servers/create.
func (h *ServerHandler) Create(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)

	var req dto.CreateServerHTTPRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, "INVALID_JSON", "invalid JSON body")
		return
	}
	if err := h.validator.Struct(req); err != nil {
		details := toResponseFieldErrors(validator.TranslateErrors(err))
		response.WriteError(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "request validation failed", details...)
		return
	}

	out, err := h.service.CreateServer(r.Context(), req.ToServiceInput())
	if err != nil {
		var verr domain.ValidationErrors
		if errors.As(err, &verr) {
			response.WriteError(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "domain validation failed", domainValidationDetails(verr)...)
			return
		}
		if errors.Is(err, domain.ErrAlreadyExists) {
			response.WriteError(w, http.StatusConflict, "SERVER_EXISTS", "server with this name already exists")
			return
		}
		h.logger.Error("create server", "err", err, "request_id", mw.RequestIDFromContext(r.Context()))
		response.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error")
		return
	}

	response.WriteJSON(w, http.StatusCreated, dto.FromServerView(out))
}

// List — GET /api/servers/list.
func (h *ServerHandler) List(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	in := service.ListServersInput{
		Page:        atoiDefault(q.Get("page"), 0),
		PageSize:    atoiDefault(q.Get("page_size"), 0),
		Environment: q.Get("environment"),
		Protocol:    q.Get("protocol"),
		AuthMethod:  q.Get("auth_method"),
		IsActive:    parseBoolPtr(q.Get("is_active")),
		Search:      q.Get("search"),
		SortBy:      q.Get("sort_by"),
		Order:       q.Get("order"),
	}

	out, err := h.service.ListServers(r.Context(), in)
	if err != nil {
		var verr domain.ValidationErrors
		if errors.As(err, &verr) {
			response.WriteError(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "query validation failed", domainValidationDetails(verr)...)
			return
		}
		h.logger.Error("list servers", "err", err, "request_id", mw.RequestIDFromContext(r.Context()))
		response.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error")
		return
	}

	response.WriteJSON(w, http.StatusOK, dto.FromListServersOutput(out))
}

// Update — PUT /api/servers/update.
func (h *ServerHandler) Update(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)

	var req dto.UpdateServerHTTPRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, "INVALID_JSON", "invalid JSON body")
		return
	}
	if err := h.validator.Struct(req); err != nil {
		details := toResponseFieldErrors(validator.TranslateErrors(err))
		response.WriteError(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "request validation failed", details...)
		return
	}

	out, err := h.service.UpdateServer(r.Context(), req.ToServiceInput())
	if err != nil {
		var verr domain.ValidationErrors
		if errors.As(err, &verr) {
			response.WriteError(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "domain validation failed", domainValidationDetails(verr)...)
			return
		}
		if errors.Is(err, domain.ErrNotFound) {
			response.WriteError(w, http.StatusNotFound, "SERVER_NOT_FOUND", "server not found")
			return
		}
		if errors.Is(err, domain.ErrAlreadyExists) {
			response.WriteError(w, http.StatusConflict, "SERVER_EXISTS", "server with this name already exists")
			return
		}
		h.logger.Error("update server", "err", err, "request_id", mw.RequestIDFromContext(r.Context()))
		response.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error")
		return
	}

	response.WriteJSON(w, http.StatusOK, dto.FromServerView(out))
}

// RemoteConnect — POST /api/servers/remote/connect.
// Подключается к серверу по SSH (наш ключ → пароль), проверяет сессию.
// Недоступность/отказ auth — 200 с connected=false (не HTTP-ошибка).
func (h *ServerHandler) RemoteConnect(w http.ResponseWriter, r *http.Request) {
	id, ok := h.decodeRemoteID(w, r)
	if !ok {
		return
	}
	out, err := h.service.RemoteConnect(r.Context(), id)
	if err != nil {
		h.writeRemoteError(w, r, err, "remote connect")
		return
	}
	response.WriteJSON(w, http.StatusOK, dto.FromRemoteConnectOutput(out))
}

// RemotePing — POST /api/servers/remote/ping.
// Пингует SSH-соединение и выставляет is_active (успех → true, провал → false).
func (h *ServerHandler) RemotePing(w http.ResponseWriter, r *http.Request) {
	id, ok := h.decodeRemoteID(w, r)
	if !ok {
		return
	}
	out, err := h.service.RemotePing(r.Context(), id)
	if err != nil {
		h.writeRemoteError(w, r, err, "remote ping")
		return
	}
	response.WriteJSON(w, http.StatusOK, dto.FromRemotePingOutput(out))
}

// InstallKey — POST /api/servers/remote/install-ssh.
// Заходит на сервер по паролю и устанавливает наш публичный ключ приложения
// в ~/.ssh/authorized_keys (идемпотентно), затем верифицирует ключевую
// аутентификацию. При успехе ставит ssh_key_installed=true в БД.
// Недоступность/AUTH_FAILED — 200 с подробностями в теле (не HTTP-ошибка).
func (h *ServerHandler) InstallKey(w http.ResponseWriter, r *http.Request) {
	id, ok := h.decodeRemoteID(w, r)
	if !ok {
		return
	}
	out, err := h.service.InstallSSHKey(r.Context(), id)
	if err != nil {
		h.writeRemoteError(w, r, err, "install ssh key")
		return
	}
	response.WriteJSON(w, http.StatusOK, dto.FromInstallSSHKeyOutput(out))
}

// decodeRemoteID парсит тело {id}. При ошибке сам пишет ответ и возвращает ok=false.
func (h *ServerHandler) decodeRemoteID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
	var req dto.RemoteServerHTTPRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, "INVALID_JSON", "invalid JSON body")
		return uuid.Nil, false
	}
	return req.ID, true
}

func (h *ServerHandler) writeRemoteError(w http.ResponseWriter, r *http.Request, err error, op string) {
	var verr domain.ValidationErrors
	if errors.As(err, &verr) {
		response.WriteError(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "request validation failed", domainValidationDetails(verr)...)
		return
	}
	if errors.Is(err, domain.ErrNotFound) {
		response.WriteError(w, http.StatusNotFound, "SERVER_NOT_FOUND", "server not found")
		return
	}
	h.logger.Error(op, "err", err, "request_id", mw.RequestIDFromContext(r.Context()))
	response.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error")
}

// Delete — DELETE /api/servers/delete (soft delete).
func (h *ServerHandler) Delete(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)

	var req dto.DeleteServerHTTPRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, "INVALID_JSON", "invalid JSON body")
		return
	}

	out, err := h.service.DeleteServer(r.Context(), req.ID)
	if err != nil {
		var verr domain.ValidationErrors
		if errors.As(err, &verr) {
			response.WriteError(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "request validation failed", domainValidationDetails(verr)...)
			return
		}
		if errors.Is(err, domain.ErrNotFound) {
			response.WriteError(w, http.StatusNotFound, "SERVER_NOT_FOUND", "server not found")
			return
		}
		h.logger.Error("delete server", "err", err, "request_id", mw.RequestIDFromContext(r.Context()))
		response.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error")
		return
	}

	response.WriteJSON(w, http.StatusOK, dto.FromDeleteServerOutput(out))
}
