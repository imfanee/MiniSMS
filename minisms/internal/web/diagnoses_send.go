// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package web

import (
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/minisms/minisms/internal/carrier"
	"github.com/minisms/minisms/internal/db"
	"github.com/minisms/minisms/internal/sending"
)

type diagnoseSendStatusView struct {
	MessageID         string
	Status            string
	CarrierName       string
	FailoverSequence  int
	Segments          int
	Charged           string
	DLRRequested      bool
	DLRStatus         string
	DLRReceivedAt     string
	DispatchedAt      string
	FailedAt          string
	CarrierResponse   string
	ErrorSummary      string
	KeepPolling       bool
	WaitingForDLR     bool
}

func parseDiagnoseForm(r *http.Request) simulateForm {
	_ = r.ParseForm()
	return simulateForm{
		ClientID:    strings.TrimSpace(r.FormValue("client_id")),
		Destination: strings.TrimSpace(r.FormValue("destination")),
		SenderID:    strings.TrimSpace(r.FormValue("sender_id")),
		Message:     strings.TrimSpace(r.FormValue("message")),
		Errors:      map[string]string{},
	}
}

func validateDiagnoseCommon(form *simulateForm) {
	if form.ClientID == "" {
		form.Errors["client_id"] = "Client is required"
	}
	if !simulateE164Re.MatchString(form.Destination) {
		form.Errors["destination"] = "Destination must be E.164 format (example: +447700900123)"
	}
}

func validateDiagnoseMessage(message string) map[string]string {
	m := map[string]string{}
	n := utf8.RuneCountInString(message)
	if n < 1 {
		m["message"] = "SMS text is required"
	} else if n > 1600 {
		m["message"] = "SMS text must be 1600 characters or fewer"
	}
	return m
}

func (h *Handlers) SendDiagnosticMessage() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if h.Send == nil {
			ServerError(w, r, errSendServiceNotConfigured, h.Log, h.T500)
			return
		}
		clients, err := db.ListClients(r.Context(), h.Pool)
		if err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		form := parseDiagnoseForm(r)
		validateDiagnoseCommon(&form)
		for k, v := range validateDiagnoseMessage(form.Message) {
			form.Errors[k] = v
		}
		if len(form.Errors) > 0 {
			h.renderSimulatePage(w, r, clients, form, nil, diagnoseSendStatusView{ErrorSummary: "Fix form errors before sending"})
			return
		}

		cl, err := db.GetClient(r.Context(), h.Pool, form.ClientID)
		if err != nil {
			if err == pgx.ErrNoRows {
				form.Errors["client_id"] = "Client not found"
				h.renderSimulatePage(w, r, clients, form, nil, diagnoseSendStatusView{ErrorSummary: "Client not found"})
				return
			}
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		if cl.Status != "active" {
			h.renderSimulatePage(w, r, clients, form, nil, diagnoseSendStatusView{ErrorSummary: "Client is not active"})
			return
		}

		systemDefaultSenderID := h.simulationSetting(r, "default_sender_id", "MiniSMS")
		sidResolution, err := carrier.ResolveSenderID(r.Context(), h.Pool, cl, form.SenderID, systemDefaultSenderID)
		if err != nil {
			h.renderSimulatePage(w, r, clients, form, nil, diagnoseSendStatusView{ErrorSummary: "Sender ID not allowed for this client"})
			return
		}
		from := form.SenderID
		if from == "" {
			from = sidResolution.Value
		}

		out := h.Send.Submit(r.Context(), sending.SubmitParams{
			Client: cl,
			Message: sending.AcceptedMessage{
				To:               form.Destination,
				From:             from,
				Body:             form.Message,
				DLRRequested:     true,
				DLRWebhookURL:    sending.ResolveDLRWebhookURL(true, "", cl.DLRWebhookURL),
				IngressTransport: sending.IngressHTTP,
			},
			SenderID: sidResolution,
		})

		status := diagnoseSendStatusFromOutcome(out)
		if status.MessageID != "" {
			if row, err := h.loadDiagnoseSendStatus(r, status.MessageID); err == nil {
				status = row
			}
		}
		h.renderSimulatePage(w, r, clients, form, nil, status)
	}
}

func (h *Handlers) GetDiagnosticSendStatus() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		msgID := chi.URLParam(r, "message_id")
		status, err := h.loadDiagnoseSendStatus(r, msgID)
		if err != nil {
			if err == pgx.ErrNoRows {
				status = diagnoseSendStatusView{MessageID: msgID, ErrorSummary: "Message not found", KeepPolling: false}
			} else {
				ServerError(w, r, err, h.Log, h.T500)
				return
			}
		}
		_ = execT(w, h.SimulateT, "diagnose_send_status", status)
	}
}

