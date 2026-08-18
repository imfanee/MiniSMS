// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package web

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/minisms/minisms/internal/carrier"
	"github.com/minisms/minisms/internal/db"
	"github.com/minisms/minisms/internal/sending"
)

// smsLogResendResult drives the resend outcome modal (templates/admin/sms_logs/resend_result.html).
type smsLogResendResult struct {
	OK               bool
	Error            string
	OrigMessageID    string
	NewMessageID     string
	Client           string
	To               string
	From             string
	Carrier          string
	FailoverSequence int
	Segments         int
	Charged          string
	Status           string
}

// ResendSMSLog re-sends one logged message as a brand new SMS, reusing only the original client, sender,
// destination and text. It runs the live send pipeline (rating, routing, billing) exactly like a fresh
// client submission, so it creates a new sms_log and charges the client again. Mounted POST under the
// simulate permission (CSRF-protected) and audited as sms.resend.
func (h *Handlers) ResendSMSLog() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		if h.Send == nil {
			h.renderResendResult(w, r, smsLogResendResult{OrigMessageID: id, Error: "Send service is not configured."})
			return
		}
		d, err := h.loadSMSLogDetail(r, id)
		if err != nil {
			if err == pgx.ErrNoRows {
				h.renderResendResult(w, r, smsLogResendResult{OrigMessageID: id, Error: "Original message not found."})
				return
			}
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		cl, err := db.GetClient(r.Context(), h.Pool, d.ClientID)
		if err != nil {
			if err == pgx.ErrNoRows {
				h.renderResendResult(w, r, smsLogResendResult{OrigMessageID: id, Error: "Client not found."})
				return
			}
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		if cl.Status != "active" {
			h.renderResendResult(w, r, smsLogResendResult{OrigMessageID: id, Error: "Client is not active; cannot resend."})
			return
		}
		if strings.TrimSpace(d.MessageBody) == "" {
			h.renderResendResult(w, r, smsLogResendResult{OrigMessageID: id, Error: "Original message body is empty; nothing to resend."})
			return
		}

		requestedSender := ""
		if d.FromNumber != nil {
			requestedSender = strings.TrimSpace(*d.FromNumber)
		}
		systemDefaultSenderID := h.simulationSetting(r, "default_sender_id", "MiniSMS")
		sidResolution, err := carrier.ResolveSenderID(r.Context(), h.Pool, cl, requestedSender, systemDefaultSenderID)
		if err != nil {
			h.renderResendResult(w, r, smsLogResendResult{OrigMessageID: id, Error: "Sender ID not allowed for this client."})
			return
		}
		from := requestedSender
		if from == "" {
			from = sidResolution.Value
		}

		out := h.Send.Submit(r.Context(), sending.SubmitParams{
			Client: cl,
			Message: sending.AcceptedMessage{
				To:               d.ToNumber,
				From:             from,
				Body:             d.MessageBody,
				DLRRequested:     d.DLRRequested,
				DLRWebhookURL:    sending.ResolveDLRWebhookURL(d.DLRRequested, "", cl.DLRWebhookURL),
				IngressTransport: sending.IngressHTTP,
			},
			SenderID: sidResolution,
		})

		res := smsLogResendResult{
			OrigMessageID: id,
			Client:        derefOrDash(d.ClientName),
			To:            d.ToNumber,
			From:          from,
		}
		switch out.Kind {
		case sending.OutcomeAccepted:
			res.OK = true
			res.NewMessageID = out.Accepted.MessageID
			res.Carrier = out.Accepted.Carrier
			res.FailoverSequence = out.Accepted.FailoverSequence
			res.Segments = out.Accepted.Segments
			res.Charged = out.Accepted.Charged
			res.Status = "accepted"
		default:
			st := diagnoseSendStatusFromOutcome(out)
			res.Error = st.ErrorSummary
			res.NewMessageID = st.MessageID // a carrier-failure still creates a (failed) log
		}

		auditPayload := map[string]any{"new_message_id": res.NewMessageID}
		if res.Error != "" {
			auditPayload["error"] = res.Error
		}
		h.recordAudit(r, "sms.resend", "sms_log", &id, nil, auditPayload)
		h.renderResendResult(w, r, res)
	}
}

func (h *Handlers) renderResendResult(w http.ResponseWriter, r *http.Request, res smsLogResendResult) {
	if err := execT(w, h.SMSLogFragT, "sms_log_resend_result", res); err != nil {
		ServerError(w, r, err, h.Log, h.T500)
	}
}

// SimulateFromLog opens the routing simulator pre-filled with one logged message's client, destination,
// sender and text, so an operator can re-run the routing decision for that exact message. It renders the
// full simulate page (no send, no log). Mounted GET under the simulate permission.
func (h *Handlers) SimulateFromLog() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		d, err := h.loadSMSLogDetail(r, id)
		if err != nil {
			if err == pgx.ErrNoRows {
				http.NotFound(w, r)
				return
			}
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		clients, err := db.ListClients(r.Context(), h.Pool)
		if err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		from := ""
		if d.FromNumber != nil {
			from = *d.FromNumber
		}
		form := simulateForm{
			ClientID:    d.ClientID,
			Destination: d.ToNumber,
			SenderID:    from,
			Message:     d.MessageBody,
			Errors:      map[string]string{},
		}
		h.renderSimulatePage(w, r, clients, form, nil, diagnoseSendStatusView{})
	}
}
