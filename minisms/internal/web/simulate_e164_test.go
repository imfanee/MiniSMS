// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package web

import "testing"

// The simulate destination accepts an OPTIONAL leading + (x-www-form-urlencoded turns a typed '+' into
// a space that gets trimmed, so requiring '+' wrongly rejected valid numbers).
func TestSimulateE164OptionalPlus(t *testing.T) {
	ok := []string{"+447700900123", "447700900123", "+15551234567", "15551234567"}
	bad := []string{"", "+0447700900", "07700900123", "abc", "+44 7700", "+123"}
	for _, s := range ok {
		if !simulateE164Re.MatchString(s) {
			t.Errorf("expected %q to be accepted", s)
		}
	}
	for _, s := range bad {
		if simulateE164Re.MatchString(s) {
			t.Errorf("expected %q to be rejected", s)
		}
	}
}
