// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package dlr

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/minisms/minisms/internal/db"
)

func testDLRRow() *db.DLRMessage {
	carrier := "TestCarrier"
	from := "SENDER"
	ref := "cref-1"
	return &db.DLRMessage{
		MessageID:        "00000000-0000-4000-8000-000000000001",
		ClientRef:        &ref,
		ToNumber:         "+447700900123",
		FromNumber:       &from,
		Segments:         2,
		TotalCharged:     "0.020000",
		CarrierName:      &carrier,
		FailoverSequence: 1,
		ReceivedAt:       time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC),
		DLRWebhookURL:    strPtr("https://client.example.com/dlr"),
		DLRWebhookMethod: WebhookMethodPOST,
	}
}

func strPtr(s string) *string { return &s }

func TestBuildClientForwardDefaultPOST(t *testing.T) {
	row := testDLRRow()
	now := time.Date(2026, 6, 1, 11, 0, 0, 0, time.UTC)
	req, err := BuildClientForward(row, "delivered", now)
	if err != nil {
		t.Fatal(err)
	}
	if req.Method != "POST" || req.ContentType != "application/json" {
		t.Fatalf("method/ct: %s %s", req.Method, req.ContentType)
	}
	var m map[string]any
	if err := json.Unmarshal(req.Body, &m); err != nil {
		t.Fatal(err)
	}
	if m["dlr_status"] != "delivered" || m["message_id"] != row.MessageID {
		t.Fatalf("payload: %v", m)
	}
}

func TestBuildClientForwardGET(t *testing.T) {
	row := testDLRRow()
	row.DLRWebhookMethod = WebhookMethodGET
	now := time.Date(2026, 6, 1, 11, 0, 0, 0, time.UTC)
	req, err := BuildClientForward(row, "delivered", now)
	if err != nil {
		t.Fatal(err)
	}
	if req.Method != "GET" {
		t.Fatalf("method %s", req.Method)
	}
	u, err := url.Parse(req.URL)
	if err != nil {
		t.Fatal(err)
	}
	if u.Query().Get("dlr_status") != "delivered" {
		t.Fatalf("query: %s", u.RawQuery)
	}
}

func TestBuildClientForwardCustomBodyTemplate(t *testing.T) {
	row := testDLRRow()
	tmpl := `status={{dlr_status}}&id={{message_id}}`
	row.DLRWebhookBodyTemplate = &tmpl
	req, err := BuildClientForward(row, "undelivered", time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	body := string(req.Body)
	if !strings.Contains(body, "status=undelivered") || !strings.Contains(body, row.MessageID) {
		t.Fatalf("body %q", body)
	}
}
