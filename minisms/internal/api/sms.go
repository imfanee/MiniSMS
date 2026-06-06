// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/minisms/minisms/internal/carrier"
	"github.com/minisms/minisms/internal/config"
	"github.com/minisms/minisms/internal/db"
	"github.com/minisms/minisms/internal/dlr"
	"github.com/minisms/minisms/internal/sending"
	"github.com/minisms/minisms/internal/smpp/egress"
)

var e164Re = regexp.MustCompile(`^\+[1-9]\d{6,14}$`)

type Handlers struct {
	Pool   *pgxpool.Pool
	Config *config.Config
	Send   *sending.Service
	DLR    *dlr.Processor
}

type sendSMSRequest struct {
	To        string `json:"to"`
	From      string `json:"from"`
	Message   string `json:"message"`
	ClientRef string `json:"client_ref"`
	DLR       string `json:"dlr"`
	DLRURL    string `json:"dlr_url"`
}

type sendSMSResponse struct {
	Status           string  `json:"status"`
	MessageID        string  `json:"message_id"`
	ClientRef        string  `json:"client_ref,omitempty"`
	SenderID         string  `json:"sender_id"`
	SenderIDSource   string  `json:"sender_id_source"`
	Segments         int     `json:"segments"`
	Charged          string  `json:"charged"`
	BalanceRemaining string  `json:"balance_remaining"`
	Carrier          string  `json:"carrier"`
	FailoverSequence int     `json:"failover_sequence"`
	SourceAddrTON    *int16  `json:"source_addr_ton,omitempty"`
	SourceAddrNPI    *int16  `json:"source_addr_npi,omitempty"`
	DestAddrTON      *int16  `json:"dest_addr_ton,omitempty"`
	DestAddrNPI      *int16  `json:"dest_addr_npi,omitempty"`
	DLRRequested     bool    `json:"dlr_requested"`
	DLRWebhookURL    *string `json:"dlr_webhook_url"`
}

func NewHandlers(pool *pgxpool.Pool, cfg *config.Config, eg *egress.Manager, send *sending.Service) *Handlers {
	if send == nil {
		send = sending.NewWithEgress(pool, cfg, eg, nil)
	}
	dlrProc := &dlr.Processor{Pool: pool, SecretKey: cfg.SecretKey}
	return &Handlers{
		Pool:   pool,
		Config: cfg,
		Send:   send,
		DLR:    dlrProc,
	}
}

func (h *Handlers) SendSMS() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		client := APIClientFromContext(r.Context())
		if client == nil {
			writeJSONError(w, http.StatusUnauthorized, "SMS_ERR_UNAUTHORIZED", "missing authenticated client")
			return
		}
		var req sendSMSRequest
		r.Body = http.MaxBytesReader(w, r.Body, 65536)
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "SMS_ERR_INVALID_REQUEST", "invalid json body")
			return
		}
		req.To = strings.TrimSpace(req.To)
		req.From = strings.TrimSpace(req.From)
		req.Message = strings.TrimSpace(req.Message)
		req.ClientRef = strings.TrimSpace(req.ClientRef)
		req.DLR = strings.TrimSpace(req.DLR)
		req.DLRURL = strings.TrimSpace(req.DLRURL)
		if !e164Re.MatchString(req.To) {
			writeJSONError(w, http.StatusBadRequest, "SMS_ERR_INVALID_REQUEST", "to must be valid E.164 format")
			return
		}
		if len([]rune(req.Message)) < 1 || len([]rune(req.Message)) > 1600 {
			writeJSONError(w, http.StatusBadRequest, "SMS_ERR_INVALID_REQUEST", "message must be between 1 and 1600 characters")
			return
		}
		dlrRequested, err := parseDLRRequested(req.DLR)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "SMS_ERR_INVALID_REQUEST", "dlr must be YES or NO")
			return
		}
		if req.DLRURL != "" && !isHTTPSURL(req.DLRURL) {
			writeJSONError(w, http.StatusBadRequest, "SMS_ERR_INVALID_REQUEST", "dlr_url must be valid https:// URL")
			return
		}
		systemDefaultSenderID := h.systemSetting(r, "default_sender_id", "MiniSMS")
		sidResolution, err := carrier.ResolveSenderID(r.Context(), h.Pool, client, req.From, systemDefaultSenderID)
		if err != nil {
			detail := "Sender ID is not allowed for this client"
			if errors.Is(err, carrier.ErrSenderNotAllowed) {
				detail = carrier.SenderNotAllowedDetail(r.Context(), h.Pool, client.ClientID, req.From)
			}
			writeJSONError(w, http.StatusUnprocessableEntity, "SMS_ERR_SENDER_NOT_ALLOWED", detail)
			return
		}
		if req.From == "" {
			req.From = systemDefaultSenderID
		}

		out := h.Send.Submit(r.Context(), sending.SubmitParams{
			Client: client,
			Message: sending.AcceptedMessage{
				To:               req.To,
				From:             req.From,
				Body:             req.Message,
				ClientRef:        req.ClientRef,
				DLRRequested:     dlrRequested,
				DLRWebhookURL:    sending.ResolveDLRWebhookURL(dlrRequested, req.DLRURL, client.DLRWebhookURL),
				IngressTransport: sending.IngressHTTP,
			},
			SenderID:        sidResolution,
			DispatchTimeout: 0,
		})
		writeSubmitOutcome(w, out)
	}
}

