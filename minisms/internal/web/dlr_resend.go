// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package web

import (
	"context"
	"html"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// DLRResender re-sends an already-received delivery receipt to the client over the client's current
// DLR channel (http/smpp/both). Implemented by *dlr.Processor and wired in main; kept as an interface
// so web does not import the dlr package. It is forward-only: no re-rating and no ledger writes.
type DLRResender interface {
	ResendToClient(ctx context.Context, messageID string) (string, error)
}

// ResendDLR re-delivers the stored DLR for one message to the client on operator request. It is
// mounted under the sms-logs permission group (POST, CSRF-protected) and is audited as dlr.resend.
func (h *Handlers) ResendDLR() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		if h.DLRResend == nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			writeResendAlert(w, "warning", "DLR resend is unavailable (SMPP ingress or processor not wired).")
			return
		}
		status, err := h.DLRResend.ResendToClient(r.Context(), id)
		payload := map[string]any{"forward_status": status}
		if err != nil {
			payload["error"] = err.Error()
		}
		h.recordAudit(r, "dlr.resend", "sms_log", &id, nil, payload)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			writeResendAlert(w, "danger", "Resend failed: "+err.Error())
			return
		}
		switch status {
		case "smpp_ok", "success":
			writeResendAlert(w, "success", "DLR re-sent to client ("+status+"). Close and reopen to see the timeline entry.")
		default:
			writeResendAlert(w, "warning", "DLR resend attempted but not delivered: "+status+".")
		}
	}
}

func writeResendAlert(w http.ResponseWriter, kind, msg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(`<span class="badge bg-` + kind + `">` + html.EscapeString(msg) + `</span>`))
}
