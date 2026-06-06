// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package web

import (
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/csrf"
	"github.com/jackc/pgx/v5"

	"github.com/minisms/minisms/internal/adminauth"
	"github.com/minisms/minisms/internal/db"
)

type adminUsersListPage struct {
	AdminView
	Title, CurrentPath, CSRFToken string
	Flash                         *Flash
	Users                         []adminUserRowView
}

type adminUserRowView struct {
	ID           string
	Username     string
	DisplayName  string
	Email        string
	Phone        string
	IsActive     bool
	IsSuperAdmin bool
	PermCount    int
	LastLogin    string
}

type permGroupView struct {
	Group string
	Items []adminauth.PermissionDef
}

type adminUserFormPage struct {
	AdminView
	Title, CurrentPath, CSRFToken string
	Flash                         *Flash
	IsNew                         bool
	UserID                        string
	Username                      string
	DisplayName                   string
	Email                         string
	Phone                         string
	IsActive                      bool
	IsSuperAdmin                  bool
	SelectedPerms                 map[string]bool
	PermissionGroups              []permGroupView
	Errors                        map[string]string
}

func permissionGroups() []permGroupView {
	var groups []permGroupView
	var cur *permGroupView
	for _, d := range adminauth.AllAssignablePermissions {
		if cur == nil || cur.Group != d.Group {
			groups = append(groups, permGroupView{Group: d.Group})
			cur = &groups[len(groups)-1]
		}
		cur.Items = append(cur.Items, d)
	}
	return groups
}

func (h *Handlers) ListAdminUsers() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		users, err := db.ListAdminUsers(r.Context(), h.Pool)
		if err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		views := make([]adminUserRowView, 0, len(users))
		for _, u := range users {
			v := adminUserRowView{
				ID: u.AdminUserID, Username: u.Username, DisplayName: u.DisplayName,
				IsActive: u.IsActive, IsSuperAdmin: u.IsSuperAdmin,
				PermCount: len(u.Permissions),
			}
			if u.Email != nil {
				v.Email = *u.Email
			}
			if u.Phone != nil {
				v.Phone = *u.Phone
			}
			if u.LastLoginAt != nil {
				v.LastLogin = u.LastLoginAt.UTC().Format("2006-01-02 15:04")
			} else {
				v.LastLogin = "—"
			}
			if v.DisplayName == "" {
				v.DisplayName = u.Username
			}
			views = append(views, v)
		}
		p := adminUsersListPage{
			Title: "Admin users", CurrentPath: "/admin/admin-users", CSRFToken: csrf.Token(r),
			Flash: GetFlash(w, r, "/", h.Config.SecretKey, h.Config.IsProduction()),
			Users: views,
		}
		if err := execT(w, h.AdminUsersListT, "base", p, r); err != nil {
			ServerError(w, r, err, h.Log, h.T500)
		}
	}
}

func (h *Handlers) ShowNewAdminUser() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := adminUserFormPage{
			Title: "New admin user", CurrentPath: "/admin/admin-users", CSRFToken: csrf.Token(r),
			IsNew: true, IsActive: true,
			PermissionGroups: permissionGroups(),
			SelectedPerms:    map[string]bool{},
			Errors:           map[string]string{},
		}
		if err := execT(w, h.AdminUsersFormT, "base", p, r); err != nil {
			ServerError(w, r, err, h.Log, h.T500)
		}
	}
}

func (h *Handlers) CreateAdminUser() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		in, errs := parseAdminUserForm(r, true)
		if len(errs) > 0 {
			renderAdminUserForm(w, r, h, adminUserFormPage{
				Title: "New admin user", CurrentPath: "/admin/admin-users", CSRFToken: csrf.Token(r),
				IsNew: true, Username: in.Username, DisplayName: in.DisplayName,
				Email: in.Email, Phone: in.Phone, IsActive: in.IsActive, IsSuperAdmin: in.IsSuperAdmin,
				SelectedPerms: permSet(in.Permissions), PermissionGroups: permissionGroups(),
				Errors: errs,
			})
			return
		}
		newID, err := db.CreateAdminUser(r.Context(), h.Pool, in)
		if err != nil {
			if strings.Contains(err.Error(), "duplicate") || strings.Contains(err.Error(), "unique") {
				renderAdminUserForm(w, r, h, adminUserFormPage{
					Title: "New admin user", CurrentPath: "/admin/admin-users", CSRFToken: csrf.Token(r),
					IsNew: true, Username: in.Username, DisplayName: in.DisplayName,
					Email: in.Email, Phone: in.Phone, IsActive: in.IsActive, IsSuperAdmin: in.IsSuperAdmin,
					SelectedPerms: permSet(in.Permissions), PermissionGroups: permissionGroups(),
					Errors: map[string]string{"username": "Username already exists"},
				})
				return
			}
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		uname := in.Username
		h.recordAudit(r, "admin_user.create", "admin_user", &newID, &uname, map[string]any{
			"username": in.Username, "is_super_admin": in.IsSuperAdmin, "is_active": in.IsActive,
		})
		SetFlash(w, r, "/", h.Config.SecretKey, h.Config.IsProduction(), &Flash{Type: "success", Message: "Admin user created"})
		http.Redirect(w, r, "/admin/admin-users", http.StatusSeeOther)
	}
}

