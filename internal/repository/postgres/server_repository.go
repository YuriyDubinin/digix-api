package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/YuriyDubinin/dijex-api/internal/domain"
)

type ServerRepository struct {
	pool *pgxpool.Pool
}

func NewServerRepository(pool *pgxpool.Pool) *ServerRepository {
	return &ServerRepository{pool: pool}
}

// serverColumns — порядок колонок для чтения сервера (см. scanServer).
const serverColumns = `
	id, name, host, port, protocol::text, COALESCE(username, ''), auth_method::text,
	COALESCE(password_encrypted, ''), COALESCE(private_key_encrypted, ''), COALESCE(private_key_passphrase_encrypted, ''),
	COALESCE(description, ''), environment::text, COALESCE(provider, ''), COALESCE(location, ''), COALESCE(tags, '{}'),
	COALESCE(os, ''), COALESCE(os_version, ''), COALESCE(arch, ''), COALESCE(kernel_version, ''), COALESCE(remote_hostname, ''),
	cpu_cores, memory_total_bytes, disk_total_bytes,
	COALESCE(remote_public_ip, ''), COALESCE(country_code, ''), COALESCE(country, ''),
	is_active, ssh_key_installed, last_checked_at, COALESCE(last_status, ''), COALESCE(last_error, ''),
	created_at, updated_at
`

func scanServer(row pgx.Row) (*domain.Server, error) {
	var s domain.Server
	err := row.Scan(
		&s.ID, &s.Name, &s.Host, &s.Port, &s.Protocol, &s.Username, &s.AuthMethod,
		&s.PasswordEncrypted, &s.PrivateKeyEncrypted, &s.PrivateKeyPassphraseEncrypted,
		&s.Description, &s.Environment, &s.Provider, &s.Location, &s.Tags,
		&s.OS, &s.OSVersion, &s.Arch, &s.KernelVersion, &s.RemoteHostname,
		&s.CPUCores, &s.MemoryTotalBytes, &s.DiskTotalBytes,
		&s.RemotePublicIP, &s.CountryCode, &s.Country,
		&s.IsActive, &s.SSHKeyInstalled, &s.LastCheckedAt, &s.LastStatus, &s.LastError,
		&s.CreatedAt, &s.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func (r *ServerRepository) Create(ctx context.Context, s *domain.Server) error {
	const query = `
		INSERT INTO servers
			(id, name, host, port, protocol, username, auth_method,
			 password_encrypted, private_key_encrypted, private_key_passphrase_encrypted,
			 description, environment, provider, location, tags,
			 is_active, created_at, updated_at)
		VALUES
			($1, $2, $3, $4, $5::server_protocol, NULLIF($6, ''), $7::server_auth_method,
			 NULLIF($8, ''), NULLIF($9, ''), NULLIF($10, ''),
			 NULLIF($11, ''), $12::server_environment, NULLIF($13, ''), NULLIF($14, ''), $15,
			 $16, $17, $18)
	`
	_, err := r.pool.Exec(ctx, query,
		s.ID, s.Name, s.Host, s.Port, s.Protocol, s.Username, s.AuthMethod,
		s.PasswordEncrypted, s.PrivateKeyEncrypted, s.PrivateKeyPassphraseEncrypted,
		s.Description, s.Environment, s.Provider, s.Location, s.Tags,
		s.IsActive, s.CreatedAt, s.UpdatedAt,
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == uniqueViolationCode {
			return fmt.Errorf("postgres: create server: %w", domain.ErrAlreadyExists)
		}
		return fmt.Errorf("postgres: create server: %w", err)
	}
	return nil
}

func (r *ServerRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.Server, error) {
	query := "SELECT " + serverColumns + " FROM servers WHERE id = $1 AND deleted_at IS NULL"
	srv, err := scanServer(r.pool.QueryRow(ctx, query, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("postgres: get server %s: %w", id, domain.ErrNotFound)
		}
		return nil, fmt.Errorf("postgres: get server %s: %w", id, err)
	}
	return srv, nil
}

func (r *ServerRepository) Update(ctx context.Context, s *domain.Server) error {
	const query = `
		UPDATE servers SET
			name                             = $1,
			host                             = $2,
			port                             = $3,
			protocol                         = $4::server_protocol,
			username                         = NULLIF($5, ''),
			auth_method                      = $6::server_auth_method,
			password_encrypted               = NULLIF($7, ''),
			private_key_encrypted            = NULLIF($8, ''),
			private_key_passphrase_encrypted = NULLIF($9, ''),
			description                      = NULLIF($10, ''),
			environment                      = $11::server_environment,
			provider                         = NULLIF($12, ''),
			location                         = NULLIF($13, ''),
			tags                             = $14,
			is_active                        = $15
		WHERE id = $16 AND deleted_at IS NULL
		RETURNING created_at, updated_at, ssh_key_installed
	`
	err := r.pool.QueryRow(ctx, query,
		s.Name, s.Host, s.Port, s.Protocol, s.Username, s.AuthMethod,
		s.PasswordEncrypted, s.PrivateKeyEncrypted, s.PrivateKeyPassphraseEncrypted,
		s.Description, s.Environment, s.Provider, s.Location, s.Tags,
		s.IsActive, s.ID,
	).Scan(&s.CreatedAt, &s.UpdatedAt, &s.SSHKeyInstalled)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("postgres: update server %s: %w", s.ID, domain.ErrNotFound)
		}
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == uniqueViolationCode {
			return fmt.Errorf("postgres: update server: %w", domain.ErrAlreadyExists)
		}
		return fmt.Errorf("postgres: update server: %w", err)
	}
	return nil
}