func diagnoseSendStatusFromOutcome(out sending.SubmitOutcome) diagnoseSendStatusView {
	switch out.Kind {
	case sending.OutcomeAccepted:
		return diagnoseSendStatusView{
			MessageID:        out.Accepted.MessageID,
			Status:           "accepted",
			CarrierName:      out.Accepted.Carrier,
			FailoverSequence: out.Accepted.FailoverSequence,
			Segments:         out.Accepted.Segments,
			Charged:          out.Accepted.Charged,
			DLRRequested:     out.Accepted.DLRRequested,
			KeepPolling:      out.Accepted.DLRRequested,
			WaitingForDLR:    out.Accepted.DLRRequested,
		}
	case sending.OutcomeInsufficientBalance:
		b := out.InsufficientBalance
		return diagnoseSendStatusView{
			ErrorSummary: "Insufficient balance (need " + b.Required + ", have " + b.Balance + ")",
		}
	case sending.OutcomeNoRate:
		return diagnoseSendStatusView{ErrorSummary: out.NoRate}
	case sending.OutcomeNoRoute:
		return diagnoseSendStatusView{ErrorSummary: out.NoRoute}
	case sending.OutcomeNoEligibleCarrier:
		return diagnoseSendStatusView{ErrorSummary: out.NoEligibleCarrier}
	case sending.OutcomeCarrierFailure:
		v := diagnoseSendStatusView{
			MessageID:    out.CarrierFailure.MessageID,
			Status:       "failed",
			ErrorSummary: "Carrier dispatch failed",
			KeepPolling:  false,
		}
		return v
	case sending.OutcomeTemporaryUnavailable:
		return diagnoseSendStatusView{ErrorSummary: out.TemporaryUnavailable}
	default:
		return diagnoseSendStatusView{ErrorSummary: "Unexpected send result"}
	}
}

func (h *Handlers) loadDiagnoseSendStatus(r *http.Request, messageID string) (diagnoseSendStatusView, error) {
	var v diagnoseSendStatusView
	var dlrStatus, carrierBody *string
	var dlrReceivedAt, dispatchedAt, failedAt *string
	err := h.Pool.QueryRow(r.Context(), `
		SELECT sl.message_id::text, sl.status, COALESCE(c.name, ''),
			sl.failover_sequence, sl.segments, sl.total_charged::text,
			sl.dlr_requested, sl.dlr_status, sl.dlr_received_at::text,
			sl.dispatched_at::text, sl.failed_at::text, sl.carrier_response_body
		FROM sms_logs sl
		LEFT JOIN carriers c ON c.carrier_id = sl.carrier_id
		WHERE sl.message_id = $1::uuid`, messageID).
		Scan(
			&v.MessageID, &v.Status, &v.CarrierName,
			&v.FailoverSequence, &v.Segments, &v.Charged,
			&v.DLRRequested, &dlrStatus, &dlrReceivedAt,
			&dispatchedAt, &failedAt, &carrierBody,
		)
	if err != nil {
		return v, err
	}
	if dlrStatus != nil {
		v.DLRStatus = *dlrStatus
	}
	if dlrReceivedAt != nil {
		v.DLRReceivedAt = *dlrReceivedAt
	}
	if dispatchedAt != nil {
		v.DispatchedAt = *dispatchedAt
	}
	if failedAt != nil {
		v.FailedAt = *failedAt
	}
	if carrierBody != nil {
		v.CarrierResponse = *carrierBody
	}
	v.KeepPolling, v.WaitingForDLR = diagnoseShouldKeepPolling(v)
	return v, nil
}

func diagnoseShouldKeepPolling(v diagnoseSendStatusView) (keepPolling, waitingDLR bool) {
	if v.MessageID == "" {
		return false, false
	}
	if v.DLRReceivedAt != "" {
		return false, false
	}
	if v.Status == "failed" {
		return false, false
	}
	if v.DLRRequested && (v.Status == "accepted" || v.Status == "delivered" || v.Status == "sent") {
		return true, true
	}
	if v.Status == "pending" {
		return true, false
	}
	return false, false
}

var errSendServiceNotConfigured = errors.New("send service not configured")

func (h *Handlers) RedirectSimulateToDiagnoses() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/admin/diagnoses/simulate", http.StatusFound)
	}
}
