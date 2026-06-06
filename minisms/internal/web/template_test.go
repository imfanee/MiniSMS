// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package web

import (
	"html/template"
	"strings"
	"testing"

	"github.com/minisms/minisms"
)

func TestTemplateFormPreservesJSONInTextarea(t *testing.T) {
	tm, err := template.ParseFS(minisms.TemplateFS, "templates/admin/carriers/template_form.html")
	if err != nil {
		t.Fatal(err)
	}
	body := "{\n  \"to\": \"{{to}}\",\n  \"from\": \"{{from}}\"\n}"
	var out strings.Builder
	err = tm.ExecuteTemplate(&out, "template_form", TemplatePanelData{
		CarrierID: "x", CSRFToken: "t", ContentType: "application/json",
		Body: templateTextareaHTML(body), Query: templateTextareaHTML(""),
		BodyB64: templateBodyB64(body), QueryB64: "",
		HTTPMethod: "POST", Errors: map[string]string{},
	})
	if err != nil {
		t.Fatal(err)
	}
	s := out.String()
	if !strings.Contains(s, `data-body-b64="`) || !strings.Contains(s, `"to": "{{to}}"`) {
		t.Fatalf("expected body b64 and literal JSON in pre, got fragment: %s", s[:min(800, len(s))])
	}
	if strings.Contains(s, "&#34;to&#34;") {
		t.Fatalf("body-data must not HTML-escape quotes")
	}
	if strings.Contains(s, "<script") && strings.Contains(s, `id="body-data"`) {
		t.Fatal("body-data must use pre not script (scripts are stripped on HTMX innerHTML)")
	}
}

func TestCurrenciesListTemplate(t *testing.T) {
	tm, err := template.New("base.html").Funcs(TemplateFuncs()).ParseFS(minisms.TemplateFS,
		"templates/layout/base.html",
		"templates/layout/partials/navbar.html",
		"templates/layout/partials/flash.html",
		"templates/admin/currencies/list.html",
	)
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	err = tm.ExecuteTemplate(&b, "base", currenciesPage{
		CurrentPath: "/admin/currencies",
		CSRFToken:   "t",
		AdminView: AdminView{
			IsSuperAdmin: true,
			Perms:        map[string]bool{},
		},
		Form: currencyForm{DecimalPlaces: "2", Errors: map[string]string{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), "Currencies") {
		t.Fatal("expected currencies content")
	}
}

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
		}, DashboardPage{
			CurrentPath: "/admin/dashboard",
			AdminView: AdminView{
				IsSuperAdmin: true,
				Perms:        map[string]bool{},
			},
		}},
	} {
		tm, err := template.New("base.html").Funcs(TemplateFuncs()).ParseFS(minisms.TemplateFS, p.names...)
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
