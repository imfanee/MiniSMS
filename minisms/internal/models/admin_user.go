// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package models

import "time"

// AdminUser is a row from admin_users.
type AdminUser struct {
	AdminUserID  string
	Username     string
	PasswordHash string
	DisplayName  string
	Email        *string
	Phone        *string
	IsActive     bool
	IsSuperAdmin bool
	Permissions  []string
	CreatedAt    time.Time
	UpdatedAt    time.Time
	LastLoginAt  *time.Time
}
