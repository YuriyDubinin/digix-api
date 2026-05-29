package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/YuriyDubinin/dijex-api/internal/domain"
	"github.com/YuriyDubinin/dijex-api/internal/sshclient"
	"github.com/YuriyDubinin/dijex-api/internal/sshkey"
	"github.com/YuriyDubinin/dijex-api/pkg/crypto"
)

// serverConnector — узкий контракт SSH-подключения. Реализуется *sshclient.Connector.
type serverConnector interface {
	Connect(ctx context.Context, t sshclient.Target) sshclient.Result
	Ping(ctx context.Context, t sshclient.Target) sshclient.Result
	InstallPublicKey(ctx context.Context, t sshclient.Target, publicKey string) sshclient.InstallResult
}

// serverKeyProvider — контракт получения публичного ключа приложения.
// Реализуется *sshkey.Manager.
type serverKeyProvider interface {
	Check(ctx context.Context) (sshkey.KeyInfo, error)
}

const (
	defaultServerPageSize = 20
	maxServerPageSize     = 100
	defaultSSHPort        = 22
	maxServerTags         = 30
	maxServerTagLen       = 50
)

type ServerService struct {
	repo      domain.ServerRepository
	cipher    *crypto.Cipher
	connector serverConnector
	keys      serverKeyProvider
	logger    *slog.Logger
	clock     func() time.Time
}

func NewServerService(
	repo domain.ServerRepository,
	cipher *crypto.Cipher,
	connector serverConnector,
	keys serverKeyProvider,
	logger *slog.Logger,
) *ServerService {
	return &ServerService{
		repo:      repo,
		cipher:    cipher,
		connector: connector,
		keys:      keys,
		logger:    logger,
		clock:     time.Now,
	}
}

// RemoteConnect подключается к серверу по SSH (наш ключ → пароль), проверяет
// сессию и собирает базовые факты. Недоступность/отказ auth — НЕ ошибка метода:
// возвращается Output с Connected=false. is_active НЕ трогаем (это делает Ping).
func (s *ServerService) RemoteConnect(ctx context.Context, id uuid.UUID) (*RemoteConnectOutput, error) {
	srv, password, err := s.loadServerForSSH(ctx, id)
	if err != nil {
		return nil, err
	}

	res := s.connector.Connect(ctx, sshclient.Target{
		Host:     srv.Host,
		Port:     srv.Port,
		User:     srv.Username,
		Password: password,
	})

	now := s.clock()
	errMsg := ""
	if !res.Connected {
		errMsg = res.Message
	}
	// connect не управляет is_active — только фиксирует статус попытки.
	if uerr := s.repo.UpdateConnectionStatus(ctx, id, res.Status, errMsg, now, nil); uerr != nil {
		s.logger.Warn("update server connection status", "err", uerr, "server_id", id)
	}

	out := &RemoteConnectOutput{
		ID:        id,
		Connected: res.Connected,
		Method:    res.Method,
		Status:    res.Status,
		Message:   res.Message,
		CheckedAt: now,
	}
	if res.Connected && res.Facts != nil {
		facts := domain.ServerFacts{
			OS:             res.Facts.OS,
			Arch:           res.Facts.Arch,
			KernelVersion:  res.Facts.KernelVersion,
			RemoteHostname: res.Facts.Hostname,
		}
		if res.Facts.CPUCores > 0 {
			cores := res.Facts.CPUCores
			facts.CPUCores = &cores
		}
		if ferr := s.repo.UpdateFacts(ctx, id, facts); ferr != nil {
			s.logger.Warn("update server facts", "err", ferr, "server_id", id)
		}
		out.RemoteHostname = res.Facts.Hostname
		out.OS = res.Facts.OS
		out.KernelVersion = res.Facts.KernelVersion
		out.Arch = res.Facts.Arch
		out.CPUCores = facts.CPUCores
	}

	s.logger.Info("server remote connect", "server_id", id, "status", res.Status, "method", res.Method, "connected", res.Connected)
	return out, nil
}

