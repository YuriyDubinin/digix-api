package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/YuriyDubinin/dijex-api/internal/domain"
	"github.com/YuriyDubinin/dijex-api/internal/registryclient"
	"github.com/YuriyDubinin/dijex-api/internal/service"
	"github.com/YuriyDubinin/dijex-api/internal/transport/http/dto"
	mw "github.com/YuriyDubinin/dijex-api/internal/transport/http/middleware"
	"github.com/YuriyDubinin/dijex-api/internal/transport/http/response"
	"github.com/YuriyDubinin/dijex-api/pkg/validator"
)

// RegistryService — узкий контракт хендлера. Реализуется *service.RegistryService.
type RegistryService interface {
	CreateRegistry(ctx context.Context, input service.CreateRegistryInput) (*service.CreateRegistryOutput, error)
	ListRegistries(ctx context.Context, input service.ListRegistriesInput) (*service.ListRegistriesOutput, error)
	UpdateRegistry(ctx context.Context, input service.UpdateRegistryInput) (*service.UpdateRegistryOutput, error)
	DeleteRegistry(ctx context.Context, id uuid.UUID) (*service.DeleteRegistryOutput, error)
	ConnectRegistry(ctx context.Context, id uuid.UUID) (*service.ConnectRegistryOutput, error)
	PingRegistry(ctx context.Context, id uuid.UUID) (*service.PingRegistryOutput, error)
	ListRegistryImages(ctx context.Context, input service.ListRegistryImagesInput) (*service.ListRegistryImagesOutput, error)
}

type RegistryHandler struct {
	service   RegistryService
	validator *validator.Validator
	logger    *slog.Logger
}

func NewRegistryHandler(svc RegistryService, v *validator.Validator, logger *slog.Logger) *RegistryHandler {
	return &RegistryHandler{service: svc, validator: v, logger: logger}
}

// Create обрабатывает POST /api/registries. Защищённый роут.
//
// Ответы:
//   - 400 INVALID_JSON         — тело не парсится / неизвестные поля
//   - 422 VALIDATION_ERROR     — провал валидации полей
//   - 409 REGISTRY_EXISTS      — registry с таким именем уже существует
//   - 500 INTERNAL_ERROR       — внутренняя ошибка
//   - 201                      — создано; тело — CreateRegistryHTTPResponse
func (h *RegistryHandler) Create(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)

	var req dto.CreateRegistryHTTPRequest
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

	out, err := h.service.CreateRegistry(r.Context(), req.ToServiceInput())
	if err != nil {
		var verr domain.ValidationErrors
		if errors.As(err, &verr) {
			response.WriteError(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "domain validation failed", domainValidationDetails(verr)...)
			return
		}
		if errors.Is(err, domain.ErrAlreadyExists) {
			response.WriteError(w, http.StatusConflict, "REGISTRY_EXISTS", "registry with this name already exists")
			return
		}
		h.logger.Error("create registry",
			"err", err,
			"request_id", mw.RequestIDFromContext(r.Context()),
		)
		response.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error")
		return
	}

	response.WriteJSON(w, http.StatusCreated, dto.FromCreateRegistryOutput(out))
}

// Update обрабатывает PUT /api/registries/update. Защищённый роут.
//
// Ответы:
//   - 400 INVALID_JSON         — тело не парсится / неизвестные поля
//   - 422 VALIDATION_ERROR     — провал валидации (в т.ч. отсутствует id)
//   - 404 REGISTRY_NOT_FOUND   — registry с таким id не найден
//   - 409 REGISTRY_EXISTS      — другое registry с таким именем уже существует
//   - 500 INTERNAL_ERROR       — внутренняя ошибка
//   - 200                      — обновлено; тело — UpdateRegistryHTTPResponse
func (h *RegistryHandler) Update(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)

	var req dto.UpdateRegistryHTTPRequest
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

	out, err := h.service.UpdateRegistry(r.Context(), req.ToServiceInput())
	if err != nil {
		var verr domain.ValidationErrors
		if errors.As(err, &verr) {
			response.WriteError(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "domain validation failed", domainValidationDetails(verr)...)
			return
		}
		if errors.Is(err, domain.ErrNotFound) {
			response.WriteError(w, http.StatusNotFound, "REGISTRY_NOT_FOUND", "registry not found")
			return
		}
		if errors.Is(err, domain.ErrAlreadyExists) {
			response.WriteError(w, http.StatusConflict, "REGISTRY_EXISTS", "registry with this name already exists")
			return
		}
		h.logger.Error("update registry",
			"err", err,
			"request_id", mw.RequestIDFromContext(r.Context()),
		)
		response.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error")
		return
	}

	response.WriteJSON(w, http.StatusOK, dto.FromUpdateRegistryOutput(out))
}

