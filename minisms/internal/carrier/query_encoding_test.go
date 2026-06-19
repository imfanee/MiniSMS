// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package carrier

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// TestInjectQueryVariablesEscapesReservedChars asserts that a literal "+" in from/to becomes
// "%2B" (never a literal "+" that the gateway would decode to a space) and that reserved
// characters (+, &, =, /, space) in any value round-trip when the gateway parses the query.
func TestInjectQueryVariablesEscapesReservedChars(t *testing.T) {
	vars := map[string]string{
		"from":                     "+14155550101",
		"to":                       "+14155550102",
		"message":                  "1+1=2 & a/b done",
		"dlr_callback_url_encoded": "https%3A%2F%2Fsw%2Fdlr%3Fx%3D%25d",
	}
	out := InjectQueryVariables(
		"username=switch&from={{from}}&to={{to}}&text={{message}}&dlr-mask=31&dlr-url={{dlr_callback_url_encoded}}",
		vars,
	)

	if !strings.Contains(out, "from=%2B14155550101") {
		t.Fatalf("from must be percent-encoded: %s", out)
	}
	if strings.Contains(out, "from=+17") || strings.Contains(out, "from= 17") {
		t.Fatalf("from must not contain a literal + or space: %s", out)
	}
	if !strings.Contains(out, "dlr-mask=31") {
		t.Fatalf("dlr-mask must be preserved verbatim: %s", out)
	}
	// Pre-encoded callback must pass through unchanged (no double-encoding of its %2F etc.).
	if !strings.Contains(out, "dlr-url=https%3A%2F%2Fsw%2Fdlr%3Fx%3D%25d") {
		t.Fatalf("dlr_callback_url_encoded was altered/double-encoded: %s", out)
	}

	q, err := url.ParseQuery(out)
	if err != nil {
		t.Fatalf("encoded query does not parse: %v", err)
	}
	if q.Get("from") != "+14155550101" {
		t.Fatalf("from round-trip = %q, want +14155550101", q.Get("from"))
	}
	if q.Get("to") != "+14155550102" {
		t.Fatalf("to round-trip = %q, want +14155550102", q.Get("to"))
	}
	if q.Get("text") != "1+1=2 & a/b done" {
		t.Fatalf("text round-trip = %q (reserved chars corrupted)", q.Get("text"))
	}
}

// TestDispatchToCarrierDeliversPlusAsEncoded is the end-to-end acceptance test for Bug #1:
// the gateway must receive from decoded as "+14155550101", not " 14155550101".
func TestDispatchToCarrierDeliversPlusAsEncoded(t *testing.T) {
	var rawQuery, gotFrom, gotTo, gotText, gotMask string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rawQuery = r.URL.RawQuery
		gotFrom = r.URL.Query().Get("from")
		gotTo = r.URL.Query().Get("to")
		gotText = r.URL.Query().Get("text")
		gotMask = r.URL.Query().Get("dlr-mask")
		_, _ = io.WriteString(w, "0: Accepted for delivery")
	}))
	defer srv.Close()

	SetDispatchEndpointValidatorForTest(func(string) error { return nil })
	defer ResetDispatchEndpointValidator()

	vars := map[string]string{
		"from":    "+14155550101",
		"to":      "+14155550102",
		"message": "Hello from switch & co",
	}
	query := InjectQueryVariables("username=switch&from={{from}}&to={{to}}&text={{message}}&dlr-mask=31", vars)
	res, err := DispatchToCarrier(DispatchRequest{
		Method:      http.MethodGet,
		EndpointURL: srv.URL,
		Query:       query,
		Timeout:     5 * time.Second,
	})
	if err != nil {
		t.Fatalf("dispatch error: %v", err)
	}
	if res.StatusCode != http.StatusOK || res.Body != "0: Accepted for delivery" {
		t.Fatalf("unexpected gateway response: %d %q", res.StatusCode, res.Body)
	}
	if !strings.Contains(rawQuery, "from=%2B14155550101") {
		t.Fatalf("wire query must encode + as %%2B, got: %s", rawQuery)
	}
	if gotFrom != "+14155550101" {
		t.Fatalf("gateway decoded from = %q, want +14155550101 (a leading space means the bug is present)", gotFrom)
	}
	if gotTo != "+14155550102" {
		t.Fatalf("gateway decoded to = %q, want +14155550102", gotTo)
	}
	if gotText != "Hello from switch & co" {
		t.Fatalf("message corrupted in transit: %q", gotText)
	}
	if gotMask != "31" {
		t.Fatalf("dlr-mask not present on the wire: %q", gotMask)
	}
}

// TestResolveTONNPISenderClassification covers Bug #2: a numeric sender must be tagged with a
// numeric TON (1/2), never alphanumeric (5), while a true alphanumeric sender keeps the SMSC
// default (5). cfg is empty so detection is dynamic.
func TestResolveTONNPISenderClassification(t *testing.T) {
	cfg := SMPPConfig{}
	dest := "+14155550102"

	if p := ResolveTONNPI(cfg, "+14155550101", dest); p.SourceAddrTON != 1 || p.SourceAddrNPI != 1 {
		t.Fatalf("numeric +MSISDN: got ton=%d npi=%d, want ton=1 npi=1", p.SourceAddrTON, p.SourceAddrNPI)
	}
	if p := ResolveTONNPI(cfg, "14155550101", dest); p.SourceAddrTON == 5 {
		t.Fatalf("bare-digit sender must not be classified alphanumeric (got ton=5)")
	}
	for _, alpha := range []string{"ACME", "Zaz.Bet", "My Brand"} {
		if p := ResolveTONNPI(cfg, alpha, dest); p.SourceAddrTON != 5 {
			t.Fatalf("alphanumeric sender %q: got ton=%d, want 5 (SMSC default)", alpha, p.SourceAddrTON)
		}
	}
}
