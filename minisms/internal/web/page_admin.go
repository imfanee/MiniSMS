// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package web

import (
	"net/http"
	"reflect"
)

func applyAdminViewFields(dst any, v AdminView) {
	rv := reflect.ValueOf(dst)
	for rv.Kind() == reflect.Ptr {
		if rv.IsNil() {
			return
		}
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Struct {
		return
	}
	if f := rv.FieldByName("AdminView"); f.IsValid() && f.CanSet() {
		f.Set(reflect.ValueOf(v))
		return
	}
	setStr(rv, "AdminUsername", v.AdminUsername)
	setStr(rv, "AdminDisplayName", v.AdminDisplayName)
	setBool(rv, "IsSuperAdmin", v.IsSuperAdmin)
	if f := rv.FieldByName("Perms"); f.IsValid() && f.CanSet() && f.Kind() == reflect.Map {
		f.Set(reflect.ValueOf(v.Perms))
	}
}

// withAdminView returns page data with AdminView filled for navbar templates.
// Handlers often pass structs by value; mutating through a pointer does not update the copy used by ExecuteTemplate.
func withAdminView(v any, r *http.Request) any {
	if r == nil {
		return v
	}
	av := AdminViewFromUser(AdminFromContext(r.Context()))
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Ptr:
		if !rv.IsNil() {
			applyAdminViewFields(v, av)
		}
		return v
	case reflect.Struct:
		ptr := reflect.New(rv.Type())
		ptr.Elem().Set(rv)
		applyAdminViewFields(ptr.Interface(), av)
		return ptr.Elem().Interface()
	default:
		return v
	}
}

func setStr(rv reflect.Value, name, val string) {
	f := rv.FieldByName(name)
	if f.IsValid() && f.CanSet() && f.Kind() == reflect.String {
		f.SetString(val)
	}
}

func setBool(rv reflect.Value, name string, val bool) {
	f := rv.FieldByName(name)
	if f.IsValid() && f.CanSet() && f.Kind() == reflect.Bool {
		f.SetBool(val)
	}
}

// Can returns whether the current admin has a permission (for handlers).
func Can(r *http.Request, perm string) bool {
	u := AdminFromContext(r.Context())
	if u == nil {
		return false
	}
	if u.IsSuperAdmin {
		return true
	}
	for _, p := range u.Permissions {
		if p == perm {
			return true
		}
	}
	return false
}