// RemotePing пингует SSH-соединение и выставляет is_active: успех → true,
// провал → false (в обе стороны).
func (s *ServerService) RemotePing(ctx context.Context, id uuid.UUID) (*RemotePingOutput, error) {
	srv, password, err := s.loadServerForSSH(ctx, id)
	if err != nil {
		return nil, err
	}

	res := s.connector.Ping(ctx, sshclient.Target{
		Host:     srv.Host,
		Port:     srv.Port,
		User:     srv.Username,
		Password: password,
	})

	now := s.clock()
	errMsg := ""
	if !res.Connected {
		errMsg = res.Message
	}
	active := res.Connected
	if uerr := s.repo.UpdateConnectionStatus(ctx, id, res.Status, errMsg, now, &active); uerr != nil {
		s.logger.Warn("update server connection status", "err", uerr, "server_id", id)
	}

	s.logger.Info("server remote ping", "server_id", id, "status", res.Status, "connected", res.Connected, "is_active", active)
	return &RemotePingOutput{
		ID:        id,
		Connected: res.Connected,
		Method:    res.Method,
		Status:    res.Status,
		Message:   res.Message,
		IsActive:  active,
		CheckedAt: now,
	}, nil
}

// InstallSSHKey устанавливает наш SSH-ключ приложения в authorized_keys
// удалённого сервера. Заходит ПО ПАРОЛЮ (ключа на сервере ещё нет), добавляет
// публичный ключ идемпотентно, проверяет верификацией (повторным коннектом по
// ключу), и при успехе ставит ssh_key_installed=true в БД.
//
// Недоступность сервера / неверный пароль — НЕ ошибка метода: 200 с подробным
// статусом в теле. Ошибки метода — только проблемы запроса/конфигурации.
func (s *ServerService) InstallSSHKey(ctx context.Context, id uuid.UUID) (*InstallSSHKeyOutput, error) {
	srv, password, err := s.loadServerForSSH(ctx, id)
	if err != nil {
		return nil, err // ValidationErrors / domain.ErrNotFound / decrypt error
	}

	// Без пароля установка ключа невозможна — это бутстрап, ключа на сервере ещё нет.
	if password == "" {
		return nil, domain.ValidationErrors{
			&domain.ValidationError{Field: "password", Message: "server has no password — install-key requires password to bootstrap"},
		}
	}

	// Берём наш публичный ключ.
	keyInfo, err := s.keys.Check(ctx)
	if err != nil {
		return nil, fmt.Errorf("install-key: read app ssh key: %w", err)
	}
	if !keyInfo.Valid || keyInfo.PublicKey == "" {
		return nil, domain.ValidationErrors{
			&domain.ValidationError{Field: "ssh_key", Message: "app ssh key is missing or invalid — create it via POST /api/system/ssh/create"},
		}
	}

	res := s.connector.InstallPublicKey(ctx, sshclient.Target{
		Host:     srv.Host,
		Port:     srv.Port,
		User:     srv.Username,
		Password: password,
	}, keyInfo.PublicKey)

	now := s.clock()

	// Ставим флаг в БД ТОЛЬКО при подтверждённой работе ключа.
	flagSet := false
	if res.Verified {
		if err := s.repo.MarkSSHKeyInstalled(ctx, id, true); err != nil {
			s.logger.Warn("mark ssh_key_installed", "err", err, "server_id", id)
		} else {
			flagSet = true
		}
	}

	// Полезно также обновить last_status: успех install-key — это и успешный коннект.
	errMsg := ""
	if !res.Connected {
		errMsg = res.Message
	}
	if uerr := s.repo.UpdateConnectionStatus(ctx, id, res.Status, errMsg, now, nil); uerr != nil {
		s.logger.Warn("update server connection status", "err", uerr, "server_id", id)
	}

	s.logger.Info("server install ssh key",
		"server_id", id,
		"status", res.Status,
		"already_installed", res.AlreadyInstalled,
		"installed", res.Installed,
		"verified", res.Verified,
	)

	return &InstallSSHKeyOutput{
		ID:               id,
		Connected:        res.Connected,
		AlreadyInstalled: res.AlreadyInstalled,
		Installed:        res.Installed,
		Verified:         res.Verified,
		SSHKeyInstalled:  flagSet,
		Status:           res.Status,
		Message:          res.Message,
		CheckedAt:        now,
	}, nil
}

