-- Тип registry. GENERIC — catch-all под любой registry, не попавший в список.
-- Расширяется через ALTER TYPE registry_type ADD VALUE '...'.
CREATE TYPE registry_type AS ENUM (
    'DOCKERHUB',
    'GHCR',
    'GITLAB',
    'HARBOR',
    'ECR',
    'GENERIC'
);

CREATE TABLE registries (
    id                 UUID PRIMARY KEY DEFAULT uuid_generate_v4(),

    -- Человекочитаемое имя подключения ("DockerHub prod", "Private GHCR").
    name               VARCHAR(100) UNIQUE NOT NULL,
    type               registry_type NOT NULL DEFAULT 'GENERIC',

    -- Endpoint registry. Примеры:
    --   DockerHub:  https://registry-1.docker.io
    --   GHCR:       https://ghcr.io
    --   свой:       https://registry.example.com:5000
    url                VARCHAR(500) NOT NULL,

    -- Учётные данные. NULL — анонимный доступ к публичному registry.
    -- password_encrypted хранит ШИФРТЕКСТ (AES-GCM), не открытый пароль.
    -- Сюда же кладётся токен, если registry авторизуется по токену.
    username           VARCHAR(255),
    password_encrypted TEXT,
    email              VARCHAR(255),    -- опционально (legacy DockerHub)

    -- Дефолтный namespace / организация / проект для просмотра образов.
    namespace          VARCHAR(255),

    is_default         BOOLEAN NOT NULL DEFAULT FALSE, -- registry по умолчанию
    is_active          BOOLEAN NOT NULL DEFAULT TRUE,  -- включён / выключен
    insecure           BOOLEAN NOT NULL DEFAULT FALSE, -- разрешить http / self-signed TLS

    -- Аудит состояния подключения (заполняется при health-check'е).
    last_checked_at    TIMESTAMPTZ,
    last_status        VARCHAR(20),     -- OK | AUTH_FAILED | UNREACHABLE | TLS_ERROR | ...
    last_error         TEXT,

    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at         TIMESTAMPTZ
);

-- Быстрый список активных (не удалённых) registry.
CREATE INDEX idx_registries_active
    ON registries (is_active)
    WHERE deleted_at IS NULL;

-- Гарантия: не более одного registry по умолчанию среди живых записей.
CREATE UNIQUE INDEX idx_registries_single_default
    ON registries (is_default)
    WHERE is_default = TRUE AND deleted_at IS NULL;

-- Автообновление updated_at. Переиспользуем функцию trigger_set_updated_at()
-- из миграции 000001.
CREATE TRIGGER set_updated_at_registries
    BEFORE UPDATE ON registries
    FOR EACH ROW
    EXECUTE FUNCTION trigger_set_updated_at();