func (r *ServerRepository) SoftDelete(ctx context.Context, id uuid.UUID) (time.Time, error) {
	const query = `
		UPDATE servers
		SET deleted_at = NOW()
		WHERE id = $1 AND deleted_at IS NULL
		RETURNING deleted_at
	`
	var deletedAt time.Time
	if err := r.pool.QueryRow(ctx, query, id).Scan(&deletedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return time.Time{}, fmt.Errorf("postgres: soft delete server %s: %w", id, domain.ErrNotFound)
		}
		return time.Time{}, fmt.Errorf("postgres: soft delete server %s: %w", id, err)
	}
	return deletedAt, nil
}

// UpdateConnectionStatus пишет итог проверки подключения. setActive: nil —
// не менять is_active; иначе установить значение (COALESCE).
func (r *ServerRepository) UpdateConnectionStatus(ctx context.Context, id uuid.UUID, status, errMsg string, checkedAt time.Time, setActive *bool) error {
	const query = `
		UPDATE servers
		SET last_checked_at = $1,
		    last_status     = $2,
		    last_error      = NULLIF($3, ''),
		    is_active       = COALESCE($5, is_active)
		WHERE id = $4 AND deleted_at IS NULL
	`
	if _, err := r.pool.Exec(ctx, query, checkedAt, status, errMsg, id, setActive); err != nil {
		return fmt.Errorf("postgres: update server connection status %s: %w", id, err)
	}
	return nil
}

// UpdateFacts сохраняет факты о сервере (пустые строки → NULL, cpu_cores как есть).
// Гео-поля (remote_public_ip / country_code / country) обновляются вместе с
// остальными — они тоже факты, собираемые при /api/servers/remote/connect.
func (r *ServerRepository) UpdateFacts(ctx context.Context, id uuid.UUID, f domain.ServerFacts) error {
	const query = `
		UPDATE servers
		SET os               = NULLIF($1, ''),
		    os_version       = NULLIF($2, ''),
		    arch             = NULLIF($3, ''),
		    kernel_version   = NULLIF($4, ''),
		    remote_hostname  = NULLIF($5, ''),
		    cpu_cores        = $6,
		    remote_public_ip = NULLIF($7, ''),
		    country_code     = NULLIF($8, ''),
		    country          = NULLIF($9, '')
		WHERE id = $10 AND deleted_at IS NULL
	`
	if _, err := r.pool.Exec(ctx, query,
		f.OS, f.OSVersion, f.Arch, f.KernelVersion, f.RemoteHostname, f.CPUCores,
		f.RemotePublicIP, f.CountryCode, f.Country,
		id,
	); err != nil {
		return fmt.Errorf("postgres: update server facts %s: %w", id, err)
	}
	return nil
}

