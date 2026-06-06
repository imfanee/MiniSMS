// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package web

import "testing"

func TestCsrfTrustedHosts(t *testing.T) {
	got := csrfTrustedHosts([]string{"https://sms.telecotech.net:18080"}, "sms.telecotech.net:18080")
	if len(got) != 1 || got[0] != "sms.telecotech.net:18080" {
		t.Fatalf("got %v", got)
	}
}
