// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package web

import (
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestParsePaymentReferenceOther(t *testing.T) {
	form := url.Values{}
	form.Set("reference_type", "other")
	form.Set("payment_reference", "WIRE-123")
	r := httptest.NewRequest("POST", "/", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	_ = r.ParseForm()
	errs := map[string]string{}
	parsed := parsePaymentReference(r, errs)
	if len(errs) > 0 {
		t.Fatalf("errs: %v", errs)
	}
	if parsed.RefType != "other" || parsed.PaymentRef == nil || *parsed.PaymentRef != "WIRE-123" {
		t.Fatalf("parsed: %+v", parsed)
	}
}

func TestParsePaymentReferenceInvoiceRequiresSelection(t *testing.T) {
	form := url.Values{}
	form.Set("reference_type", "invoice")
	r := httptest.NewRequest("POST", "/", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	_ = r.ParseForm()
	errs := map[string]string{}
	parsePaymentReference(r, errs)
	if errs["invoice_id"] == "" {
		t.Fatal("expected invoice_id error")
	}
}
