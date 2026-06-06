// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package carrier

import "testing"

func TestValidateEndpointURLBlocksPrivate(t *testing.T) {
	for _, u := range []string{
		"http://127.0.0.1/send",
		"http://localhost/send",
		"http://10.0.0.1/send",
		"http://192.168.1.1/send",
		"http://169.254.169.254/latest/meta-data/",
	} {
		if err := ValidateEndpointURL(u); err == nil {
			t.Fatalf("expected block for %s", u)
		}
	}
}

func TestValidateEndpointURLAllowsPublicHTTPS(t *testing.T) {
	// TEST-NET-3 (RFC 5737) — public, non-routable documentation range
	if err := ValidateEndpointURL("https://203.0.113.50/sms"); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
}
