package service

import (
	"context"
	"fmt"
	"log/slog"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/YuriyDubinin/dijex-api/internal/domain"
	"github.com/YuriyDubinin/dijex-api/internal/registryclient"
	"github.com/YuriyDubinin/dijex-api/pkg/crypto"
)

// registryConnector — узкий контракт проверки подключения и листинга образов.
// Реализуется *registryclient.Checker. Вынесен в интерфейс для тестируемости.
type registryConnector interface {
	Check(ctx context.Context, target registryclient.Target) registryclient.Result
	ListImages(ctx context.Context, target registryclient.ListTarget) (registryclient.ImageList, error)
}

type RegistryService struct {
	repo      domain.RegistryRepository
	cipher    *crypto.Cipher
	connector registryConnector
	logger    *slog.Logger
	clock     func() time.Time
}

func NewRegistryService(repo domain.RegistryRepository, cipher *crypto.Cipher, connector registryConnector, logger *slog.Logger) *RegistryService {
	return &RegistryService{
		repo:      repo,
		cipher:    cipher,
		connector: connector,
		logger:    logger,
		clock:     time.Now,
	}
}

// CreateRegistry нормализует и валидирует вход, шифрует пароль и сохраняет
// подключение. Возвращает данные без пароля.
func (s *RegistryService) CreateRegistry(ctx context.Context, input CreateRegistryInput) (*CreateRegistryOutput, error) {
	in := normalizeRegistryInput(input)

	if errs := validateRegistryInput(in); len(errs) > 0 {
		return nil, errs
	}

	// Шифруем пароль/токен только если он задан (для анонимных registry — пусто).
	encrypted := ""
	if in.Password != "" {
		ct, err := s.cipher.Encrypt(in.Password)
		if err != nil {
			return nil, fmt.Errorf("registry: encrypt password: %w", err)
		}
		encrypted = ct
	}

	now := s.clock()
	reg := &domain.Registry{
		ID:                uuid.New(),
		Name:              in.Name,
		Type:              in.Type,
		URL:               in.URL,
		Username:          in.Username,
		PasswordEncrypted: encrypted,
		Email:             in.Email,
		Namespace:         in.Namespace,
		IsDefault:         in.IsDefault,
		IsActive:          false, // создаётся выключенным; активируется успешным connect
		Insecure:          in.Insecure,
		CreatedAt:         now,
		UpdatedAt:         now,
	}

	if err := s.repo.Create(ctx, reg); err != nil {
		return nil, err // в т.ч. domain.ErrAlreadyExists (имя занято)
	}

	s.logger.Info("registry created",
		"registry_id", reg.ID,
		"name", reg.Name,
		"type", reg.Type,
	)

	return &CreateRegistryOutput{
		ID:             reg.ID,
		Name:           reg.Name,
		Type:           reg.Type,
		URL:            reg.URL,
		Username:       reg.Username,
		Email:          reg.Email,
		Namespace:      reg.Namespace,
		IsDefault:      reg.IsDefault,
		IsActive:       reg.IsActive,
		Insecure:       reg.Insecure,
		HasCredentials: encrypted != "",
		CreatedAt:      reg.CreatedAt,
	}, nil
}

const (
	defaultRegistryPageSize = 20
	maxRegistryPageSize     = 100
)