// Images обрабатывает POST /api/registries/images. Защищённый роут.
// По сохранённому registry (id) подключается к реестру и отдаёт список образов
// с тегами и метаданными. Поток зависит от типа реестра (DockerHub Hub API / V2).
//
// Ответы:
//   - 400 INVALID_JSON           — тело не парсится
//   - 422 VALIDATION_ERROR       — нет id
//   - 404 REGISTRY_NOT_FOUND     — registry не найден
//   - 502 REGISTRY_AUTH_FAILED   — реестр отклонил креды
//   - 502 REGISTRY_UNREACHABLE   — реестр недоступен
//   - 422 REGISTRY_UNSUPPORTED   — листинг не поддержан (нет namespace / нет _catalog)
//   - 500 INTERNAL_ERROR         — сбой расшифровки / прочее
//   - 200                        — список образов
func (h *RegistryHandler) Images(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)

	var req dto.ListRegistryImagesHTTPRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, "INVALID_JSON", "invalid JSON body")
		return
	}

	out, err := h.service.ListRegistryImages(r.Context(), service.ListRegistryImagesInput{
		ID:        req.ID,
		Namespace: req.Namespace,
	})
	if err != nil {
		var verr domain.ValidationErrors
		switch {
		case errors.As(err, &verr):
			response.WriteError(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "request validation failed", domainValidationDetails(verr)...)
		case errors.Is(err, domain.ErrNotFound):
			response.WriteError(w, http.StatusNotFound, "REGISTRY_NOT_FOUND", "registry not found")
		case errors.Is(err, registryclient.ErrListAuth):
			response.WriteError(w, http.StatusBadGateway, "REGISTRY_AUTH_FAILED", "registry rejected credentials")
		case errors.Is(err, registryclient.ErrListUnreachable):
			response.WriteError(w, http.StatusBadGateway, "REGISTRY_UNREACHABLE", "registry unreachable")
		case errors.Is(err, registryclient.ErrListUnsupported):
			response.WriteError(w, http.StatusUnprocessableEntity, "REGISTRY_UNSUPPORTED", err.Error())
		default:
			h.logger.Error("list registry images",
				"err", err,
				"request_id", mw.RequestIDFromContext(r.Context()),
			)
			response.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error")
		}
		return
	}

	response.WriteJSON(w, http.StatusOK, dto.FromListRegistryImagesOutput(out))
}

// Ping обрабатывает POST /api/registries/ping. Защищённый роут.
// Health-check сохранённого registry по id: переключает is_active в обе стороны
// (успех → активна, провал → неактивна).
//
// Недоступность/отказ авторизации — это 200 с connected=false (не HTTP-ошибка).
//
// Ответы:
//   - 400 INVALID_JSON         — тело не парсится
//   - 422 VALIDATION_ERROR     — нет id
//   - 404 REGISTRY_NOT_FOUND   — registry не найден
//   - 500 INTERNAL_ERROR       — сбой расшифровки / прочее
//   - 200                      — результат проверки (connected true|false)
func (h *RegistryHandler) Ping(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)

	var req dto.PingRegistryHTTPRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, "INVALID_JSON", "invalid JSON body")
		return
	}

	out, err := h.service.PingRegistry(r.Context(), req.ID)
	if err != nil {
		var verr domain.ValidationErrors
		if errors.As(err, &verr) {
			response.WriteError(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "request validation failed", domainValidationDetails(verr)...)
			return
		}
		if errors.Is(err, domain.ErrNotFound) {
			response.WriteError(w, http.StatusNotFound, "REGISTRY_NOT_FOUND", "registry not found")
			return
		}
		h.logger.Error("ping registry",
			"err", err,
			"request_id", mw.RequestIDFromContext(r.Context()),
		)
		response.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error")
		return
	}

	response.WriteJSON(w, http.StatusOK, dto.FromPingRegistryOutput(out))
}

