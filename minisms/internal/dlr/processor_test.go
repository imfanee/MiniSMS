// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package dlr

import (
	"testing"
	"time"
)

// TestForwardEndpointValidatorBlocksInternalWebhooks ensures the SSRF guard wired into the
// DLR forward path rejects client webhook URLs that resolve to loopback, private, link-local,
// or cloud-metadata targets, and accepts a public destination. Literal IPs keep this hermetic
// (the guard short-circuits DNS when the host is already an IP).
func TestForwardEndpointValidatorBlocksInternalWebhooks(t *testing.T) {
	now := time.Date(2026, 6, 1, 11, 0, 0, 0, time.UTC)

	blocked := []string{
		"http://127.0.0.1/dlr",
		"https://127.0.0.1:8443/dlr",
		"http://10.0.0.5/dlr",
		"http://192.168.1.10/dlr",
		"http://169.254.169.254/latest/meta-data",
		"http://localhost/dlr",
		"ftp://93.184.216.34/dlr",
	}
	for _, target := range blocked {
		row := testDLRRow()
		row.DLRWebhookURL = strPtr(target)
		fwd, err := BuildClientForward(row, "delivered", now)
		if err != nil {
			t.Fatalf("build forward for %q: %v", target, err)
		}
		if err := forwardEndpointValidator(fwd.URL); err == nil {
			t.Errorf("expected SSRF guard to block webhook %q (built url %q), but it was allowed", target, fwd.URL)
		}
	}

	// Public literal IP must be allowed so legitimate client webhooks still forward.
	allowed := testDLRRow()
	allowed.DLRWebhookURL = strPtr("https://93.184.216.34/dlr")
	fwd, err := BuildClientForward(allowed, "delivered", now)
	if err != nil {
		t.Fatal(err)
	}
	if err := forwardEndpointValidator(fwd.URL); err != nil {
		t.Errorf("expected public webhook to be allowed, got %v", err)
	}
}

// TestIsFinalStatus checks which normalized statuses are treated as terminal.
func TestIsFinalStatus(t *testing.T) {
	for _, s := range []string{"delivered", "undelivered", "rejected", "DELIVERED", " Rejected "} {
		if !IsFinalStatus(s) {
			t.Errorf("IsFinalStatus(%q) = false, want true", s)
		}
	}
	for _, s := range []string{"unknown", "", "queued", "accepted", "ack"} {
		if IsFinalStatus(s) {
			t.Errorf("IsFinalStatus(%q) = true, want false", s)
		}
	}
}

// TestShouldForwardDLR covers the multi-bit dlr-mask forward decision: the first receipt always
// forwards, later receipts forward only when they reach a final state.
func TestShouldForwardDLR(t *testing.T) {
	cases := []struct {
		hadPrior bool
		status   string
		want     bool
	}{
		{false, "unknown", true},     // first receipt (intermediate) still forwards
		{false, "delivered", true},   // first receipt (final) forwards
		{true, "unknown", false},     // trailing intermediate does not re-forward
		{true, "delivered", true},    // upgrade to final forwards
		{true, "undelivered", true},  // upgrade to final forwards
	}
	for _, c := range cases {
		if got := shouldForwardDLR(c.hadPrior, c.status); got != c.want {
			t.Errorf("shouldForwardDLR(%v, %q) = %v, want %v", c.hadPrior, c.status, got, c.want)
		}
	}
}

// TestForwardEndpointValidatorBlocksInternalWebhookGET covers the GET form, where the built
// URL carries an appended query string, to confirm the guard still inspects the host.
func TestForwardEndpointValidatorBlocksInternalWebhookGET(t *testing.T) {
	row := testDLRRow()
	row.DLRWebhookMethod = WebhookMethodGET
	row.DLRWebhookURL = strPtr("http://169.254.169.254/dlr")
	fwd, err := BuildClientForward(row, "delivered", time.Date(2026, 6, 1, 11, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if err := forwardEndpointValidator(fwd.URL); err == nil {
		t.Errorf("expected SSRF guard to block metadata webhook (built url %q), but it was allowed", fwd.URL)
	}
}