// ListRegistries возвращает страницу registry с фильтрами. Параметры пагинации
// клампятся в безопасные пределы; невалидный type → ValidationErrors.
func (s *RegistryService) ListRegistries(ctx context.Context, in ListRegistriesInput) (*ListRegistriesOutput, error) {
	regType := strings.ToUpper(strings.TrimSpace(in.Type))
	if regType != "" && !domain.IsValidRegistryType(regType) {
		return nil, domain.ValidationErrors{
			&domain.ValidationError{Field: "type", Message: "must be one of DOCKERHUB, GHCR, GITLAB, HARBOR, ECR, GENERIC"},
		}
	}

	page := in.Page
	if page < 1 {
		page = 1
	}
	size := in.PageSize
	if size < 1 {
		size = defaultRegistryPageSize
	}
	if size > maxRegistryPageSize {
		size = maxRegistryPageSize
	}

	filter := domain.RegistryListFilter{
		Type:      regType,
		IsActive:  in.IsActive,
		IsDefault: in.IsDefault,
		Search:    strings.TrimSpace(in.Search),
		Limit:     size,
		Offset:    (page - 1) * size,
		SortBy:    in.SortBy,
		SortDesc:  strings.ToLower(in.Order) != "asc", // по умолчанию desc
	}

	items, total, err := s.repo.List(ctx, filter)
	if err != nil {
		return nil, err
	}

	totalPages := 0
	if total > 0 {
		totalPages = (total + size - 1) / size
	}

	out := &ListRegistriesOutput{
		Items: make([]RegistryItem, 0, len(items)),
		Pagination: Pagination{
			Page:       page,
			PageSize:   size,
			Total:      total,
			TotalPages: totalPages,
		},
	}
	for _, r := range items {
		out.Items = append(out.Items, RegistryItem{
			ID:             r.ID,
			Name:           r.Name,
			Type:           r.Type,
			URL:            r.URL,
			Username:       r.Username,
			Email:          r.Email,
			Namespace:      r.Namespace,
			IsDefault:      r.IsDefault,
			IsActive:       r.IsActive,
			Insecure:       r.Insecure,
			HasCredentials: r.PasswordEncrypted != "",
			LastCheckedAt:  r.LastCheckedAt,
			LastStatus:     r.LastStatus,
			CreatedAt:      r.CreatedAt,
			UpdatedAt:      r.UpdatedAt,
		})
	}
	return out, nil
}

func normalizeRegistryInput(in CreateRegistryInput) CreateRegistryInput {
	url := strings.TrimSpace(in.URL)
	// Если схема не указана — подставляем https (удобство ввода "ghcr.io").
	if url != "" && !strings.Contains(url, "://") {
		url = "https://" + url
	}
	return CreateRegistryInput{
		Name:      strings.TrimSpace(in.Name),
		Type:      strings.ToUpper(strings.TrimSpace(in.Type)),
		URL:       url,
		Username:  strings.TrimSpace(in.Username),
		Password:  in.Password, // пароль не трогаем (пробелы могут быть значимы)
		Email:     strings.ToLower(strings.TrimSpace(in.Email)),
		Namespace: strings.TrimSpace(in.Namespace),
		IsDefault: in.IsDefault,
		Insecure:  in.Insecure,
	}
}

func validateRegistryInput(in CreateRegistryInput) domain.ValidationErrors {
	return validateRegistryFields(in.Name, in.Type, in.URL, in.Username, in.Email, in.Namespace)
}

// validateRegistryFields — общая проверка полей для create и update.
func validateRegistryFields(name, regType, url, username, email, namespace string) domain.ValidationErrors {
	var errs domain.ValidationErrors

	switch {
	case name == "":
		errs = append(errs, &domain.ValidationError{Field: "name", Message: "is required"})
	default:
		if l := utf8.RuneCountInString(name); l < 2 || l > 100 {
			errs = append(errs, &domain.ValidationError{Field: "name", Message: "must be between 2 and 100 characters"})
		}
	}

	if !domain.IsValidRegistryType(regType) {
		errs = append(errs, &domain.ValidationError{Field: "type", Message: "must be one of DOCKERHUB, GHCR, GITLAB, HARBOR, ECR, GENERIC"})
	}

	switch {
	case url == "":
		errs = append(errs, &domain.ValidationError{Field: "url", Message: "is required"})
	default:
		if utf8.RuneCountInString(url) > 500 {
			errs = append(errs, &domain.ValidationError{Field: "url", Message: "must be at most 500 characters"})
		}
	}

	if username != "" && utf8.RuneCountInString(username) > 255 {
		errs = append(errs, &domain.ValidationError{Field: "username", Message: "must be at most 255 characters"})
	}

	switch {
	case email == "":
		errs = append(errs, &domain.ValidationError{Field: "email", Message: "is required"})
	default:
		if _, err := mail.ParseAddress(email); err != nil {
			errs = append(errs, &domain.ValidationError{Field: "email", Message: "is not a valid email"})
		}
		if utf8.RuneCountInString(email) > 255 {
			errs = append(errs, &domain.ValidationError{Field: "email", Message: "must be at most 255 characters"})
		}
	}

	if namespace != "" && utf8.RuneCountInString(namespace) > 255 {
		errs = append(errs, &domain.ValidationError{Field: "namespace", Message: "must be at most 255 characters"})
	}

	return errs
}

