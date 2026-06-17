// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package smslog

import (
	"testing"
	"time"
)

func TestSynthesizeTimelineIncludesRequestAndDLR(t *testing.T) {
	recv := time.Date(2026, 6, 5, 10, 0, 0, 0, time.UTC)
	disp := recv.Add(2 * time.Second)
	dlrAt := recv.Add(30 * time.Second)
	status := "delivered"
	fwd := "no_url"
	events := SynthesizeTimeline(LegacyDetail{
		ReceivedAt: recv, DispatchedAt: &disp,
		IngressTransport: "http", CarrierName: strPtr("Carrier A"), FailoverSequence: 0,
		CarrierResponseCode: intPtr(202), CarrierResponseBody: strPtr("accepted"),
		Status: "accepted", DLRRequested: true, DLRReceivedAt: &dlrAt, DLRStatus: &status,
		DLRForwardStatus: &fwd,
	})
	if len(events) < 4 {
		t.Fatalf("expected synthesized timeline, got %d events", len(events))
	}
	if events[0].Kind != EventRequestReceived {
		t.Fatalf("first event: %s", events[0].Kind)
	}
	foundDLR := false
	for _, e := range events {
		if e.Kind == EventDLRForward {
			foundDLR = true
		}
	}
	if !foundDLR {
		t.Fatal("expected dlr_forward in synthesized timeline")
	}
}

func strPtr(s string) *string { return &s }
func intPtr(n int) *int       { return &n }

func TestFormatViewsDLRReceivedShowsInboundPayload(t *testing.T) {
	events := []TimelineEvent{{
		At:     time.Now().UTC().Format(time.RFC3339),
		Kind:   EventDLRReceived,
		Title:  "DLR received from carrier",
		Detail: "Mapped status: delivered",
		Meta: map[string]any{
			"mapped_status": "delivered",
			"channel":       "http",
			"http_method":   "POST",
			"request_path":  "/api/v1/dlr/abc",
			"query_params":  map[string]string{"status": "DELIVRD"},
			"request_body":  `{"status":"DELIVRD"}`,
		},
	}}
	views := FormatViews(events)
	if len(views) != 1 {
		t.Fatalf("views: %d", len(views))
	}
	v := views[0]
	if v.MappedStatus != "delivered" {
		t.Fatalf("mapped: %q", v.MappedStatus)
	}
	if v.QueryParamsJSON == "" {
		t.Fatal("expected query params json")
	}
	if v.RequestBody == "" {
		t.Fatal("expected request body")
	}
	if v.MetaJSON != "" {
		t.Fatalf("expected no raw meta json for dlr_received, got %q", v.MetaJSON)
	}
}
