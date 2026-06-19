// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package web

import (
	"net/url"
	"strings"
	"testing"
)

func TestValidJSONAfterTemplateSubst(t *testing.T) {
	if !validJSONAfterTemplateSubst(`{"to":"{{to}}","from":"{{from}}"}`) {
		t.Fatal("quoted vars should be valid after substitution")
	}
	if validJSONAfterTemplateSubst("{\n  \"to\":{{to}},\n  \"from\":{{from}}\n}") {
		t.Fatal("unquoted vars must not validate as JSON")
	}
}

func TestValidateRequestTemplateJSONBody(t *testing.T) {
	em := validateRequestTemplate("POST", "application/json", "{\n  \"to\":{{to}},\n  \"from\":{{from}}\n}", "")
	if em["body_template"] == "" {
		t.Fatalf("expected body_template error, got %v", em)
	}
	if !strings.Contains(em["body_template"], "JSON body:") {
		t.Fatalf("expected JSON body prefix, got %q", em["body_template"])
	}
	em = validateRequestTemplate("POST", "application/json", `{"to":"{{to}}","from":"{{from}}"}`, "")
	if len(em) != 0 {
		t.Fatalf("expected no errors, got %v", em)
	}
}

func TestValidateRequestTemplateFormBody(t *testing.T) {
	em := validateRequestTemplate("POST", "application/x-www-form-urlencoded", "to={{to}}&badsegment", "")
	if em["body_template"] == "" {
		t.Fatalf("expected form error, got %v", em)
	}
	em = validateRequestTemplate("POST", "application/x-www-form-urlencoded", "to={{to}}&text={{message}}", "")
	if len(em) != 0 {
		t.Fatalf("expected no errors, got %v", em)
	}
}

func TestValidateRequestTemplateXMLBody(t *testing.T) {
	em := validateRequestTemplate("POST", "text/xml", "<open><to>{{to}}</to>", "")
	if em["body_template"] == "" {
		t.Fatalf("expected XML error, got %v", em)
	}
	em = validateRequestTemplate("POST", "text/xml", "<submit><to>{{to}}</to><text>{{message}}</text></submit>", "")
	if len(em) != 0 {
		t.Fatalf("expected no errors, got %v", em)
	}
}

func TestValidateRequestTemplateGETQueryWithJSONContentType(t *testing.T) {
	query := "to={{to}}&from={{from}}"
	body := `{"to":"{{to}}","from":"{{from}}"}`
	em := validateRequestTemplate("GET", "application/json", body, query)
	if len(em) != 0 {
		t.Fatalf("GET query key=value should validate with JSON body content-type, got %v", em)
	}
	em = validateRequestTemplate("GET", "application/json", body, "badsegment")
	if em["query_template"] == "" {
		t.Fatalf("expected query error, got %v", em)
	}
}

func TestResolvePOSTQueryTemplateForSave(t *testing.T) {
	got := resolvePOSTQueryTemplateForSave("POST", "", "to={{to}}")
	if got != "to={{to}}" {
		t.Fatalf("empty submit should keep stored query, got %q", got)
	}
	got = resolvePOSTQueryTemplateForSave("POST", "a=1", "old")
	if got != "a=1" {
		t.Fatalf("non-empty submit should be kept, got %q", got)
	}
	got = resolvePOSTQueryTemplateForSave("GET", "", "stored")
	if got != "" {
		t.Fatalf("GET empty submit should stay empty, got %q", got)
	}
}

func TestResolveGETBodyTemplateForSave(t *testing.T) {
	got := resolveGETBodyTemplateForSave("GET", `{"to":"{{to}}"}`, "old")
	if got != `{"to":"{{to}}"}` {
		t.Fatalf("submitted body should be kept, got %q", got)
	}
	got = resolveGETBodyTemplateForSave("GET", "", "stored")
	if got != "stored" {
		t.Fatalf("empty submit should keep stored body, got %q", got)
	}
	got = resolveGETBodyTemplateForSave("POST", "", "stored")
	if got != "" {
		t.Fatalf("POST empty submit should stay empty, got %q", got)
	}
}

func TestApplyCarrierSenderFields(t *testing.T) {
	em := map[string]string{}
	policy, def := applyCarrierSenderFields(url.Values{
		"sender_id_policy":        {"list"},
		"default_sender_id_value": {"My Brand"},
	}, em)
	if len(em) != 0 {
		t.Fatalf("unexpected errors: %v", em)
	}
	if policy != "list" {
		t.Fatalf("policy: got %q", policy)
	}
	if def == nil || *def != "My Brand" {
		t.Fatalf("default: got %v", def)
	}

	em = map[string]string{}
	_, _ = applyCarrierSenderFields(url.Values{"sender_id_policy": {"bad"}}, em)
	if em["sender_id_policy"] == "" {
		t.Fatal("expected invalid policy error")
	}
}