// UpdateRegistry полностью обновляет registry по ID. Пароль обновляется только
// если задан (см. UpdateRegistryInput.Password). Возвращает данные без пароля.
func (s *RegistryService) UpdateRegistry(ctx context.Context, input UpdateRegistryInput) (*UpdateRegistryOutput, error) {
	in := normalizeUpdateRegistryInput(input)

	var errs domain.ValidationErrors
	if in.ID == uuid.Nil {
		errs = append(errs, &domain.ValidationError{Field: "id", Message: "is required"})
	}
	errs = append(errs, validateRegistryFields(in.Name, in.Type, in.URL, in.Username, in.Email, in.Namespace)...)
	if len(errs) > 0 {
		return nil, errs
	}

	existing, err := s.repo.GetByID(ctx, in.ID)
	if err != nil {
		return nil, err // в т.ч. domain.ErrNotFound
	}

	// Пароль: по умолчанию оставляем текущий; nil — не трогаем, "" — очищаем,
	// значение — шифруем заново.
	encrypted := existing.PasswordEncrypted
	if in.Password != nil {
		if *in.Password == "" {
			encrypted = ""
		} else {
			ct, err := s.cipher.Encrypt(*in.Password)
			if err != nil {
				return nil, fmt.Errorf("registry: encrypt password: %w", err)
			}
			encrypted = ct
		}
	}

	reg := &domain.Registry{
		ID:                in.ID,
		Name:              in.Name,
		Type:              in.Type,
		URL:               in.URL,
		Username:          in.Username,
		PasswordEncrypted: encrypted,
		Email:             in.Email,
		Namespace:         in.Namespace,
		IsDefault:         in.IsDefault,
		IsActive:          in.IsActive,
		Insecure:          in.Insecure,
	}

	if err := s.repo.Update(ctx, reg); err != nil {
		return nil, err // ErrNotFound / ErrAlreadyExists
	}

	s.logger.Info("registry updated",
		"registry_id", reg.ID,
		"name", reg.Name,
	)

	return &UpdateRegistryOutput{
		ID:             reg.ID,
		Name:           reg.Name,
		Type:           reg.Type,
		URL:            reg.URL,
		Username:       reg.Username,
		Email:          reg.Email,
		Namespace:      reg.Namespace,
		IsDefault:      reg.IsDefault,
		IsActive:       reg.IsActive,
		Insecure:       reg.Insecure,
		HasCredentials: encrypted != "",
		CreatedAt:      reg.CreatedAt,
		UpdatedAt:      reg.UpdatedAt,
	}, nil
}

