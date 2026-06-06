// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package web

import "testing"

func TestDiagnoseShouldKeepPolling(t *testing.T) {
	done, waiting := diagnoseShouldKeepPolling(diagnoseSendStatusView{
		MessageID: "id", Status: "accepted", DLRRequested: true,
	})
	if !done || !waiting {
		t.Fatal("expected polling while waiting for DLR")
	}
	done, waiting = diagnoseShouldKeepPolling(diagnoseSendStatusView{
		MessageID: "id", Status: "accepted", DLRRequested: true, DLRReceivedAt: "2026-01-01T00:00:00Z",
	})
	if done || waiting {
		t.Fatal("expected stop after DLR")
	}
	done, _ = diagnoseShouldKeepPolling(diagnoseSendStatusView{MessageID: "id", Status: "failed"})
	if done {
		t.Fatal("expected stop on failed")
	}
}
