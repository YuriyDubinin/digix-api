-- Протокол подключения. SSH — основной; WINRM/RDP — задел на будущее.
CREATE TYPE server_protocol AS ENUM (
    'SSH',
    'WINRM',
    'RDP'
);

-- Способ аутентификации.
CREATE TYPE server_auth_method AS ENUM (
    'PASSWORD',
    'PRIVATE_KEY',
    'AGENT'
);

-- Окружение сервера (для классификации/фильтров).
CREATE TYPE server_environment AS ENUM (
    'PRODUCTION',
    'STAGING',
    'DEVELOPMENT',
    'TESTING',
    'OTHER'
);

CREATE TABLE servers (
    id                                UUID PRIMARY KEY DEFAULT uuid_generate_v4(),

    -- ───────── Идентификация и подключение ─────────
    name                              VARCHAR(100) UNIQUE NOT NULL, -- человекочитаемое имя
    host                              VARCHAR(255) NOT NULL,        -- IP или hostname
    port                              INTEGER NOT NULL DEFAULT 22,
    protocol                          server_protocol NOT NULL DEFAULT 'SSH',
    username                          VARCHAR(255),
    auth_method                       server_auth_method NOT NULL DEFAULT 'PASSWORD',

    -- ───────── Секреты (ШИФРТЕКСТ AES-GCM, не плейнтекст) ─────────
    password_encrypted                TEXT,
    private_key_encrypted             TEXT,
    private_key_passphrase_encrypted  TEXT,

    -- ───────── Описание и классификация ─────────
    description                       TEXT,
    environment                       server_environment NOT NULL DEFAULT 'PRODUCTION',
    provider                          VARCHAR(100),  -- AWS, Hetzner, DigitalOcean, self-hosted...
    location                          VARCHAR(100),  -- регион / датацентр
    tags                              TEXT[],        -- произвольные метки

    -- ───────── Факты о сервере (заполняются после подключения) ─────────
    os                                VARCHAR(100),
    os_version                        VARCHAR(100),
    arch                              VARCHAR(50),
    kernel_version                    VARCHAR(100),
    remote_hostname                   VARCHAR(255),  -- hostname, сообщённый самим сервером
    cpu_cores                         INTEGER,
    memory_total_bytes                BIGINT,
    disk_total_bytes                  BIGINT,

    -- ───────── Состояние / здоровье подключения ─────────
    is_active                         BOOLEAN NOT NULL DEFAULT TRUE,
    last_checked_at                   TIMESTAMPTZ,
    last_status                       VARCHAR(20),   -- OK | UNREACHABLE | AUTH_FAILED | TIMEOUT | ...
    last_error                        TEXT,

    created_at                        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at                        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at                        TIMESTAMPTZ,

    CONSTRAINT servers_port_range CHECK (port BETWEEN 1 AND 65535)
);

-- Список активных (не удалённых) серверов.
CREATE INDEX idx_servers_active
    ON servers (is_active)
    WHERE deleted_at IS NULL;

-- Фильтр по окружению.
CREATE INDEX idx_servers_environment
    ON servers (environment)
    WHERE deleted_at IS NULL;

-- Поиск по хосту.
CREATE INDEX idx_servers_host
    ON servers (host);

-- Автообновление updated_at. Переиспользуем функцию из миграции 000001.
CREATE TRIGGER set_updated_at_servers
    BEFORE UPDATE ON servers
    FOR EACH ROW
    EXECUTE FUNCTION trigger_set_updated_at();