// ConnectRegistry берёт registry из БД, расшифровывает пароль и проверяет
// подключение к Docker Registry. Результат проверки сохраняется в last_* полях.
// Недоступность реестра — НЕ ошибка метода: возвращается Output с Connected=false
// и соответствующим статусом. Ошибки метода — только проблемы запроса/инфраструктуры
// (нет id, нет такого registry, сбой расшифровки).
func (s *RegistryService) ConnectRegistry(ctx context.Context, id uuid.UUID) (*ConnectRegistryOutput, error) {
	if id == uuid.Nil {
		return nil, domain.ValidationErrors{
			&domain.ValidationError{Field: "id", Message: "is required"},
		}
	}

	reg, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err // в т.ч. domain.ErrNotFound
	}

	password := ""
	if reg.PasswordEncrypted != "" {
		pw, derr := s.cipher.Decrypt(reg.PasswordEncrypted)
		if derr != nil {
			return nil, fmt.Errorf("registry: decrypt password: %w", derr)
		}
		password = pw
	}

	res := s.connector.Check(ctx, registryclient.Target{
		Type:     reg.Type,
		URL:      reg.URL,
		Username: reg.Email, // логин в аккаунт Docker — по email
		Password: password,
		Insecure: reg.Insecure,
	})

	now := s.clock()
	errMsg := ""
	if !res.Connected {
		errMsg = res.Message
	}
	// connect активирует при успехе; при провале is_active НЕ трогаем (setActive=nil).
	var setActive *bool
	if res.Connected {
		v := true
		setActive = &v
	}
	if uerr := s.repo.UpdateConnectionStatus(ctx, id, res.Status, errMsg, now, setActive); uerr != nil {
		s.logger.Warn("update registry connection status", "err", uerr, "registry_id", id)
	}

	s.logger.Info("registry connect",
		"registry_id", id,
		"status", res.Status,
		"connected", res.Connected,
	)

	return &ConnectRegistryOutput{
		ID:            id,
		Connected:     res.Connected,
		Authenticated: res.Authenticated,
		Status:        res.Status,
		Message:       res.Message,
		APIVersion:    res.APIVersion,
		IsActive:      res.Connected || reg.IsActive, // итоговое состояние
		CheckedAt:     now,
	}, nil
}

// ListRegistryImages берёт сохранённый registry по id, расшифровывает пароль
// и запрашивает список образов (репозиториев) с тегами и метаданными.
// Поток листинга выбирается по типу реестра внутри registryclient.
func (s *RegistryService) ListRegistryImages(ctx context.Context, input ListRegistryImagesInput) (*ListRegistryImagesOutput, error) {
	if input.ID == uuid.Nil {
		return nil, domain.ValidationErrors{
			&domain.ValidationError{Field: "id", Message: "is required"},
		}
	}

	reg, err := s.repo.GetByID(ctx, input.ID)
	if err != nil {
		return nil, err // в т.ч. domain.ErrNotFound
	}

	password := ""
	if reg.PasswordEncrypted != "" {
		pw, derr := s.cipher.Decrypt(reg.PasswordEncrypted)
		if derr != nil {
			return nil, fmt.Errorf("registry: decrypt password: %w", derr)
		}
		password = pw
	}

	// namespace: явный из запроса → из записи → username (Docker ID).
	// На email сюда не полагаемся — это логин аккаунта, а не namespace.
	namespace := strings.TrimSpace(input.Namespace)
	if namespace == "" {
		namespace = reg.Namespace
	}
	if namespace == "" {
		namespace = reg.Username
	}

	list, err := s.connector.ListImages(ctx, registryclient.ListTarget{
		Type:      reg.Type,
		URL:       reg.URL,
		Username:  reg.Email, // логин в аккаунт Docker — по email
		Password:  password,
		Insecure:  reg.Insecure,
		Namespace: namespace,
	})
	if err != nil {
		return nil, err // registryclient.ErrList* — маппится в handler
	}

	out := &ListRegistryImagesOutput{
		RegistryID: reg.ID,
		Type:       reg.Type,
		Source:     list.Source,
		Namespace:  list.Namespace,
		Total:      list.Total,
		Images:     make([]RegistryImage, 0, len(list.Images)),
	}
	for _, img := range list.Images {
		out.Images = append(out.Images, RegistryImage{
			Name:        img.Name,
			Tags:        img.Tags,
			TagCount:    img.TagCount,
			Description: img.Description,
			IsPrivate:   img.IsPrivate,
			PullCount:   img.PullCount,
			StarCount:   img.StarCount,
			LastUpdated: img.LastUpdated,
		})
	}

	s.logger.Info("registry images listed",
		"registry_id", reg.ID,
		"source", list.Source,
		"count", len(out.Images),
	)
	return out, nil
}

