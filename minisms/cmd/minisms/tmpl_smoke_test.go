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
	// The fragment set (table + detail modal + print view) is parsed separately at runtime; guard it too
	// so the detail-modal Print button and the print view cannot break startup.
	if _, err := parseTemplateFS(
		minisms.TemplateFS,
		"templates/admin/sms_logs/table.html",
		"templates/admin/sms_logs/detail_modal.html",
		"templates/admin/sms_logs/resend_result.html",
		"templates/admin/sms_logs/print.html",
	); err != nil {
		t.Fatalf("parse sms log fragment templates: %v", err)
	}
}
