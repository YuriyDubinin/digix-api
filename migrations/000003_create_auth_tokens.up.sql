-- Тип выдаваемого токена. Пока ACCESS/REFRESH — можно расширить в будущем
-- (например, API_KEY) через ALTER TYPE ... ADD VALUE.
CREATE TYPE auth_token_type AS ENUM (
    'ACCESS',
    'REFRESH'
);

-- Категория устройства, с которого выдан токен. UNKNOWN — если не удалось
-- распознать по User-Agent и клиент не передал явно.
CREATE TYPE auth_device_type AS ENUM (
    'WEB',
    'MOBILE',
    'TABLET',
    'DESKTOP',
    'API',
    'UNKNOWN'
);

CREATE TABLE auth_tokens (
    id                UUID PRIMARY KEY DEFAULT uuid_generate_v4(),

    -- Связь с сотрудником, которому выдан токен.
    -- ON DELETE CASCADE: при физическом удалении сотрудника все его токены
    -- автоматически инвалидируются. Soft-delete (status = DISABLED) FK не задевает,
    -- логика отзыва токенов при дизейбле — на стороне приложения.
    employee_id       UUID NOT NULL REFERENCES employees(id) ON DELETE CASCADE,

    -- Храним хэш токена, а не сам токен.
    -- Это стандартная практика: при компрометации БД злоумышленник не получит
    -- действующие токены. Сам токен видит только клиент (один раз — в ответе на логин).
    token_hash        TEXT NOT NULL UNIQUE,
    token_type        auth_token_type NOT NULL DEFAULT 'ACCESS',

    -- Жизненный цикл токена
    issued_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at        TIMESTAMPTZ NOT NULL,
    last_used_at      TIMESTAMPTZ,
    revoked_at        TIMESTAMPTZ,
    revoked_reason    TEXT,

    -- Сетевая информация на момент выдачи / последнего использования
    ip_address        INET,
    user_agent        TEXT,

    -- Информация об устройстве и системе клиента
    device_type       auth_device_type NOT NULL DEFAULT 'UNKNOWN',
    device_name       VARCHAR(255),
    os                VARCHAR(100),
    os_version        VARCHAR(50),
    browser           VARCHAR(100),
    browser_version   VARCHAR(50),
    app_version       VARCHAR(50),

    -- Геопозиция (опционально, для аудита и определения подозрительных входов)
    country_code      CHAR(2),
    city              VARCHAR(100),

    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Все токены конкретного сотрудника (список сессий, отзыв всех при logout-all).
CREATE INDEX idx_auth_tokens_employee_id
    ON auth_tokens (employee_id);

-- Очистка просроченных токенов фоновой задачей.
CREATE INDEX idx_auth_tokens_expires_at
    ON auth_tokens (expires_at);

-- Активные сессии сотрудника. Частичный индекс маленький и быстрый,
-- т.к. отозванные/старые токены в него не попадают.
CREATE INDEX idx_auth_tokens_active
    ON auth_tokens (employee_id)
    WHERE revoked_at IS NULL;

-- Автообновление updated_at при любом UPDATE.
-- Переиспользуем функцию trigger_set_updated_at(), созданную в миграции 000001.
CREATE TRIGGER set_updated_at_auth_tokens
    BEFORE UPDATE ON auth_tokens
    FOR EACH ROW
    EXECUTE FUNCTION trigger_set_updated_at();

-- При переводе сотрудника в статус DISABLED все его активные токены
-- помечаются как отозванные (revoked_at = NOW, revoked_reason = 'employee disabled').
-- Сами строки в auth_tokens НЕ удаляются — остаются в БД для аудита,
-- но проверка валидности (revoked_at IS NULL AND expires_at > NOW()) их отсеет.
CREATE OR REPLACE FUNCTION trigger_revoke_tokens_on_employee_disable()
RETURNS TRIGGER AS $$
BEGIN
    IF NEW.status = 'DISABLED' AND OLD.status IS DISTINCT FROM 'DISABLED' THEN
        UPDATE auth_tokens
        SET revoked_at = NOW(),
            revoked_reason = 'employee disabled'
        WHERE employee_id = NEW.id
          AND revoked_at IS NULL;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER revoke_tokens_on_employee_disable
    AFTER UPDATE ON employees
    FOR EACH ROW
    EXECUTE FUNCTION trigger_revoke_tokens_on_employee_disable();
