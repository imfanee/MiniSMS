// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package web

import (
	"context"
	"net/http"
	"testing"

	"github.com/minisms/minisms/internal/models"
)

func TestWithAdminViewStructByValue(t *testing.T) {
	u := &models.AdminUser{
		Username: "ops", DisplayName: "Ops", IsSuperAdmin: true,
	}
	r, _ := http.NewRequest(http.MethodGet, "/", nil)
	r = r.WithContext(WithAdminUser(context.Background(), u))

	out := withAdminView(CarrierListPage{Title: "Carriers"}, r).(CarrierListPage)
	if !out.IsSuperAdmin {
		t.Fatal("expected super admin flag on value struct")
	}
	if out.AdminUsername != "ops" {
		t.Fatalf("username=%q", out.AdminUsername)
	}
}

func TestWithAdminViewPointer(t *testing.T) {
	u := &models.AdminUser{Username: "ops", IsSuperAdmin: true}
	r, _ := http.NewRequest(http.MethodGet, "/", nil)
	r = r.WithContext(WithAdminUser(context.Background(), u))

	p := &CarrierListPage{Title: "Carriers"}
	out := withAdminView(p, r).(*CarrierListPage)
	if !out.IsSuperAdmin {
		t.Fatal("expected super admin on pointer")
	}
}
