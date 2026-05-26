CREATE TYPE employee_role AS ENUM (
  'OWNER',
  'ADMIN',
  'MANAGER',
  'SUPPORT',
  'VIEWER'
);

CREATE TYPE employee_status AS ENUM (
  'ENABLED',
  'DISABLED'
);

CREATE TABLE employees (
  id              UUID PRIMARY KEY DEFAULT uuid_generate_v4(),

  full_name       VARCHAR(100) NOT NULL,
  email           VARCHAR(255) UNIQUE NOT NULL,
  phone           VARCHAR(30),

  role            employee_role NOT NULL DEFAULT 'VIEWER',
  status          employee_status NOT NULL DEFAULT 'ENABLED',

  password_hash   TEXT NOT NULL,

  created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  deleted_at      TIMESTAMPTZ
);

-- Автоматическое обновление updated_at при любом UPDATE.
-- Переиспользуем функцию trigger_set_updated_at(), созданную в миграции 000001.
CREATE TRIGGER set_updated_at_employees
    BEFORE UPDATE ON employees
    FOR EACH ROW
    EXECUTE FUNCTION trigger_set_updated_at();

-- При смене status на 'DISABLED' проставляем deleted_at = NOW().
-- Если статус возвращается обратно в 'ENABLED' — обнуляем deleted_at.
CREATE OR REPLACE FUNCTION trigger_sync_employee_deleted_at()
RETURNS TRIGGER AS $$
BEGIN
    IF NEW.status = 'DISABLED' AND OLD.status IS DISTINCT FROM 'DISABLED' THEN
        NEW.deleted_at := NOW();
    ELSIF NEW.status = 'ENABLED' AND OLD.status IS DISTINCT FROM 'ENABLED' THEN
        NEW.deleted_at := NULL;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER sync_deleted_at_employees
    BEFORE UPDATE ON employees
    FOR EACH ROW
    EXECUTE FUNCTION trigger_sync_employee_deleted_at();
