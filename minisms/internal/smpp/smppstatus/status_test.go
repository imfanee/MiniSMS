// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package smppstatus

import (
	"strings"
	"testing"
)

func TestDescribe_KnownAndUnknown(t *testing.T) {
	name, desc := Describe(0x0D)
	if name != "ESME_RBINDFAIL" {
		t.Fatalf("0x0D name=%q want ESME_RBINDFAIL", name)
	}
	// The RBINDFAIL description must make the firewall-vs-bind distinction clear.
	if !strings.Contains(strings.ToLower(desc), "not a network") {
		t.Fatalf("RBINDFAIL desc should distinguish from a firewall block: %q", desc)
	}
	if n, _ := Describe(0x58); n != "ESME_RTHROTTLED" {
		t.Fatalf("0x58 name=%q want ESME_RTHROTTLED", n)
	}
	// Unknown code still yields a usable label, no panic.
	n, d := Describe(0x12345678)
	if n == "" || d == "" {
		t.Fatal("unknown code must still return a label and description")
	}
	if !strings.Contains(Format(0x0D), "ESME_RBINDFAIL") {
		t.Fatalf("Format missing mnemonic: %s", Format(0x0D))
	}
}
