// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package main

import (
	"testing"

	"github.com/minisms/minisms"
)

// TestSMSLogTemplatesParse guards the SMS Logs template set (list + table, with base/navbar/flash)
// because those templates are parsed only at runtime; a syntax slip would otherwise surface as a
// startup crash on the host rather than a failed build.
func TestSMSLogTemplatesParse(t *testing.T) {
	if _, err := parseTemplateFS(
		minisms.TemplateFS,
		"templates/layout/base.html",
		"templates/layout/partials/navbar.html",
		"templates/layout/partials/flash.html",
		"templates/admin/sms_logs/list.html",
		"templates/admin/sms_logs/table.html",
	); err != nil {
		t.Fatalf("parse sms log templates: %v", err)
	}
}