// PingRegistry — health-check сохранённого registry по id. В отличие от connect,
// переключает is_active в ОБЕ стороны: успех → активна, провал → неактивна.
func (s *RegistryService) PingRegistry(ctx context.Context, id uuid.UUID) (*PingRegistryOutput, error) {
	if id == uuid.Nil {
		return nil, domain.ValidationErrors{
			&domain.ValidationError{Field: "id", Message: "is required"},
		}
	}

	reg, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err // в т.ч. domain.ErrNotFound
	}

	password := ""
	if reg.PasswordEncrypted != "" {
		pw, derr := s.cipher.Decrypt(reg.PasswordEncrypted)
		if derr != nil {
			return nil, fmt.Errorf("registry: decrypt password: %w", derr)
		}
		password = pw
	}

	res := s.connector.Check(ctx, registryclient.Target{
		Type:     reg.Type,
		URL:      reg.URL,
		Username: reg.Email, // логин в аккаунт Docker — по email
		Password: password,
		Insecure: reg.Insecure,
	})

	now := s.clock()
	errMsg := ""
	if !res.Connected {
		errMsg = res.Message
	}
	// ping управляет активностью в обе стороны: is_active = connected.
	active := res.Connected
	if uerr := s.repo.UpdateConnectionStatus(ctx, id, res.Status, errMsg, now, &active); uerr != nil {
		s.logger.Warn("update registry connection status", "err", uerr, "registry_id", id)
	}

	s.logger.Info("registry ping",
		"registry_id", id,
		"status", res.Status,
		"connected", res.Connected,
		"is_active", active,
	)

	return &PingRegistryOutput{
		ID:            id,
		Connected:     res.Connected,
		Authenticated: res.Authenticated,
		Status:        res.Status,
		Message:       res.Message,
		APIVersion:    res.APIVersion,
		IsActive:      active,
		CheckedAt:     now,
	}, nil
}

// DeleteRegistry мягко удаляет registry по ID (soft delete через deleted_at).
func (s *RegistryService) DeleteRegistry(ctx context.Context, id uuid.UUID) (*DeleteRegistryOutput, error) {
	if id == uuid.Nil {
		return nil, domain.ValidationErrors{
			&domain.ValidationError{Field: "id", Message: "is required"},
		}
	}

	deletedAt, err := s.repo.SoftDelete(ctx, id)
	if err != nil {
		return nil, err // в т.ч. domain.ErrNotFound
	}

	s.logger.Info("registry deleted", "registry_id", id)
	return &DeleteRegistryOutput{ID: id, DeletedAt: deletedAt}, nil
}

func normalizeUpdateRegistryInput(in UpdateRegistryInput) UpdateRegistryInput {
	url := strings.TrimSpace(in.URL)
	if url != "" && !strings.Contains(url, "://") {
		url = "https://" + url
	}
	return UpdateRegistryInput{
		ID:        in.ID,
		Name:      strings.TrimSpace(in.Name),
		Type:      strings.ToUpper(strings.TrimSpace(in.Type)),
		URL:       url,
		Username:  strings.TrimSpace(in.Username),
		Password:  in.Password, // pointer — состояние «оставить/очистить/задать»
		Email:     strings.ToLower(strings.TrimSpace(in.Email)),
		Namespace: strings.TrimSpace(in.Namespace),
		IsDefault: in.IsDefault,
		IsActive:  in.IsActive,
		Insecure:  in.Insecure,
	}
}
