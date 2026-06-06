// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"

	"github.com/minisms/minisms/internal/adminauth"
	"github.com/minisms/minisms/internal/models"
)

// EnsureBootstrapSuperAdmin inserts the env-configured super admin when no admin users exist.
func EnsureBootstrapSuperAdmin(ctx context.Context, pool *pgxpool.Pool, username, passwordHash string) error {
	var n int64
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM admin_users`).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	username = strings.TrimSpace(username)
	if username == "" || passwordHash == "" {
		return fmt.Errorf("bootstrap admin: ADMIN_USERNAME and ADMIN_PASSWORD_HASH required when no admin users exist")
	}
	_, err := pool.Exec(ctx, `
		INSERT INTO admin_users (username, password_hash, display_name, is_super_admin, permissions)
		VALUES ($1, $2, $3, true, '[]'::jsonb)`,
		username, passwordHash, username,
	)
	if err != nil {
		return fmt.Errorf("bootstrap super admin: %w", err)
	}
	return nil
}

// AuthenticateAdmin returns the user when username/password match and account is active.
func AuthenticateAdmin(ctx context.Context, pool *pgxpool.Pool, username, password string) (*models.AdminUser, error) {
	u, err := GetAdminUserByUsername(ctx, pool, username)
	if err != nil {
		return nil, err
	}
	if u == nil || !u.IsActive {
		return nil, nil
	}
	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)); err != nil {
		return nil, nil
	}
	return u, nil
}

func scanAdminUser(row pgx.Row) (*models.AdminUser, error) {
	var u models.AdminUser
	var email, phone *string
	var permsJSON []byte
	var lastLogin *time.Time
	err := row.Scan(
		&u.AdminUserID, &u.Username, &u.PasswordHash, &u.DisplayName,
		&email, &phone, &u.IsActive, &u.IsSuperAdmin, &permsJSON,
		&u.CreatedAt, &u.UpdatedAt, &lastLogin,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	u.Email = email
	u.Phone = phone
	u.LastLoginAt = lastLogin
	if len(permsJSON) > 0 {
		_ = json.Unmarshal(permsJSON, &u.Permissions)
	}
	return &u, nil
}

const adminUserSelect = `
	SELECT admin_user_id::text, username, password_hash, display_name,
	       email, phone, is_active, is_super_admin, permissions,
	       created_at, updated_at, last_login_at
	FROM admin_users`

// GetAdminUserByID loads one admin user.
func GetAdminUserByID(ctx context.Context, pool *pgxpool.Pool, id string) (*models.AdminUser, error) {
	return scanAdminUser(pool.QueryRow(ctx, adminUserSelect+` WHERE admin_user_id = $1::uuid`, id))
}

// GetAdminUserByUsername loads one admin user (case-sensitive username match).
func GetAdminUserByUsername(ctx context.Context, pool *pgxpool.Pool, username string) (*models.AdminUser, error) {
	username = strings.TrimSpace(username)
	if username == "" {
		return nil, nil
	}
	return scanAdminUser(pool.QueryRow(ctx, adminUserSelect+` WHERE username = $1`, username))
}

// ListAdminUsers returns all admin accounts ordered by username.
func ListAdminUsers(ctx context.Context, pool *pgxpool.Pool) ([]models.AdminUser, error) {
	rows, err := pool.Query(ctx, adminUserSelect+` ORDER BY username`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []models.AdminUser
	for rows.Next() {
		var u models.AdminUser
		var email, phone *string
		var permsJSON []byte
		var lastLogin *time.Time
		if err := rows.Scan(
			&u.AdminUserID, &u.Username, &u.PasswordHash, &u.DisplayName,
			&email, &phone, &u.IsActive, &u.IsSuperAdmin, &permsJSON,
			&u.CreatedAt, &u.UpdatedAt, &lastLogin,
		); err != nil {
			return nil, err
		}
		u.Email = email
		u.Phone = phone
		u.LastLoginAt = lastLogin
		if len(permsJSON) > 0 {
			_ = json.Unmarshal(permsJSON, &u.Permissions)
		}
		list = append(list, u)
	}
	return list, rows.Err()
}

// AdminUserInput is data for create/update.
type AdminUserInput struct {
	Username     string
	DisplayName  string
	Email        string
	Phone        string
	IsActive     bool
	IsSuperAdmin bool
	Permissions  []string
	Password     string // plain; empty on update means keep existing
}

// CreateAdminUser inserts a new admin with bcrypt password.
func CreateAdminUser(ctx context.Context, pool *pgxpool.Pool, in AdminUserInput) (string, error) {
	hash, err := hashPassword(in.Password)
	if err != nil {
		return "", err
	}
	perms, err := json.Marshal(in.Permissions)
	if err != nil {
		return "", err
	}
	var id string
	err = pool.QueryRow(ctx, `
		INSERT INTO admin_users (username, password_hash, display_name, email, phone, is_active, is_super_admin, permissions)
		VALUES ($1, $2, $3, NULLIF($4,''), NULLIF($5,''), $6, $7, $8::jsonb)
		RETURNING admin_user_id::text`,
		strings.TrimSpace(in.Username), hash, strings.TrimSpace(in.DisplayName),
		strings.TrimSpace(in.Email), strings.TrimSpace(in.Phone),
		in.IsActive, in.IsSuperAdmin, perms,
	).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("create admin user: %w", err)
	}
	return id, nil
}

// UpdateAdminUser updates profile, permissions, and optionally password.
func UpdateAdminUser(ctx context.Context, pool *pgxpool.Pool, id string, in AdminUserInput) error {
	perms, err := json.Marshal(in.Permissions)
	if err != nil {
		return err
	}
	if strings.TrimSpace(in.Password) != "" {
		hash, err := hashPassword(in.Password)
		if err != nil {
			return err
		}
		ct, err := pool.Exec(ctx, `
			UPDATE admin_users SET
				username = $2, password_hash = $3, display_name = $4,
				email = NULLIF($5,''), phone = NULLIF($6,''),
				is_active = $7, is_super_admin = $8, permissions = $9::jsonb,
				updated_at = now()
			WHERE admin_user_id = $1::uuid`,
			id, strings.TrimSpace(in.Username), hash, strings.TrimSpace(in.DisplayName),
			strings.TrimSpace(in.Email), strings.TrimSpace(in.Phone),
			in.IsActive, in.IsSuperAdmin, perms,
		)
		if err != nil {
			return err
		}
		if ct.RowsAffected() == 0 {
			return pgx.ErrNoRows
		}
		return nil
	}
	ct, err := pool.Exec(ctx, `
		UPDATE admin_users SET
			username = $2, display_name = $3,
			email = NULLIF($4,''), phone = NULLIF($5,''),
			is_active = $6, is_super_admin = $7, permissions = $8::jsonb,
			updated_at = now()
		WHERE admin_user_id = $1::uuid`,
		id, strings.TrimSpace(in.Username), strings.TrimSpace(in.DisplayName),
		strings.TrimSpace(in.Email), strings.TrimSpace(in.Phone),
		in.IsActive, in.IsSuperAdmin, perms,
	)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

// TouchAdminUserLastLogin sets last_login_at for the user.
func TouchAdminUserLastLogin(ctx context.Context, pool *pgxpool.Pool, adminUserID string) error {
	_, err := pool.Exec(ctx, `UPDATE admin_users SET last_login_at = now() WHERE admin_user_id = $1::uuid`, adminUserID)
	return err
}

// CountSuperAdmins returns active super admin count.
func CountSuperAdmins(ctx context.Context, pool *pgxpool.Pool) (int64, error) {
	return countOne(ctx, pool, `SELECT COUNT(*) FROM admin_users WHERE is_super_admin = true AND is_active = true`)
}

var ErrCannotDemoteLastSuperAdmin = errors.New("cannot demote or deactivate the last active super admin")

// ValidateSuperAdminDemotion ensures at least one active super admin remains.
func ValidateSuperAdminDemotion(ctx context.Context, pool *pgxpool.Pool, targetID string, willBeSuper, willBeActive bool) error {
	if willBeSuper && willBeActive {
		return nil
	}
	u, err := GetAdminUserByID(ctx, pool, targetID)
	if err != nil || u == nil {
		return err
	}
	if !u.IsSuperAdmin {
		return nil
	}
	n, err := CountSuperAdmins(ctx, pool)
	if err != nil {
		return err
	}
	if n <= 1 {
		return ErrCannotDemoteLastSuperAdmin
	}
	return nil
}

func hashPassword(plain string) (string, error) {
	plain = strings.TrimSpace(plain)
	if plain == "" {
		return "", fmt.Errorf("password is required")
	}
	b, err := bcrypt.GenerateFromPassword([]byte(plain), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// PermissionsFromInput parses form permission checkboxes for non-super admins.
func PermissionsFromInput(isSuper bool, keys []string) []string {
	if isSuper {
		return nil
	}
	return adminauth.ParsePermissionList(keys)
}
