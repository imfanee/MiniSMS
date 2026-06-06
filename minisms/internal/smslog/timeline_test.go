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
