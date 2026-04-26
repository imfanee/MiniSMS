package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
)

func (h *Handlers) GetBalance() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		client := APIClientFromContext(r.Context())
		if client == nil {
			writeJSONError(w, http.StatusUnauthorized, "SMS_ERR_UNAUTHORIZED", "missing authenticated client")
			return
		}
		var balance string
		err := h.Pool.QueryRow(r.Context(), `SELECT balance::text FROM clients WHERE client_id=$1::uuid`, client.ClientID).Scan(&balance)
		if err != nil {
			writeJSONError(w, http.StatusServiceUnavailable, "SMS_ERR_TEMPORARY_UNAVAILABLE", "failed to read balance")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{
			"client_id": client.ClientID,
			"balance":   balance,
			"currency":  client.Currency,
		})
	}
}

func (h *Handlers) GetMessageStatus() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		client := APIClientFromContext(r.Context())
		if client == nil {
			writeJSONError(w, http.StatusUnauthorized, "SMS_ERR_UNAUTHORIZED", "missing authenticated client")
			return
		}
		msgID := chi.URLParam(r, "message_id")
		var d struct {
			MessageID                                                    string
			ClientID                                                     string
			Status                                                       string
			ToNumber                                                     string
			FromNumber, ClientRef, CarrierID, CarrierName               *string
			FailoverSequence, Segments, DLRForwardAttempts              int
			Charged                                                      string
			DLRRequested                                                 bool
			DLRWebhookURL, DLRStatus, DLRForwardStatus                  *string
			CarrierResponseCode                                          *int
			ReceivedAt, DispatchedAt, DeliveredAt, FailedAt             *string
			DLRReceivedAt, DLRForwardedAt                                *string
			SourceAddrTON, SourceAddrNPI, DestAddrTON, DestAddrNPI      *int16
		}
		err := h.Pool.QueryRow(r.Context(), `
			SELECT sl.message_id::text, sl.client_id::text, sl.status, sl.to_number, sl.from_number, sl.client_ref,
				sl.carrier_id::text, c.name, sl.failover_sequence, sl.segments, sl.total_charged::text,
				sl.carrier_response_code,
				sl.received_at::text, sl.dispatched_at::text, sl.delivered_at::text, sl.failed_at::text,
				sl.dlr_requested, sl.dlr_webhook_url, sl.dlr_status, sl.dlr_received_at::text, sl.dlr_forwarded_at::text,
				sl.dlr_forward_status, sl.dlr_forward_attempts,
				sl.source_addr_ton, sl.source_addr_npi, sl.dest_addr_ton, sl.dest_addr_npi
			FROM sms_logs sl
			LEFT JOIN carriers c ON c.carrier_id = sl.carrier_id
			WHERE sl.message_id = $1::uuid`, msgID).
			Scan(
				&d.MessageID, &d.ClientID, &d.Status, &d.ToNumber, &d.FromNumber, &d.ClientRef,
				&d.CarrierID, &d.CarrierName, &d.FailoverSequence, &d.Segments, &d.Charged,
				&d.CarrierResponseCode,
				&d.ReceivedAt, &d.DispatchedAt, &d.DeliveredAt, &d.FailedAt,
				&d.DLRRequested, &d.DLRWebhookURL, &d.DLRStatus, &d.DLRReceivedAt, &d.DLRForwardedAt,
				&d.DLRForwardStatus, &d.DLRForwardAttempts,
				&d.SourceAddrTON, &d.SourceAddrNPI, &d.DestAddrTON, &d.DestAddrNPI,
			)
		if err != nil {
			if err == pgx.ErrNoRows {
				writeJSONError(w, http.StatusNotFound, "SMS_ERR_NOT_FOUND", "message not found")
				return
			}
			writeJSONError(w, http.StatusServiceUnavailable, "SMS_ERR_TEMPORARY_UNAVAILABLE", "failed to read message status")
			return
		}
		if d.ClientID != client.ClientID {
			writeJSONError(w, http.StatusForbidden, "SMS_ERR_FORBIDDEN", "message does not belong to client")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"message_id":            d.MessageID,
			"status":                d.Status,
			"to":                    d.ToNumber,
			"from":                  d.FromNumber,
			"client_ref":            d.ClientRef,
			"segments":              d.Segments,
			"charged":               d.Charged,
			"carrier_id":            d.CarrierID,
			"carrier":               d.CarrierName,
			"failover_sequence":     d.FailoverSequence,
			"carrier_response_code": d.CarrierResponseCode,
			"received_at":           d.ReceivedAt,
			"dispatched_at":         d.DispatchedAt,
			"delivered_at":          d.DeliveredAt,
			"failed_at":             d.FailedAt,
			"dlr_requested":         d.DLRRequested,
			"dlr_webhook_url":       d.DLRWebhookURL,
			"dlr_status":            d.DLRStatus,
			"dlr_received_at":       d.DLRReceivedAt,
			"dlr_forwarded_at":      d.DLRForwardedAt,
			"dlr_forward_status":    d.DLRForwardStatus,
			"dlr_forward_attempts":  d.DLRForwardAttempts,
			"source_addr_ton":       d.SourceAddrTON,
			"source_addr_npi":       d.SourceAddrNPI,
			"dest_addr_ton":         d.DestAddrTON,
			"dest_addr_npi":         d.DestAddrNPI,
		})
	}
}
