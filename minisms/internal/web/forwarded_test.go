// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package web

import "testing"

func TestCsrfTrustedHosts(t *testing.T) {
	got := csrfTrustedHosts([]string{"https://staging.example.com:18080"}, "staging.example.com:18080")
	if len(got) != 1 || got[0] != "staging.example.com:18080" {
		t.Fatalf("got %v", got)
	}
}