// loadServerForSSH достаёт сервер по id и расшифровывает пароль.
func (s *ServerService) loadServerForSSH(ctx context.Context, id uuid.UUID) (*domain.Server, string, error) {
	if id == uuid.Nil {
		return nil, "", domain.ValidationErrors{
			&domain.ValidationError{Field: "id", Message: "is required"},
		}
	}
	srv, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, "", err // в т.ч. domain.ErrNotFound
	}
	password := ""
	if srv.PasswordEncrypted != "" {
		pw, derr := s.cipher.Decrypt(srv.PasswordEncrypted)
		if derr != nil {
			return nil, "", fmt.Errorf("server: decrypt password: %w", derr)
		}
		password = pw
	}
	return srv, password, nil
}

// CreateServer нормализует вход, валидирует, шифрует секреты и сохраняет сервер.
func (s *ServerService) CreateServer(ctx context.Context, input CreateServerInput) (*ServerView, error) {
	in := normalizeCreateServerInput(input)
	if errs := validateServerFields(in.Name, in.Host, in.Port, in.Protocol, in.Username, in.AuthMethod, in.Environment, in.Provider, in.Location, in.Tags); len(errs) > 0 {
		return nil, errs
	}

	pwEnc, err := s.encryptOptional(in.Password)
	if err != nil {
		return nil, err
	}
	keyEnc, err := s.encryptOptional(in.PrivateKey)
	if err != nil {
		return nil, err
	}
	passEnc, err := s.encryptOptional(in.PrivateKeyPassphrase)
	if err != nil {
		return nil, err
	}

	now := s.clock()
	srv := &domain.Server{
		ID:                            uuid.New(),
		Name:                          in.Name,
		Host:                          in.Host,
		Port:                          in.Port,
		Protocol:                      in.Protocol,
		Username:                      in.Username,
		AuthMethod:                    in.AuthMethod,
		PasswordEncrypted:             pwEnc,
		PrivateKeyEncrypted:           keyEnc,
		PrivateKeyPassphraseEncrypted: passEnc,
		Description:                   in.Description,
		Environment:                  in.Environment,
		Provider:                     in.Provider,
		Location:                     in.Location,
		Tags:                         in.Tags,
		IsActive:                     true, // сервер включён при создании
		CreatedAt:                    now,
		UpdatedAt:                    now,
	}

	if err := s.repo.Create(ctx, srv); err != nil {
		return nil, err // в т.ч. domain.ErrAlreadyExists
	}

	s.logger.Info("server created", "server_id", srv.ID, "name", srv.Name, "host", srv.Host)
	return toServerView(srv), nil
}

