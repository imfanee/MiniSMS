// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package adminauth

import "testing"

func TestHasPermissionSuperAdmin(t *testing.T) {
	if !HasPermission(true, nil, PermCarriersView) {
		t.Fatal("super admin should have all permissions")
	}
}

func TestParsePermissionList(t *testing.T) {
	got := ParsePermissionList([]string{PermCarriersView, "invalid", PermCarriersView})
	if len(got) != 1 || got[0] != PermCarriersView {
		t.Fatalf("got %v", got)
	}
}
