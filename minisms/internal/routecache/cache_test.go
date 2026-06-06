// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package routecache

import "testing"

func TestLongestPrefix(t *testing.T) {
	entries := []RouteEntry{
		{Prefix: "*", PrimaryCarrierID: "catch"},
		{Prefix: "44", PrimaryCarrierID: "uk"},
		{Prefix: "447", PrimaryCarrierID: "uk7"},
	}
	got := longestPrefix(entries, "+447911123456")
	if got == nil || got.PrimaryCarrierID != "uk7" {
		t.Fatalf("got %+v", got)
	}
}