// UpdateServer полностью обновляет сервер. Секреты обновляются только если заданы.
func (s *ServerService) UpdateServer(ctx context.Context, input UpdateServerInput) (*ServerView, error) {
	in := normalizeUpdateServerInput(input)

	var errs domain.ValidationErrors
	if in.ID == uuid.Nil {
		errs = append(errs, &domain.ValidationError{Field: "id", Message: "is required"})
	}
	errs = append(errs, validateServerFields(in.Name, in.Host, in.Port, in.Protocol, in.Username, in.AuthMethod, in.Environment, in.Provider, in.Location, in.Tags)...)
	if len(errs) > 0 {
		return nil, errs
	}

	existing, err := s.repo.GetByID(ctx, in.ID)
	if err != nil {
		return nil, err // в т.ч. domain.ErrNotFound
	}

	pwEnc, err := s.resolveSecret(in.Password, existing.PasswordEncrypted)
	if err != nil {
		return nil, err
	}
	keyEnc, err := s.resolveSecret(in.PrivateKey, existing.PrivateKeyEncrypted)
	if err != nil {
		return nil, err
	}
	passEnc, err := s.resolveSecret(in.PrivateKeyPassphrase, existing.PrivateKeyPassphraseEncrypted)
	if err != nil {
		return nil, err
	}

	srv := &domain.Server{
		ID:                            in.ID,
		Name:                          in.Name,
		Host:                          in.Host,
		Port:                          in.Port,
		Protocol:                      in.Protocol,
		Username:                      in.Username,
		AuthMethod:                    in.AuthMethod,
		PasswordEncrypted:             pwEnc,
		PrivateKeyEncrypted:           keyEnc,
		PrivateKeyPassphraseEncrypted: passEnc,
		Description:                   in.Description,
		Environment:                  in.Environment,
		Provider:                     in.Provider,
		Location:                     in.Location,
		Tags:                         in.Tags,
		IsActive:                     in.IsActive,
		// факты (OS/CPU/...) через CRUD не меняются — заполняются при подключении.
	}

	if err := s.repo.Update(ctx, srv); err != nil {
		return nil, err // ErrNotFound / ErrAlreadyExists
	}

	s.logger.Info("server updated", "server_id", srv.ID, "name", srv.Name)
	return toServerView(srv), nil
}

// ListServers возвращает страницу серверов с фильтрами.
func (s *ServerService) ListServers(ctx context.Context, in ListServersInput) (*ListServersOutput, error) {
	var errs domain.ValidationErrors
	env := strings.ToUpper(strings.TrimSpace(in.Environment))
	if env != "" && !domain.IsValidServerEnvironment(env) {
		errs = append(errs, &domain.ValidationError{Field: "environment", Message: "invalid value"})
	}
	proto := strings.ToUpper(strings.TrimSpace(in.Protocol))
	if proto != "" && !domain.IsValidServerProtocol(proto) {
		errs = append(errs, &domain.ValidationError{Field: "protocol", Message: "invalid value"})
	}
	auth := strings.ToUpper(strings.TrimSpace(in.AuthMethod))
	if auth != "" && !domain.IsValidServerAuthMethod(auth) {
		errs = append(errs, &domain.ValidationError{Field: "auth_method", Message: "invalid value"})
	}
	if len(errs) > 0 {
		return nil, errs
	}

	page := in.Page
	if page < 1 {
		page = 1
	}
	size := in.PageSize
	if size < 1 {
		size = defaultServerPageSize
	}
	if size > maxServerPageSize {
		size = maxServerPageSize
	}

	filter := domain.ServerListFilter{
		Environment: env,
		Protocol:    proto,
		AuthMethod:  auth,
		IsActive:    in.IsActive,
		Search:      strings.TrimSpace(in.Search),
		Limit:       size,
		Offset:      (page - 1) * size,
		SortBy:      in.SortBy,
		SortDesc:    strings.ToLower(in.Order) != "asc",
	}

	items, total, err := s.repo.List(ctx, filter)
	if err != nil {
		return nil, err
	}

	totalPages := 0
	if total > 0 {
		totalPages = (total + size - 1) / size
	}

	out := &ListServersOutput{
		Items: make([]ServerView, 0, len(items)),
		Pagination: Pagination{
			Page:       page,
			PageSize:   size,
			Total:      total,
			TotalPages: totalPages,
		},
	}
	for _, srv := range items {
		out.Items = append(out.Items, *toServerView(srv))
	}
	return out, nil
}

