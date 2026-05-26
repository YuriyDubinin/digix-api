package domain

import (
	"time"

	"github.com/google/uuid"
)

// Employee — сотрудник системы (строка из таблицы employees).
// PasswordHash хранится как bcrypt-результат (см. pkg/crypto.PasswordHasher).
type Employee struct {
	ID           uuid.UUID
	FullName     string
	Email        string
	Phone        string
	Role         string // ENUM employee_role
	Status       string // ENUM employee_status
	PasswordHash string
	CreatedAt    time.Time
	UpdatedAt    time.Time
	DeletedAt    *time.Time
}

// IsActive — true, если сотрудник может пользоваться системой:
// status = ENABLED и нет soft-delete метки.
func (e *Employee) IsActive() bool {
	return e.Status == EmployeeStatusEnabled && e.DeletedAt == nil
}

// Значения ENUM employee_status из миграции 000002.
const (
	EmployeeStatusEnabled  = "ENABLED"
	EmployeeStatusDisabled = "DISABLED"
)

// Значения ENUM employee_role из миграции 000002.
const (
	EmployeeRoleOwner   = "OWNER"
	EmployeeRoleAdmin   = "ADMIN"
	EmployeeRoleManager = "MANAGER"
	EmployeeRoleSupport = "SUPPORT"
	EmployeeRoleViewer  = "VIEWER"
)