func writeSubmitOutcome(w http.ResponseWriter, out sending.SubmitOutcome) {
	switch out.Kind {
	case sending.OutcomeAccepted:
		a := out.Accepted
		writeJSON(w, http.StatusAccepted, sendSMSResponse{
			Status:           "accepted",
			MessageID:        a.MessageID,
			ClientRef:        a.ClientRef,
			SenderID:         a.SenderID,
			SenderIDSource:   a.SenderIDSource,
			Segments:         a.Segments,
			Charged:          a.Charged,
			BalanceRemaining: a.BalanceRemaining,
			Carrier:          a.Carrier,
			FailoverSequence: a.FailoverSequence,
			SourceAddrTON:    a.SourceAddrTON,
			SourceAddrNPI:    a.SourceAddrNPI,
			DestAddrTON:      a.DestAddrTON,
			DestAddrNPI:      a.DestAddrNPI,
			DLRRequested:     a.DLRRequested,
			DLRWebhookURL:    a.DLRWebhookURL,
		})
	case sending.OutcomeInsufficientBalance:
		b := out.InsufficientBalance
		writeJSON(w, http.StatusPaymentRequired, map[string]string{
			"error":    "SMS_ERR_INSUFFICIENT_BALANCE",
			"balance":  b.Balance,
			"required": b.Required,
		})
	case sending.OutcomeNoRate:
		writeJSONError(w, http.StatusUnprocessableEntity, "SMS_ERR_NO_RATE", out.NoRate)
	case sending.OutcomeNoRoute:
		writeJSONError(w, http.StatusUnprocessableEntity, "SMS_ERR_NO_ROUTE", out.NoRoute)
	case sending.OutcomeNoEligibleCarrier:
		writeJSONError(w, http.StatusServiceUnavailable, "SMS_ERR_NO_ELIGIBLE_CARRIER", out.NoEligibleCarrier)
	case sending.OutcomeCarrierFailure:
		writeJSON(w, http.StatusBadGateway, map[string]string{
			"error":      "SMS_ERR_CARRIER_FAILURE",
			"message_id": out.CarrierFailure.MessageID,
		})
	case sending.OutcomeTemporaryUnavailable:
		writeJSONError(w, http.StatusServiceUnavailable, "SMS_ERR_TEMPORARY_UNAVAILABLE", out.TemporaryUnavailable)
	default:
		writeJSONError(w, http.StatusServiceUnavailable, "SMS_ERR_INTERNAL", "unexpected submit outcome")
	}
}

func parseDLRRequested(raw string) (bool, error) {
	if strings.TrimSpace(raw) == "" {
		return false, nil
	}
	switch strings.ToUpper(strings.TrimSpace(raw)) {
	case "YES":
		return true, nil
	case "NO":
		return false, nil
	default:
		return false, errors.New("invalid dlr value")
	}
}

func (h *Handlers) systemSetting(r *http.Request, key, def string) string {
	return db.Setting(r.Context(), h.Pool, key, def)
}

func isHTTPSURL(v string) bool {
	u, err := url.Parse(v)
	if err != nil {
		return false
	}
	return strings.EqualFold(u.Scheme, "https") && strings.TrimSpace(u.Host) != ""
}
