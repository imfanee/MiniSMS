// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package carrier

import (
	"context"
	"testing"
)

func TestIsValidPhoneSenderID(t *testing.T) {
	if !IsValidPhoneSenderID("441234567890") {
		t.Fatal("expected numeric phone")
	}
	if !IsValidPhoneSenderID("+447700900123") {
		t.Fatal("expected e164 phone")
	}
	if IsValidPhoneSenderID("BrandX") {
		t.Fatal("expected invalid phone")
	}
}

func TestIsValidAnySenderIDDefaultPattern(t *testing.T) {
	if !IsValidAnySenderID(context.Background(), nil, "MiniSMS") {
		t.Fatal("expected alphanumeric any")
	}
	if !IsValidAnySenderID(context.Background(), nil, "My Brand") {
		t.Fatal("expected space in any mode")
	}
	if !IsValidAnySenderID(context.Background(), nil, "IZ_tech") {
		t.Fatal("expected underscore in any mode")
	}
	if IsValidAnySenderID(context.Background(), nil, "bad!") {
		t.Fatal("expected invalid any")
	}
}

func TestParseAllowedSenderIDsMode(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"list", AllowedSenderList},
		{"phone", AllowedSenderPhone},
		{"any", AllowedSenderAny},
	} {
		got, ok := ParseAllowedSenderIDsMode(tc.in)
		if !ok || got != tc.want {
			t.Fatalf("%q: got %q ok=%v", tc.in, got, ok)
		}
	}
	if _, ok := ParseAllowedSenderIDsMode("invalid"); ok {
		t.Fatal("expected invalid mode")
	}
}
