// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package dlr

import (
	"net/http"
	"strings"
)

const maxInboundBodyStore = 32 << 10

// InboundCallback captures the carrier DLR callback as received (HTTP or SMPP).
type InboundCallback struct {
	Channel         string
	HTTPMethod      string
	RequestPath     string
	QueryParams     map[string]string
	RequestBody     string
	ContentType     string
	SMPPReceiptRef  string
	SMPPReceiptStat string
}

var sensitiveQueryKeys = map[string]struct{}{
	"secret": {}, "password": {}, "token": {}, "api_key": {}, "apikey": {}, "auth": {},
}

// InboundFromHTTP builds callback metadata from an HTTP DLR request.
func InboundFromHTTP(r *http.Request, queryParams map[string]string, body []byte) *InboundCallback {
	if r == nil {
		return nil
	}
	return &InboundCallback{
		Channel:     "http",
		HTTPMethod:  r.Method,
		RequestPath: r.URL.Path,
		QueryParams: redactQueryParams(queryParams),
		RequestBody: truncateBody(string(body)),
		ContentType: strings.TrimSpace(r.Header.Get("Content-Type")),
	}
}

// InboundFromSMPP builds callback metadata from a carrier SMPP delivery receipt.
func InboundFromSMPP(receiptRef, receiptStat string) *InboundCallback {
	receiptRef = strings.TrimSpace(receiptRef)
	receiptStat = strings.TrimSpace(receiptStat)
	if receiptRef == "" && receiptStat == "" {
		return nil
	}
	return &InboundCallback{
		Channel:         "smpp",
		SMPPReceiptRef:  receiptRef,
		SMPPReceiptStat: receiptStat,
	}
}

// TimelineMeta returns event_timeline metadata for a DLR received event.
func (c *InboundCallback) TimelineMeta(mappedStatus string) map[string]any {
	meta := map[string]any{"mapped_status": mappedStatus}
	if c == nil {
		return meta
	}
	if ch := strings.TrimSpace(c.Channel); ch != "" {
		meta["channel"] = ch
	}
	if m := strings.TrimSpace(c.HTTPMethod); m != "" {
		meta["http_method"] = m
	}
	if p := strings.TrimSpace(c.RequestPath); p != "" {
		meta["request_path"] = p
	}
	if len(c.QueryParams) > 0 {
		meta["query_params"] = c.QueryParams
	}
	if body := strings.TrimSpace(c.RequestBody); body != "" {
		meta["request_body"] = body
	}
	if ct := strings.TrimSpace(c.ContentType); ct != "" {
		meta["content_type"] = ct
	}
	if ref := strings.TrimSpace(c.SMPPReceiptRef); ref != "" {
		meta["smpp_receipt_ref"] = ref
	}
	if stat := strings.TrimSpace(c.SMPPReceiptStat); stat != "" {
		meta["smpp_receipt_stat"] = stat
	}
	return meta
}

func redactQueryParams(m map[string]string) map[string]string {
	if len(m) == 0 {
		return m
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		if _, ok := sensitiveQueryKeys[strings.ToLower(strings.TrimSpace(k))]; ok && strings.TrimSpace(v) != "" {
			out[k] = "***"
			continue
		}
		out[k] = v
	}
	return out
}

func truncateBody(s string) string {
	if len(s) <= maxInboundBodyStore {
		return s
	}
	return s[:maxInboundBodyStore] + "\n... (truncated)"
}
