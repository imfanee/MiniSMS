// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package dlr

import "testing"

// Gateway/Kannel bitmask status codes (doc/dlr.md).
var kamexStatusMap = map[string]string{
	"1":  "delivered",
	"2":  "undelivered",
	"4":  "queued",
	"8":  "submitted",
	"16": "undelivered",
}

func TestNormalizeFromFields_GatewayNumericStatus(t *testing.T) {
	field := "status"
	got := NormalizeFromFields(map[string]string{"status": "1"}, &field, kamexStatusMap)
	if got != "delivered" {
		t.Fatalf("got %q want delivered", got)
	}
	got = NormalizeFromFields(map[string]string{"status": "16"}, &field, kamexStatusMap)
	if got != "undelivered" {
		t.Fatalf("got %q want undelivered", got)
	}
}

func TestNormalizeFromFields_GatewayAnswerFallback(t *testing.T) {
	field := "status"
	got := NormalizeFromFields(map[string]string{"answer": "DELIVRD"}, &field, kamexStatusMap)
	if got != "delivered" {
		t.Fatalf("got %q want delivered", got)
	}
}

func TestNormalizeFromFields_EmptyQueryLikeGatewayBareURL(t *testing.T) {
	field := "status"
	got := NormalizeFromFields(map[string]string{}, &field, kamexStatusMap)
	if got != "unknown" {
		t.Fatalf("got %q want unknown", got)
	}
}
