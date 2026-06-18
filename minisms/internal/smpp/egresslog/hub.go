// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.

// Package egresslog provides a small in-memory, per-carrier ring buffer of SMPP
// egress session events (bind lifecycle, delivery receipts, submit errors) plus
// a non-blocking subscriber fan-out for live tailing in the admin UI. It holds
// no secrets and never touches disk: callers must never pass credentials into a
// log line. Subscriber sends are non-blocking (dropped when a viewer is slow) so
// logging never stalls the egress hot path.
package egresslog

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

const (
	// maxLinesPerCarrier bounds memory: ~500 lines * a few hundred bytes/carrier.
	maxLinesPerCarrier = 500
	// subscriberBuffer is the per-viewer channel depth before lines are dropped.
	subscriberBuffer = 256
)

// Hub fans out per-carrier log lines to live subscribers and retains a bounded
// history so a viewer launched after the fact still sees recent events.
type Hub struct {
	mu      sync.Mutex
	history map[string][]string
	subs    map[string]map[int]chan string
	nextID  int
}

// NewHub returns an empty hub ready for use.
func NewHub() *Hub {
	return &Hub{
		history: make(map[string][]string),
		subs:    make(map[string]map[int]chan string),
	}
}

// Append records a preformatted line for a carrier and delivers it to every live
// subscriber. Newlines in the line are flattened so a single event stays a single
// line (also defends the SSE framing downstream).
func (h *Hub) Append(carrierID, line string) {
	if h == nil || carrierID == "" {
		return
	}
	line = strings.ReplaceAll(strings.ReplaceAll(line, "\r", " "), "\n", " ")
	h.mu.Lock()
	buf := append(h.history[carrierID], line)
	if len(buf) > maxLinesPerCarrier {
		buf = buf[len(buf)-maxLinesPerCarrier:]
	}
	h.history[carrierID] = buf
	for _, ch := range h.subs[carrierID] {
		select {
		case ch <- line:
		default: // drop for slow viewers; never block the egress goroutine
		}
	}
	h.mu.Unlock()
}

// Event formats and appends a timestamped event line. kv are alternating
// key/value strings. Callers control the content and must not pass secrets.
func (h *Hub) Event(carrierID, level, msg string, kv ...string) {
	if h == nil {
		return
	}
	var b strings.Builder
	b.WriteString(time.Now().UTC().Format("2006-01-02T15:04:05.000Z"))
	b.WriteByte(' ')
	b.WriteString(level)
	b.WriteByte(' ')
	b.WriteString(msg)
	for i := 0; i+1 < len(kv); i += 2 {
		fmt.Fprintf(&b, " %s=%s", kv[i], sanitizeVal(kv[i+1]))
	}
	h.Append(carrierID, b.String())
}

func sanitizeVal(v string) string {
	v = strings.ReplaceAll(strings.ReplaceAll(v, "\r", " "), "\n", " ")
	if strings.ContainsAny(v, " \t") {
		return "\"" + v + "\""
	}
	return v
}

// Subscribe returns a snapshot of the carrier's history plus a channel of future
// lines. The returned cancel must be called to release the subscription (after
// which no further sends occur, so the channel can be abandoned to GC).
func (h *Hub) Subscribe(carrierID string) (history []string, ch <-chan string, cancel func()) {
	c := make(chan string, subscriberBuffer)
	h.mu.Lock()
	hist := append([]string(nil), h.history[carrierID]...)
	id := h.nextID
	h.nextID++
	if h.subs[carrierID] == nil {
		h.subs[carrierID] = make(map[int]chan string)
	}
	h.subs[carrierID][id] = c
	h.mu.Unlock()

	cancel = func() {
		h.mu.Lock()
		if m := h.subs[carrierID]; m != nil {
			delete(m, id)
			if len(m) == 0 {
				delete(h.subs, carrierID)
			}
		}
		h.mu.Unlock()
	}
	return hist, c, cancel
}
