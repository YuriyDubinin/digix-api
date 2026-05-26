DROP TRIGGER IF EXISTS revoke_tokens_on_employee_disable ON employees;
DROP FUNCTION IF EXISTS trigger_revoke_tokens_on_employee_disable();
DROP TRIGGER IF EXISTS set_updated_at_auth_tokens ON auth_tokens;
DROP TABLE IF EXISTS auth_tokens;
DROP TYPE IF EXISTS auth_device_type;
DROP TYPE IF EXISTS auth_token_type;
