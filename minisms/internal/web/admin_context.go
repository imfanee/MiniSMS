// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package web

import (
	"context"
	"net/http"

	"github.com/minisms/minisms/internal/adminauth"
	"github.com/minisms/minisms/internal/models"
)

type adminUserKey struct{}

// WithAdminUser attaches the authenticated admin to the request context.
func WithAdminUser(ctx context.Context, u *models.AdminUser) context.Context {
	return context.WithValue(ctx, adminUserKey{}, u)
}

// AdminFromContext returns the admin user or nil.
func AdminFromContext(ctx context.Context) *models.AdminUser {
	v := ctx.Value(adminUserKey{})
	if v == nil {
		return nil
	}
	u, _ := v.(*models.AdminUser)
	return u
}

// AdminView is template-facing access control fields.
type AdminView struct {
	AdminUsername    string
	AdminDisplayName string
	IsSuperAdmin     bool
	Perms            map[string]bool
}

// AdminViewFromUser builds template access fields from a user model.
func AdminViewFromUser(u *models.AdminUser) AdminView {
	if u == nil {
		return AdminView{Perms: map[string]bool{}}
	}
	name := u.DisplayName
	if name == "" {
		name = u.Username
	}
	return AdminView{
		AdminUsername:    u.Username,
		AdminDisplayName: name,
		IsSuperAdmin:     u.IsSuperAdmin,
		Perms:            adminauth.PermMap(u.IsSuperAdmin, u.Permissions),
	}
}

// ApplyAdminView copies AdminView fields onto page structs that define them (via reflection).
// Prefer withAdminView when the value is passed by value to a template.
func ApplyAdminView(dst any, r *http.Request) {
	if r == nil {
		return
	}
	applyAdminViewFields(dst, AdminViewFromUser(AdminFromContext(r.Context())))
}