// MarkSSHKeyInstalled выставляет ssh_key_installed для сервера. При
// installed=true одновременно переключает auth_method на PRIVATE_KEY —
// если ключ верифицирован, дальше подключения должны идти именно по нему.
// При installed=false auth_method не трогается (нет смысла откатывать выбор
// пользователя из-за неуспешной проверки).
// ErrNotFound, если сервера нет (или soft-deleted).
func (r *ServerRepository) MarkSSHKeyInstalled(ctx context.Context, id uuid.UUID, installed bool) error {
	const query = `
		UPDATE servers
		SET ssh_key_installed = $2,
		    auth_method = CASE
		        WHEN $2 THEN 'PRIVATE_KEY'::server_auth_method
		        ELSE auth_method
		    END
		WHERE id = $1 AND deleted_at IS NULL
	`
	tag, err := r.pool.Exec(ctx, query, id, installed)
	if err != nil {
		return fmt.Errorf("postgres: mark ssh_key_installed for %s: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("postgres: mark ssh_key_installed for %s: %w", id, domain.ErrNotFound)
	}
	return nil
}

var serverSortColumns = map[string]string{
	"created_at":  "created_at",
	"updated_at":  "updated_at",
	"name":        "name",
	"host":        "host",
	"environment": "environment",
}

func serverSortColumn(s string) string {
	if col, ok := serverSortColumns[s]; ok {
		return col
	}
	return "created_at"
}

func (r *ServerRepository) List(ctx context.Context, f domain.ServerListFilter) ([]*domain.Server, int, error) {
	conds := []string{"deleted_at IS NULL"}
	args := []any{}

	if f.Environment != "" {
		args = append(args, f.Environment)
		conds = append(conds, fmt.Sprintf("environment = $%d::server_environment", len(args)))
	}
	if f.Protocol != "" {
		args = append(args, f.Protocol)
		conds = append(conds, fmt.Sprintf("protocol = $%d::server_protocol", len(args)))
	}
	if f.AuthMethod != "" {
		args = append(args, f.AuthMethod)
		conds = append(conds, fmt.Sprintf("auth_method = $%d::server_auth_method", len(args)))
	}
	if f.IsActive != nil {
		args = append(args, *f.IsActive)
		conds = append(conds, fmt.Sprintf("is_active = $%d", len(args)))
	}
	if f.Search != "" {
		args = append(args, "%"+f.Search+"%")
		conds = append(conds, fmt.Sprintf("(name ILIKE $%d OR host ILIKE $%d)", len(args), len(args)))
	}
	where := "WHERE " + strings.Join(conds, " AND ")

	var total int
	if err := r.pool.QueryRow(ctx, "SELECT COUNT(*) FROM servers "+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("postgres: count servers: %w", err)
	}
	if total == 0 {
		return []*domain.Server{}, 0, nil
	}

	dir := "ASC"
	if f.SortDesc {
		dir = "DESC"
	}
	args = append(args, f.Limit)
	limitPos := len(args)
	args = append(args, f.Offset)
	offsetPos := len(args)

	query := fmt.Sprintf(
		"SELECT %s FROM servers %s ORDER BY %s %s, id LIMIT $%d OFFSET $%d",
		serverColumns, where, serverSortColumn(f.SortBy), dir, limitPos, offsetPos,
	)

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("postgres: list servers: %w", err)
	}
	defer rows.Close()

	var items []*domain.Server
	for rows.Next() {
		srv, err := scanServer(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("postgres: scan server: %w", err)
		}
		items = append(items, srv)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("postgres: iterate servers: %w", err)
	}
	return items, total, nil
}
