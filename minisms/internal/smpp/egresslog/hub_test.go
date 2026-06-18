// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package egresslog

import (
	"strings"
	"testing"
)

func TestHub_HistoryAndFanout(t *testing.T) {
	h := NewHub()
	h.Append("c1", "before subscribe")

	hist, ch, cancel := h.Subscribe("c1")
	defer cancel()
	if len(hist) != 1 || hist[0] != "before subscribe" {
		t.Fatalf("history snapshot wrong: %v", hist)
	}

	h.Append("c1", "after subscribe")
	select {
	case line := <-ch:
		if line != "after subscribe" {
			t.Fatalf("got %q", line)
		}
	default:
		t.Fatal("expected a live line")
	}

	// Another carrier's lines must not leak into this subscription.
	h.Append("c2", "other carrier")
	select {
	case line := <-ch:
		t.Fatalf("unexpected cross-carrier line: %q", line)
	default:
	}
}

func TestHub_RingBufferBound(t *testing.T) {
	h := NewHub()
	for i := 0; i < maxLinesPerCarrier+50; i++ {
		h.Event("c1", "INFO", "line")
	}
	hist, _, cancel := h.Subscribe("c1")
	defer cancel()
	if len(hist) != maxLinesPerCarrier {
		t.Fatalf("expected %d retained, got %d", maxLinesPerCarrier, len(hist))
	}
}

func TestHub_FlattensNewlinesAndCancel(t *testing.T) {
	h := NewHub()
	hist, ch, cancel := h.Subscribe("c1")
	_ = hist
	h.Append("c1", "two\nlines\rhere")
	if got := <-ch; strings.ContainsAny(got, "\r\n") {
		t.Fatalf("newline not flattened: %q", got)
	}
	// After cancel, no further sends reach the channel.
	cancel()
	h.Append("c1", "post cancel")
	select {
	case line := <-ch:
		t.Fatalf("received after cancel: %q", line)
	default:
	}
}