// DeleteServer мягко удаляет сервер по ID.
func (s *ServerService) DeleteServer(ctx context.Context, id uuid.UUID) (*DeleteServerOutput, error) {
	if id == uuid.Nil {
		return nil, domain.ValidationErrors{
			&domain.ValidationError{Field: "id", Message: "is required"},
		}
	}
	deletedAt, err := s.repo.SoftDelete(ctx, id)
	if err != nil {
		return nil, err // в т.ч. domain.ErrNotFound
	}
	s.logger.Info("server deleted", "server_id", id)
	return &DeleteServerOutput{ID: id, DeletedAt: deletedAt}, nil
}

// ───────────────────────────── helpers ─────────────────────────────

func (s *ServerService) encryptOptional(plain string) (string, error) {
	if plain == "" {
		return "", nil
	}
	ct, err := s.cipher.Encrypt(plain)
	if err != nil {
		return "", fmt.Errorf("server: encrypt secret: %w", err)
	}
	return ct, nil
}

// resolveSecret: nil → оставить existing; "" → очистить; значение → зашифровать.
func (s *ServerService) resolveSecret(input *string, existing string) (string, error) {
	if input == nil {
		return existing, nil
	}
	if *input == "" {
		return "", nil
	}
	return s.encryptOptional(*input)
}

func toServerView(s *domain.Server) *ServerView {
	return &ServerView{
		ID:               s.ID,
		Name:             s.Name,
		Host:             s.Host,
		Port:             s.Port,
		Protocol:         s.Protocol,
		Username:         s.Username,
		AuthMethod:       s.AuthMethod,
		Description:      s.Description,
		Environment:      s.Environment,
		Provider:         s.Provider,
		Location:         s.Location,
		Tags:             s.Tags,
		OS:               s.OS,
		OSVersion:        s.OSVersion,
		Arch:             s.Arch,
		KernelVersion:    s.KernelVersion,
		RemoteHostname:   s.RemoteHostname,
		CPUCores:         s.CPUCores,
		MemoryTotalBytes: s.MemoryTotalBytes,
		DiskTotalBytes:   s.DiskTotalBytes,
		HasPassword:      s.PasswordEncrypted != "",
		HasPrivateKey:    s.PrivateKeyEncrypted != "",
		IsActive:         s.IsActive,
		SSHKeyInstalled:  s.SSHKeyInstalled,
		LastCheckedAt:    s.LastCheckedAt,
		LastStatus:       s.LastStatus,
		CreatedAt:        s.CreatedAt,
		UpdatedAt:        s.UpdatedAt,
	}
}

func normalizeCreateServerInput(in CreateServerInput) CreateServerInput {
	out := CreateServerInput{
		Name:                 strings.TrimSpace(in.Name),
		Host:                 strings.TrimSpace(in.Host),
		Port:                 in.Port,
		Protocol:             strings.ToUpper(strings.TrimSpace(in.Protocol)),
		Username:             strings.TrimSpace(in.Username),
		AuthMethod:           strings.ToUpper(strings.TrimSpace(in.AuthMethod)),
		Password:             in.Password,
		PrivateKey:           in.PrivateKey,
		PrivateKeyPassphrase: in.PrivateKeyPassphrase,
		Description:          strings.TrimSpace(in.Description),
		Environment:          strings.ToUpper(strings.TrimSpace(in.Environment)),
		Provider:             strings.TrimSpace(in.Provider),
		Location:             strings.TrimSpace(in.Location),
		Tags:                 normalizeTags(in.Tags),
	}
	applyServerDefaults(&out.Port, &out.Protocol, &out.AuthMethod, &out.Environment)
	return out
}

func normalizeUpdateServerInput(in UpdateServerInput) UpdateServerInput {
	out := UpdateServerInput{
		ID:                   in.ID,
		Name:                 strings.TrimSpace(in.Name),
		Host:                 strings.TrimSpace(in.Host),
		Port:                 in.Port,
		Protocol:             strings.ToUpper(strings.TrimSpace(in.Protocol)),
		Username:             strings.TrimSpace(in.Username),
		AuthMethod:           strings.ToUpper(strings.TrimSpace(in.AuthMethod)),
		Password:             in.Password,
		PrivateKey:           in.PrivateKey,
		PrivateKeyPassphrase: in.PrivateKeyPassphrase,
		Description:          strings.TrimSpace(in.Description),
		Environment:          strings.ToUpper(strings.TrimSpace(in.Environment)),
		Provider:             strings.TrimSpace(in.Provider),
		Location:             strings.TrimSpace(in.Location),
		Tags:                 normalizeTags(in.Tags),
		IsActive:             in.IsActive,
	}
	applyServerDefaults(&out.Port, &out.Protocol, &out.AuthMethod, &out.Environment)
	return out
}

