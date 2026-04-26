package web

import (
	"html/template"
	"strings"
	"testing"

	"github.com/minisms/minisms"
)

func TestLoginAndDashboardTemplates(t *testing.T) {
	for _, p := range []struct {
		names []string
		data  any
	}{
		{[]string{
			"templates/layout/base.html",
			"templates/layout/partials/navbar.html",
			"templates/layout/partials/flash.html",
			"templates/admin/login.html",
		}, Page{CurrentPath: "/admin/login", CSRFToken: "t"}},
		{[]string{
			"templates/layout/base.html",
			"templates/layout/partials/navbar.html",
			"templates/layout/partials/flash.html",
			"templates/admin/dashboard.html",
			"templates/admin/dashboard_stats.html",
		}, DashboardPage{CurrentPath: "/admin/dashboard"}},
	} {
		tm, err := template.ParseFS(minisms.TemplateFS, p.names...)
		if err != nil {
			t.Fatal(err)
		}
		var b strings.Builder
		if err := tm.ExecuteTemplate(&b, "base", p.data); err != nil {
			t.Fatalf("%s: %v", p.names[len(p.names)-1], err)
		}
		if b.Len() < 100 {
			t.Fatal("output too small")
		}
	}
}
