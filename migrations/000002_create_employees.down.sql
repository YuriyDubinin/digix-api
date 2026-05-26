DROP TRIGGER IF EXISTS sync_deleted_at_employees ON employees;
DROP TRIGGER IF EXISTS set_updated_at_employees ON employees;
DROP FUNCTION IF EXISTS trigger_sync_employee_deleted_at();
DROP TABLE IF EXISTS employees;
DROP TYPE IF EXISTS employee_status;
DROP TYPE IF EXISTS employee_role;
