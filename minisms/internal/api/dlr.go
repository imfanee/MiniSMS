// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/minisms/minisms/internal/db"
	"github.com/minisms/minisms/internal/dlr"
)

func (h *Handlers) HandleDLR() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		payloadFields, queryParams, bodyBytes := parseDLRPayload(r)
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
			writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
			return
		}

		if !dlr.VerifyInboundSecret(h.Config.SecretKey, r, row) {
			writeJSONError(w, http.StatusForbidden, "DLR_ERR_FORBIDDEN", "invalid inbound secret")
			return
		}

		proc := h.DLR
		if proc == nil {
			proc = &dlr.Processor{Pool: h.Pool, SecretKey: h.Config.SecretKey}
		}
		_ = proc.HandleInbound(r.Context(), messageID, payloadFields, dlr.InboundFromHTTP(r, queryParams, bodyBytes))
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

func parseDLRPayload(r *http.Request) (payloadFields map[string]string, queryParams map[string]string, bodyBytes []byte) {
	payloadFields = map[string]string{}
	queryParams = map[string]string{}
	if raw := strings.TrimSpace(r.URL.RawQuery); raw != "" {
		if vals, err := url.ParseQuery(raw); err == nil {
			for k, v := range vals {
				if len(v) > 0 {
					queryParams[k] = v[0]
					payloadFields[strings.ToLower(strings.TrimSpace(k))] = strings.TrimSpace(v[0])
				}
			}
		}
	}
	bodyBytes, _ = io.ReadAll(io.LimitReader(r.Body, 1<<20))
	_ = r.Body.Close()
	r.Body = io.NopCloser(bytes.NewReader(bodyBytes))
	if len(bodyBytes) == 0 {
		return payloadFields, queryParams, bodyBytes
	}
	var asJSON map[string]any
	if err := json.Unmarshal(bodyBytes, &asJSON); err == nil {
		for k, v := range asJSON {
			payloadFields[strings.ToLower(strings.TrimSpace(k))] = strings.TrimSpace(toString(v))
		}
		return payloadFields, queryParams, bodyBytes
	}
	if vals, err := url.ParseQuery(string(bodyBytes)); err == nil {
		for k, v := range vals {
			if len(v) > 0 {
				payloadFields[strings.ToLower(strings.TrimSpace(k))] = strings.TrimSpace(v[0])
			}
		}
	}
	return payloadFields, queryParams, bodyBytes
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
