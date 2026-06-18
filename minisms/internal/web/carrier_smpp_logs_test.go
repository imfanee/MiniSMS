// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package web

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/minisms/minisms/internal/smpp/egresslog"
)

// TestStreamSMPPLogs proves the SSE core emits buffered history first and then
// live lines, scoped to the requested carrier, and stops on context cancel.
func TestStreamSMPPLogs(t *testing.T) {
	hub := egresslog.NewHub()
	hub.Append("carrier-A", "INFO bind established addr=1.2.3.4:8082")
	hub.Append("carrier-B", "INFO other carrier line")

	rec := httptest.NewRecorder()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		streamSMPPLogs(ctx, rec, rec, hub, "carrier-A")
		close(done)
	}()

	// Let the history flush, then push a live line for carrier-A.
	time.Sleep(40 * time.Millisecond)
	hub.Append("carrier-A", "INFO deliver_sm receipt smsc_id=ABC stat=DELIVRD")
	time.Sleep(40 * time.Millisecond)
	cancel()
	<-done

	body := rec.Body.String()
	if !strings.Contains(body, ": connected") {
		t.Fatalf("missing connect preamble: %q", body)
	}
	if !strings.Contains(body, "data: INFO bind established addr=1.2.3.4:8082") {
		t.Fatalf("missing history line: %q", body)
	}
	if !strings.Contains(body, "data: INFO deliver_sm receipt smsc_id=ABC stat=DELIVRD") {
		t.Fatalf("missing live line: %q", body)
	}
	if strings.Contains(body, "other carrier line") {
		t.Fatalf("cross-carrier leak: %q", body)
	}
}
