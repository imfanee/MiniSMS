package api

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/minisms/minisms/internal/db"
)

type dlrForwardPayload struct {
	Event            string  `json:"event"`
	MessageID        string  `json:"message_id"`
	ClientRef        *string `json:"client_ref"`
	To               string  `json:"to"`
	From             *string `json:"from"`
	DLRStatus        string  `json:"dlr_status"`
	Carrier          *string `json:"carrier"`
	FailoverSequence int     `json:"failover_sequence"`
	ReceivedAt       string  `json:"received_at"`
	DLRReceivedAt    string  `json:"dlr_received_at"`
	Segments         int     `json:"segments"`
	Charged          string  `json:"charged"`
	SourceAddrTON    *int16  `json:"source_addr_ton"`
	SourceAddrNPI    *int16  `json:"source_addr_npi"`
	DestAddrTON      *int16  `json:"dest_addr_ton"`
	DestAddrNPI      *int16  `json:"dest_addr_npi"`
}

func (h *Handlers) HandleDLR() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		payloadFields, _ := parseDLRPayload(r)
		messageID := strings.TrimSpace(chi.URLParam(r, "message_id"))
		if messageID == "" {
			for _, k := range []string{"ref", "msgid", "reference"} {
				if v := strings.TrimSpace(r.URL.Query().Get(k)); v != "" {
					messageID = v
					break
				}
			}
		}
		if messageID == "" {
			for _, k := range []string{"ref", "msgid", "reference", "message_id", "messageid", "id"} {
				if v := strings.TrimSpace(payloadFields[strings.ToLower(k)]); v != "" {
					messageID = v
					break
				}
			}
		}
		if messageID == "" {
			writeJSONError(w, http.StatusBadRequest, "DLR_ERR_INVALID_REQUEST", "missing message_id")
			return
		}
		if _, err := uuid.Parse(messageID); err != nil {
			writeJSONError(w, http.StatusBadRequest, "DLR_ERR_INVALID_REQUEST", "invalid message_id")
			return
		}

		row, err := db.GetSMSLogForDLR(r.Context(), h.Pool, messageID)
		if err != nil {
			if db.IsNotFound(err) {
				writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
				return
			}
			writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
			return
		}

		if !h.verifyInboundDLRSecret(r, row) {
			writeJSONError(w, http.StatusForbidden, "DLR_ERR_FORBIDDEN", "invalid inbound secret")
			return
		}

		dlrStatus := normalizeDLRStatus(payloadFields, row)
		_ = db.UpdateDLRReceived(r.Context(), h.Pool, messageID, dlrStatus)

		if !row.DLRRequested {
			writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
			return
		}
		if row.DLRWebhookURL == nil || strings.TrimSpace(*row.DLRWebhookURL) == "" {
			_ = db.UpdateDLRForwardStatus(r.Context(), h.Pool, messageID, "no_url", false, false)
			writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
			return
		}

		now := time.Now().UTC()
		forwardPayload := dlrForwardPayload{
			Event:            "dlr",
			MessageID:        row.MessageID,
			ClientRef:        row.ClientRef,
			To:               row.ToNumber,
			From:             row.FromNumber,
			DLRStatus:        dlrStatus,
			Carrier:          row.CarrierName,
			FailoverSequence: row.FailoverSequence,
			ReceivedAt:       row.ReceivedAt.UTC().Format(time.RFC3339),
			DLRReceivedAt:    now.Format(time.RFC3339),
			Segments:         row.Segments,
			Charged:          row.TotalCharged,
			SourceAddrTON:    row.SourceAddrTON,
			SourceAddrNPI:    row.SourceAddrNPI,
			DestAddrTON:      row.DestAddrTON,
			DestAddrNPI:      row.DestAddrNPI,
		}
		body, err := json.Marshal(forwardPayload)
		if err != nil {
			_ = db.UpdateDLRForwardStatus(r.Context(), h.Pool, messageID, "failed", false, true)
			writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
			return
		}
		req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, *row.DLRWebhookURL, bytes.NewReader(body))
		if err != nil {
			_ = db.UpdateDLRForwardStatus(r.Context(), h.Pool, messageID, "failed", false, true)
			writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
			return
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "MiniSMS-DLR/1.0")
		if sig := h.signForwardDLR(body, row.ClientWebhookSecret); sig != "" {
			req.Header.Set("X-MiniSMS-Signature", sig)
		}
		client := &http.Client{
			Timeout: 10 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
		resp, err := client.Do(req)
		if err != nil {
			_ = db.UpdateDLRForwardStatus(r.Context(), h.Pool, messageID, "failed", false, true)
			slog.Warn("dlr forward failed", "message_id", messageID, "webhook_url", *row.DLRWebhookURL, "error", err.Error())
			writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
			return
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode >= 200 && resp.StatusCode <= 299 {
			_ = db.UpdateDLRForwardStatus(r.Context(), h.Pool, messageID, "success", true, true)
		} else {
			_ = db.UpdateDLRForwardStatus(r.Context(), h.Pool, messageID, "failed", false, true)
			slog.Warn("dlr forward failed", "message_id", messageID, "webhook_url", *row.DLRWebhookURL, "http_status", resp.StatusCode)
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

func parseDLRPayload(r *http.Request) (map[string]string, []byte) {
	out := map[string]string{}
	for k, vals := range r.URL.Query() {
		if len(vals) > 0 {
			out[strings.ToLower(strings.TrimSpace(k))] = strings.TrimSpace(vals[0])
		}
	}
	bodyBytes, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	_ = r.Body.Close()
	r.Body = io.NopCloser(bytes.NewReader(bodyBytes))
	if len(bodyBytes) == 0 {
		return out, bodyBytes
	}
	var asJSON map[string]any
	if err := json.Unmarshal(bodyBytes, &asJSON); err == nil {
		for k, v := range asJSON {
			out[strings.ToLower(strings.TrimSpace(k))] = strings.TrimSpace(toString(v))
		}
		return out, bodyBytes
	}
	if vals, err := url.ParseQuery(string(bodyBytes)); err == nil {
		for k, v := range vals {
			if len(v) > 0 {
				out[strings.ToLower(strings.TrimSpace(k))] = strings.TrimSpace(v[0])
			}
		}
	}
	return out, bodyBytes
}

func normalizeDLRStatus(fields map[string]string, row *db.DLRMessage) string {
	field := "status"
	if row.CarrierDLRStatusField != nil && strings.TrimSpace(*row.CarrierDLRStatusField) != "" {
		field = strings.ToLower(strings.TrimSpace(*row.CarrierDLRStatusField))
	}
	raw := strings.TrimSpace(fields[strings.ToLower(field)])
	if raw == "" {
		return "unknown"
	}
	if row.CarrierDLRStatusMap != nil {
		if mapped, ok := row.CarrierDLRStatusMap[raw]; ok {
			return standardDLRStatus(mapped)
		}
		if mapped, ok := row.CarrierDLRStatusMap[strings.ToUpper(raw)]; ok {
			return standardDLRStatus(mapped)
		}
	}
	return standardDLRStatus(raw)
}

func standardDLRStatus(raw string) string {
	v := strings.ToLower(strings.TrimSpace(raw))
	switch v {
	case "delivered", "undelivered", "rejected", "unknown":
		return v
	case "delivrd", "ok", "success":
		return "delivered"
	case "failed", "undeliv":
		return "undelivered"
	default:
		return "unknown"
	}
}

func (h *Handlers) verifyInboundDLRSecret(r *http.Request, row *db.DLRMessage) bool {
	if row.CarrierInboundSecret == nil || strings.TrimSpace(*row.CarrierInboundSecret) == "" {
		return true
	}
	expected, err := db.DecryptValue(h.Config.SecretKey, *row.CarrierInboundSecret)
	if err != nil {
		return false
	}
	candidate := strings.TrimSpace(r.URL.Query().Get("secret"))
	if candidate == "" {
		candidate = strings.TrimSpace(r.Header.Get("X-DLR-Secret"))
	}
	if candidate == "" {
		candidate = strings.TrimSpace(r.Header.Get("X-Callback-Secret"))
	}
	if candidate == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(candidate), []byte(expected)) == 1
}

func (h *Handlers) signForwardDLR(body []byte, secretEnc *string) string {
	if secretEnc == nil || strings.TrimSpace(*secretEnc) == "" {
		return ""
	}
	secret, err := db.DecryptValue(h.Config.SecretKey, *secretEnc)
	if err != nil || strings.TrimSpace(secret) == "" {
		return ""
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func toString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case float64:
		return strings.TrimSpace(strings.TrimRight(strings.TrimRight(strconv.FormatFloat(t, 'f', -1, 64), "0"), "."))
	case bool:
		if t {
			return "true"
		}
		return "false"
	default:
		b, _ := json.Marshal(v)
		return string(b)
	}
}