// Connect обрабатывает POST /api/registries/connect. Защищённый роут.
// Берёт registry по id из БД и проверяет подключение к Docker Registry.
//
// Недоступность/отказ авторизации реестра — это НЕ HTTP-ошибка, а 200 с
// connected=false и статусом в теле. HTTP-ошибки — только про сам запрос.
//
// Ответы:
//   - 400 INVALID_JSON         — тело не парсится
//   - 422 VALIDATION_ERROR     — отсутствует id
//   - 404 REGISTRY_NOT_FOUND   — registry не найден
//   - 500 INTERNAL_ERROR       — сбой расшифровки / внутренняя ошибка
//   - 200                      — результат проверки (connected true|false)
func (h *RegistryHandler) Connect(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)

	var req dto.ConnectRegistryHTTPRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, "INVALID_JSON", "invalid JSON body")
		return
	}

	out, err := h.service.ConnectRegistry(r.Context(), req.ID)
	if err != nil {
		var verr domain.ValidationErrors
		if errors.As(err, &verr) {
			response.WriteError(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "request validation failed", domainValidationDetails(verr)...)
			return
		}
		if errors.Is(err, domain.ErrNotFound) {
			response.WriteError(w, http.StatusNotFound, "REGISTRY_NOT_FOUND", "registry not found")
			return
		}
		h.logger.Error("connect registry",
			"err", err,
			"request_id", mw.RequestIDFromContext(r.Context()),
		)
		response.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error")
		return
	}

	response.WriteJSON(w, http.StatusOK, dto.FromConnectRegistryOutput(out))
}

// Delete обрабатывает DELETE /api/registries/delete (soft delete). Защищённый роут.
//
// Ответы:
//   - 400 INVALID_JSON         — тело не парсится / неизвестные поля
//   - 422 VALIDATION_ERROR     — отсутствует id
//   - 404 REGISTRY_NOT_FOUND   — registry не найден или уже удалён
//   - 500 INTERNAL_ERROR       — внутренняя ошибка
//   - 200                      — помечен удалённым; тело — DeleteRegistryHTTPResponse
func (h *RegistryHandler) Delete(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)

	var req dto.DeleteRegistryHTTPRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, "INVALID_JSON", "invalid JSON body")
		return
	}

	out, err := h.service.DeleteRegistry(r.Context(), req.ID)
	if err != nil {
		var verr domain.ValidationErrors
		if errors.As(err, &verr) {
			response.WriteError(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "request validation failed", domainValidationDetails(verr)...)
			return
		}
		if errors.Is(err, domain.ErrNotFound) {
			response.WriteError(w, http.StatusNotFound, "REGISTRY_NOT_FOUND", "registry not found")
			return
		}
		h.logger.Error("delete registry",
			"err", err,
			"request_id", mw.RequestIDFromContext(r.Context()),
		)
		response.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error")
		return
	}

	response.WriteJSON(w, http.StatusOK, dto.FromDeleteRegistryOutput(out))
}

// List обрабатывает GET /api/registries/list. Защищённый роут.
//
// Query-параметры (все опциональны):
//   page, page_size                 — пагинация (дефолт 1 / 20, page_size max 100)
//   type                            — фильтр по типу registry
//   is_active, is_default           — true|false
//   search                          — поиск по имени (подстрока)
//   sort_by                         — created_at|updated_at|name|type (дефолт created_at)
//   order                           — asc|desc (дефолт desc)
func (h *RegistryHandler) List(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	in := service.ListRegistriesInput{
		Page:      atoiDefault(q.Get("page"), 0),
		PageSize:  atoiDefault(q.Get("page_size"), 0),
		Type:      q.Get("type"),
		IsActive:  parseBoolPtr(q.Get("is_active")),
		IsDefault: parseBoolPtr(q.Get("is_default")),
		Search:    q.Get("search"),
		SortBy:    q.Get("sort_by"),
		Order:     q.Get("order"),
	}

	out, err := h.service.ListRegistries(r.Context(), in)
	if err != nil {
		var verr domain.ValidationErrors
		if errors.As(err, &verr) {
			response.WriteError(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "query validation failed", domainValidationDetails(verr)...)
			return
		}
		h.logger.Error("list registries",
			"err", err,
			"request_id", mw.RequestIDFromContext(r.Context()),
		)
		response.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error")
		return
	}

	response.WriteJSON(w, http.StatusOK, dto.FromListRegistriesOutput(out))
}

// atoiDefault парсит целое из строки; при пустом/некорректном — возвращает def.
func atoiDefault(s string, def int) int {
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return n
}

// parseBoolPtr: "true"/"false" → *bool; всё прочее (пусто/мусор) → nil («фильтр не задан»).
func parseBoolPtr(s string) *bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "true":
		v := true
		return &v
	case "false":
		v := false
		return &v
	default:
		return nil
	}
}