func (h *Handlers) ShowEditAdminUser() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		u, err := db.GetAdminUserByID(r.Context(), h.Pool, id)
		if err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		if u == nil {
			http.NotFound(w, r)
			return
		}
		p := adminUserFormPage{
			Title: "Edit admin user", CurrentPath: "/admin/admin-users", CSRFToken: csrf.Token(r),
			UserID: id, Username: u.Username, DisplayName: u.DisplayName,
			IsActive: u.IsActive, IsSuperAdmin: u.IsSuperAdmin,
			PermissionGroups: permissionGroups(),
			SelectedPerms:    permSet(u.Permissions),
			Errors:         map[string]string{},
		}
		if u.Email != nil {
			p.Email = *u.Email
		}
		if u.Phone != nil {
			p.Phone = *u.Phone
		}
		if err := execT(w, h.AdminUsersFormT, "base", p, r); err != nil {
			ServerError(w, r, err, h.Log, h.T500)
		}
	}
}

func (h *Handlers) UpdateAdminUser() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		if err := r.ParseForm(); err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		in, errs := parseAdminUserForm(r, false)
		if len(errs) > 0 {
			renderAdminUserForm(w, r, h, adminUserFormPage{
				Title: "Edit admin user", CurrentPath: "/admin/admin-users", CSRFToken: csrf.Token(r),
				UserID: id, Username: in.Username, DisplayName: in.DisplayName,
				Email: in.Email, Phone: in.Phone, IsActive: in.IsActive, IsSuperAdmin: in.IsSuperAdmin,
				SelectedPerms: permSet(in.Permissions), PermissionGroups: permissionGroups(),
				Errors: errs,
			})
			return
		}
		if err := db.ValidateSuperAdminDemotion(r.Context(), h.Pool, id, in.IsSuperAdmin, in.IsActive); err != nil {
			if errors.Is(err, db.ErrCannotDemoteLastSuperAdmin) {
				renderAdminUserForm(w, r, h, adminUserFormPage{
					Title: "Edit admin user", CurrentPath: "/admin/admin-users", CSRFToken: csrf.Token(r),
					UserID: id, Username: in.Username, DisplayName: in.DisplayName,
					Email: in.Email, Phone: in.Phone, IsActive: in.IsActive, IsSuperAdmin: in.IsSuperAdmin,
					SelectedPerms: permSet(in.Permissions), PermissionGroups: permissionGroups(),
					Errors: map[string]string{"_form": "Cannot remove the last active super admin"},
				})
				return
			}
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		if err := db.UpdateAdminUser(r.Context(), h.Pool, id, in); err != nil {
			if err == pgx.ErrNoRows {
				http.NotFound(w, r)
				return
			}
			if strings.Contains(err.Error(), "duplicate") || strings.Contains(err.Error(), "unique") {
				renderAdminUserForm(w, r, h, adminUserFormPage{
					Title: "Edit admin user", CurrentPath: "/admin/admin-users", CSRFToken: csrf.Token(r),
					UserID: id, Username: in.Username, DisplayName: in.DisplayName,
					Email: in.Email, Phone: in.Phone, IsActive: in.IsActive, IsSuperAdmin: in.IsSuperAdmin,
					SelectedPerms: permSet(in.Permissions), PermissionGroups: permissionGroups(),
					Errors: map[string]string{"username": "Username already exists"},
				})
				return
			}
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		uname := in.Username
		h.recordAudit(r, "admin_user.update", "admin_user", &id, &uname, map[string]any{
			"username": in.Username, "is_super_admin": in.IsSuperAdmin, "is_active": in.IsActive,
		})
		SetFlash(w, r, "/", h.Config.SecretKey, h.Config.IsProduction(), &Flash{Type: "success", Message: "Admin user updated"})
		http.Redirect(w, r, "/admin/admin-users", http.StatusSeeOther)
	}
}

func renderAdminUserForm(w http.ResponseWriter, r *http.Request, h *Handlers, p adminUserFormPage) {
	w.WriteHeader(http.StatusUnprocessableEntity)
	_ = execT(w, h.AdminUsersFormT, "base", p, r)
}

func parseAdminUserForm(r *http.Request, requirePassword bool) (db.AdminUserInput, map[string]string) {
	errs := map[string]string{}
	in := db.AdminUserInput{
		Username:     strings.TrimSpace(r.FormValue("username")),
		DisplayName:  strings.TrimSpace(r.FormValue("display_name")),
		Email:        strings.TrimSpace(r.FormValue("email")),
		Phone:        strings.TrimSpace(r.FormValue("phone")),
		IsActive:     r.FormValue("is_active") == "on" || r.FormValue("is_active") == "1",
		IsSuperAdmin: r.FormValue("is_super_admin") == "on" || r.FormValue("is_super_admin") == "1",
		Password:     r.FormValue("password"),
	}
	if in.Username == "" {
		errs["username"] = "Username is required"
	}
	if in.DisplayName == "" {
		in.DisplayName = in.Username
	}
	if requirePassword && strings.TrimSpace(in.Password) == "" {
		errs["password"] = "Password is required"
	}
	in.Permissions = db.PermissionsFromInput(in.IsSuperAdmin, r.Form["permissions"])
	return in, errs
}

func permSet(perms []string) map[string]bool {
	m := make(map[string]bool)
	for _, p := range perms {
		m[p] = true
	}
	return m
}
