// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package dlr

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestInboundFromHTTPStoresQueryAndBody(t *testing.T) {
	req := httptest.NewRequest("POST", "/api/v1/dlr/00000000-0000-4000-8000-000000000001?status=DELIVRD&secret=topsecret", strings.NewReader(`{"status":"DELIVRD","note":"ok"}`))
	req.Header.Set("Content-Type", "application/json")
	query := map[string]string{"status": "DELIVRD", "secret": "topsecret"}
	body := []byte(`{"status":"DELIVRD","note":"ok"}`)

	in := InboundFromHTTP(req, query, body)
	meta := in.TimelineMeta("delivered")

	if meta["mapped_status"] != "delivered" {
		t.Fatalf("mapped_status: %v", meta["mapped_status"])
	}
	qp := meta["query_params"].(map[string]string)
	if qp["status"] != "DELIVRD" {
		t.Fatalf("query status: %q", qp["status"])
	}
	if qp["secret"] != "***" {
		t.Fatalf("expected redacted secret, got %q", qp["secret"])
	}
	if meta["request_body"] != string(body) {
		t.Fatalf("body mismatch: %q", meta["request_body"])
	}
	if meta["http_method"] != "POST" {
		t.Fatalf("method: %v", meta["http_method"])
	}
}

func TestInboundFromSMPP(t *testing.T) {
	meta := InboundFromSMPP("msg-ref-1", "DELIVRD").TimelineMeta("delivered")
	if meta["channel"] != "smpp" {
		t.Fatalf("channel: %v", meta["channel"])
	}
	if meta["smpp_receipt_ref"] != "msg-ref-1" {
		t.Fatalf("ref: %v", meta["smpp_receipt_ref"])
	}
}

func TestTimelineMetaNilInbound(t *testing.T) {
	meta := (*InboundCallback)(nil).TimelineMeta("unknown")
	if meta["mapped_status"] != "unknown" {
		t.Fatalf("mapped_status: %v", meta["mapped_status"])
	}
}