// applyServerDefaults проставляет дефолты для незаданных полей.
func applyServerDefaults(port *int, protocol, authMethod, environment *string) {
	if *port == 0 {
		*port = defaultSSHPort
	}
	if *protocol == "" {
		*protocol = domain.ServerProtocolSSH
	}
	if *authMethod == "" {
		*authMethod = domain.ServerAuthPassword
	}
	if *environment == "" {
		*environment = domain.ServerEnvProduction
	}
}

func normalizeTags(tags []string) []string {
	if len(tags) == 0 {
		return nil
	}
	out := make([]string, 0, len(tags))
	for _, t := range tags {
		t = strings.TrimSpace(t)
		if t != "" {
			out = append(out, t)
		}
	}
	return out
}

func validateServerFields(name, host string, port int, protocol, username, authMethod, environment, provider, location string, tags []string) domain.ValidationErrors {
	var errs domain.ValidationErrors

	switch {
	case name == "":
		errs = append(errs, &domain.ValidationError{Field: "name", Message: "is required"})
	default:
		if l := utf8.RuneCountInString(name); l < 2 || l > 100 {
			errs = append(errs, &domain.ValidationError{Field: "name", Message: "must be between 2 and 100 characters"})
		}
	}

	switch {
	case host == "":
		errs = append(errs, &domain.ValidationError{Field: "host", Message: "is required"})
	default:
		if utf8.RuneCountInString(host) > 255 {
			errs = append(errs, &domain.ValidationError{Field: "host", Message: "must be at most 255 characters"})
		}
	}

	if port < 1 || port > 65535 {
		errs = append(errs, &domain.ValidationError{Field: "port", Message: "must be between 1 and 65535"})
	}

	if !domain.IsValidServerProtocol(protocol) {
		errs = append(errs, &domain.ValidationError{Field: "protocol", Message: "must be one of SSH, WINRM, RDP"})
	}
	if !domain.IsValidServerAuthMethod(authMethod) {
		errs = append(errs, &domain.ValidationError{Field: "auth_method", Message: "must be one of PASSWORD, PRIVATE_KEY, AGENT"})
	}
	if !domain.IsValidServerEnvironment(environment) {
		errs = append(errs, &domain.ValidationError{Field: "environment", Message: "must be one of PRODUCTION, STAGING, DEVELOPMENT, TESTING, OTHER"})
	}

	if username != "" && utf8.RuneCountInString(username) > 255 {
		errs = append(errs, &domain.ValidationError{Field: "username", Message: "must be at most 255 characters"})
	}
	if provider != "" && utf8.RuneCountInString(provider) > 100 {
		errs = append(errs, &domain.ValidationError{Field: "provider", Message: "must be at most 100 characters"})
	}
	if location != "" && utf8.RuneCountInString(location) > 100 {
		errs = append(errs, &domain.ValidationError{Field: "location", Message: "must be at most 100 characters"})
	}

	if len(tags) > maxServerTags {
		errs = append(errs, &domain.ValidationError{Field: "tags", Message: fmt.Sprintf("must be at most %d tags", maxServerTags)})
	}
	for _, t := range tags {
		if utf8.RuneCountInString(t) > maxServerTagLen {
			errs = append(errs, &domain.ValidationError{Field: "tags", Message: fmt.Sprintf("each tag must be at most %d characters", maxServerTagLen)})
			break
		}
	}

	return errs
}
