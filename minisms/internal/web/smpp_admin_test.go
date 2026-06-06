// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package web

import "testing"

func TestValidateSMPPCIDRs(t *testing.T) {
	if err := validateSMPPCIDRs(""); err != nil {
		t.Fatal(err)
	}
	if err := validateSMPPCIDRs("203.0.113.10/32, 198.51.100.0/24"); err != nil {
		t.Fatal(err)
	}
	if err := validateSMPPCIDRs("10.0.0.1"); err != nil {
		t.Fatal(err)
	}
	if validateSMPPCIDRs("not-a-cidr") == nil {
		t.Fatal("expected error")
	}
}

func TestValidateInterconnectType(t *testing.T) {
	for _, v := range []string{"http", "smpp", "HTTP", "SMPP"} {
		if _, ok := validateInterconnectType(v); !ok {
			t.Fatalf("want ok for %q", v)
		}
	}
	if _, ok := validateInterconnectType("both"); ok {
		t.Fatal("both is no longer valid")
	}
	if _, ok := validateInterconnectType("ftp"); ok {
		t.Fatal("expected invalid")
	}
}
