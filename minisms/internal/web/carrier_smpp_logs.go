// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package web

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/csrf"
	"github.com/minisms/minisms/internal/db"
	"github.com/minisms/minisms/internal/smpp/egresslog"
)

// SMPPController is the slice of the egress manager the web layer needs: live
// bind counts and a per-carrier restart. Implemented by *egress.Manager and
// wired in main; kept as an interface so web does not import the egress package.
type SMPPController interface {
	BindStatus(carrierID string) (ready, total int, present bool)
	Restart(carrierID string)
}

var errSMPPControllerUnavailable = errors.New("smpp controller unavailable")

// smppLogsPage drives the shared (carrier or client) SMPP log popup template.
type smppLogsPage struct {
	Title      string
	StreamURL  string
	RestartURL string
	DebugURL   string
	CSRFToken  string
	Nonce      string
}

// GetCarrierSMPPLogsView serves the standalone (popup) SMPP connection-log viewer
// for one carrier. It is read-only and gated by the carriers-view permission and
// the admin session like every other /admin route. No log data is emitted here;
// the page opens an EventSource to the stream endpoint, so logs only flow once
// the window is launched.
func (h *Handlers) GetCarrierSMPPLogsView() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cid := chi.URLParam(r, "id")
		c, err := db.GetCarrier(r.Context(), h.Pool, cid)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		nonce := randomNonce()
		// Lock the popup down: no external resources, scripts/styles only via the
		// per-response nonce, and the live stream may only reach same-origin.
		w.Header().Set("Content-Security-Policy",
			"default-src 'none'; connect-src 'self'; style-src 'nonce-"+nonce+"'; script-src 'nonce-"+nonce+"'; base-uri 'none'; form-action 'none'")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Cache-Control", "no-store")
		if err := execT(w, h.SMPPLogsT, "smpp_logs", smppLogsPage{
			Title:      c.Name,
			StreamURL:  "/admin/carriers/" + c.CarrierID + "/smpp-logs/stream",
			RestartURL: "/admin/carriers/" + c.CarrierID + "/smpp-restart",
			DebugURL:   "/admin/carriers/" + c.CarrierID + "/smpp-logs/debug",
			CSRFToken:  csrf.Token(r),
			Nonce:      nonce,
		}); err != nil {
			ServerError(w, r, err, h.Log, h.T500)
		}
	}
}

// SetCarrierSMPPDebug turns verbose (deep) SMPP logging on or off for a carrier
// so the live viewer surfaces command_status codes and per-PDU detail during
// troubleshooting. Read-only permission (it only affects log verbosity, and
// auto-expires); CSRF-protected POST. Responds with the effective state.
func (h *Handlers) SetCarrierSMPPDebug() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cid := chi.URLParam(r, "id")
		c, err := db.GetCarrier(r.Context(), h.Pool, cid)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		if h.SMPPLogHub == nil {
			http.Error(w, "log hub unavailable", http.StatusInternalServerError)
			return
		}
		on := r.FormValue("on") == "1" || r.FormValue("on") == "true"
		eff := h.SMPPLogHub.SetDebug(c.CarrierID, on)
		writeDebugState(w, eff)
	}
}

// StreamCarrierSMPPLogs streams a carrier's SMPP session log lines as
// Server-Sent Events: a snapshot of recent history first, then live lines while
// the connection stays open. GET-only and same-permission as the viewer; the
// admin session cookie authenticates the EventSource automatically. Lines never
// contain credentials (the egress layer controls their content).
func (h *Handlers) StreamCarrierSMPPLogs() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cid := chi.URLParam(r, "id")
		c, err := db.GetCarrier(r.Context(), h.Pool, cid)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		flusher, ok := w.(http.Flusher)
		if !ok || h.SMPPLogHub == nil {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache, no-transform")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no") // disable nginx proxy buffering for this response

		streamSMPPLogs(r.Context(), w, flusher, h.SMPPLogHub, c.CarrierID)
	}
}

// streamSMPPLogs writes the SSE body: recent history first, then live lines until
// the context is cancelled, with periodic heartbeat comments. Split out from the
// handler so it can be tested without a DB or an authenticated session.
func streamSMPPLogs(ctx context.Context, w io.Writer, flusher http.Flusher, hub *egresslog.Hub, carrierID string) {
	history, ch, cancel := hub.Subscribe(carrierID)
	defer cancel()

	fmt.Fprint(w, "retry: 3000\n: connected\n\n")
	for _, line := range history {
		writeSSE(w, line)
	}
	flusher.Flush()

	heartbeat := time.NewTicker(20 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-heartbeat.C:
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		case line, ok := <-ch:
			if !ok {
				return
			}
			writeSSE(w, line)
			flusher.Flush()
		}
	}
}

// writeSSE emits one already-flattened (single-line) log entry as an SSE data
// frame. The hub guarantees no embedded CR/LF, so framing cannot be broken.
func writeSSE(w io.Writer, line string) {
	fmt.Fprintf(w, "data: %s\n\n", line)
}

// writeDebugState returns the effective deep-logging state to the viewer.
func writeDebugState(w http.ResponseWriter, on bool) {
	w.Header().Set("Content-Type", "application/json")
	if on {
		fmt.Fprint(w, `{"debug":true}`)
	} else {
		fmt.Fprint(w, `{"debug":false}`)
	}
}

func randomNonce() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return base64.RawURLEncoding.EncodeToString(b[:])
}
