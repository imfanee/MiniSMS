// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package web

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/csrf"
	"github.com/minisms/minisms/internal/db"
)

// SMPPIngressController is the slice of the SMPP ingress server the web layer
// needs: live bind counts and a per-client restart (force-disconnect). Implemented
// by *server.Server and wired in main; an interface so web does not import the
// server package. Nil when SMPP ingress is disabled.
type SMPPIngressController interface {
	BindCount(clientID string) int
	RestartClient(clientID string)
}

// GetClientSMPPLogsView serves the standalone popup that live-tails a client's
// SMPP ingress session events. Read-only, gated by the clients-view permission
// and the admin session; logs only flow once the EventSource connects.
func (h *Handlers) GetClientSMPPLogsView() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		c, err := db.GetClient(r.Context(), h.Pool, id)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		nonce := randomNonce()
		w.Header().Set("Content-Security-Policy",
			"default-src 'none'; connect-src 'self'; style-src 'nonce-"+nonce+"'; script-src 'nonce-"+nonce+"'; base-uri 'none'; form-action 'none'")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Cache-Control", "no-store")
		if err := execT(w, h.SMPPLogsT, "smpp_logs", smppLogsPage{
			Title:      c.Name + " (ingress)",
			StreamURL:  "/admin/clients/" + c.ClientID + "/smpp-logs/stream",
			RestartURL: "/admin/clients/" + c.ClientID + "/smpp-restart",
			CSRFToken:  csrf.Token(r),
			Nonce:      nonce,
		}); err != nil {
			ServerError(w, r, err, h.Log, h.T500)
		}
	}
}

// StreamClientSMPPLogs streams a client's SMPP ingress session log lines as SSE
// (history then live). GET-only, same permission as the viewer; the admin session
// cookie authenticates the EventSource. Lines never contain credentials.
func (h *Handlers) StreamClientSMPPLogs() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		c, err := db.GetClient(r.Context(), h.Pool, id)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		flusher, ok := w.(http.Flusher)
		if !ok || h.SMPPIngressLogHub == nil {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache, no-transform")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no")
		streamSMPPLogs(r.Context(), w, flusher, h.SMPPIngressLogHub, c.ClientID)
	}
}

// RestartClientSMPP force-disconnects a client's bound ESME sessions (they
// reconnect). State-changing, so behind PermClientsEdit + CSRF; audited. The log
// popup calls this via fetch, so its live stream shows the disconnect and the
// client's fresh binds.
func (h *Handlers) RestartClientSMPP() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		c, err := db.GetClient(r.Context(), h.Pool, id)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		if h.SMPPIngress == nil {
			ServerError(w, r, errSMPPControllerUnavailable, h.Log, h.T500)
			return
		}
		before := h.SMPPIngress.BindCount(c.ClientID)
		h.SMPPIngress.RestartClient(c.ClientID)
		name := c.Name
		h.recordAudit(r, "client.smpp_restart", "client", &c.ClientID, &name, map[string]any{
			"binds_disconnected": before,
		})
		c, _ = db.GetClient(r.Context(), h.Pool, id)
		_ = execT(w, h.CLIFragT, "client_smpp_panel",
			h.clientSMPPPanelData(r, c, "SMPP sessions disconnected; clients will reconnect.", nil))
	}
}
