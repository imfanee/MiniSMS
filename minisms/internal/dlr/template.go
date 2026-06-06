// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package dlr

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/minisms/minisms/internal/db"
)

const (
	WebhookMethodPOST = "POST"
	WebhookMethodGET  = "GET"
)

// DefaultBodyTemplate is the standard JSON POST body for client DLR webhooks.
const DefaultBodyTemplate = `{"event":"dlr","message_id":"{{message_id}}","client_ref":"{{client_ref}}","to":"{{to}}","from":"{{from}}","dlr_status":"{{dlr_status}}","carrier":"{{carrier}}","failover_sequence":"{{failover_sequence}}","received_at":"{{received_at}}","dlr_received_at":"{{dlr_received_at}}","segments":"{{segments}}","charged":"{{charged}}","source_addr_ton":"{{source_addr_ton}}","source_addr_npi":"{{source_addr_npi}}","dest_addr_ton":"{{dest_addr_ton}}","dest_addr_npi":"{{dest_addr_npi}}"}`

// DefaultQueryTemplate is the standard query string for GET client DLR webhooks.
const DefaultQueryTemplate = `event=dlr&message_id={{message_id}}&client_ref={{client_ref}}&to={{to}}&from={{from}}&dlr_status={{dlr_status}}&carrier={{carrier}}&failover_sequence={{failover_sequence}}&received_at={{received_at}}&dlr_received_at={{dlr_received_at}}&segments={{segments}}&charged={{charged}}&source_addr_ton={{source_addr_ton}}&source_addr_npi={{source_addr_npi}}&dest_addr_ton={{dest_addr_ton}}&dest_addr_npi={{dest_addr_npi}}`

// ForwardRequest is an outbound client DLR webhook call.
type ForwardRequest struct {
	Method      string
	URL         string
	Body        []byte
	SignPayload []byte
	ContentType string
}

func templateVars(row *db.DLRMessage, dlrStatus string, now time.Time) map[string]string {
	carrier := ""
	if row.CarrierName != nil {
		carrier = *row.CarrierName
	}
	clientRef := ""
	if row.ClientRef != nil {
		clientRef = *row.ClientRef
	}
	from := ""
	if row.FromNumber != nil {
		from = *row.FromNumber
	}
	return map[string]string{
		"event":              "dlr",
		"message_id":         row.MessageID,
		"client_ref":         clientRef,
		"to":                 row.ToNumber,
		"from":               from,
		"dlr_status":         dlrStatus,
		"carrier":            carrier,
		"failover_sequence":  fmt.Sprintf("%d", row.FailoverSequence),
		"received_at":        row.ReceivedAt.UTC().Format(time.RFC3339),
		"dlr_received_at":    now.UTC().Format(time.RFC3339),
		"segments":           fmt.Sprintf("%d", row.Segments),
		"charged":            row.TotalCharged,
		"source_addr_ton":    formatOptionalInt16(row.SourceAddrTON),
		"source_addr_npi":    formatOptionalInt16(row.SourceAddrNPI),
		"dest_addr_ton":      formatOptionalInt16(row.DestAddrTON),
		"dest_addr_npi":      formatOptionalInt16(row.DestAddrNPI),
	}
}

func formatOptionalInt16(v *int16) string {
	if v == nil {
		return ""
	}
	return fmt.Sprintf("%d", *v)
}

func injectTemplate(tmpl string, vars map[string]string, queryEscape bool) string {
	if tmpl == "" {
		return ""
	}
	out := tmpl
	for k, v := range vars {
		val := v
		if queryEscape {
			val = url.QueryEscape(v)
		}
		out = strings.ReplaceAll(out, "{{"+k+"}}", val)
	}
	return out
}

func normalizeWebhookMethod(method string) string {
	switch strings.ToUpper(strings.TrimSpace(method)) {
	case WebhookMethodGET:
		return WebhookMethodGET
	default:
		return WebhookMethodPOST
	}
}

func effectiveTemplate(custom *string, fallback string) string {
	if custom == nil {
		return fallback
	}
	if s := strings.TrimSpace(*custom); s != "" {
		return s
	}
	return fallback
}

// BuildClientForward constructs the HTTP request for forwarding a DLR to the client webhook.
func BuildClientForward(row *db.DLRMessage, dlrStatus string, now time.Time) (*ForwardRequest, error) {
	if row.DLRWebhookURL == nil || strings.TrimSpace(*row.DLRWebhookURL) == "" {
		return nil, fmt.Errorf("no webhook url")
	}
	baseURL := strings.TrimSpace(*row.DLRWebhookURL)
	vars := templateVars(row, dlrStatus, now)
	method := normalizeWebhookMethod(row.DLRWebhookMethod)

	switch method {
	case WebhookMethodGET:
		qTmpl := effectiveTemplate(row.DLRWebhookQueryTemplate, DefaultQueryTemplate)
		query := injectTemplate(qTmpl, vars, true)
		u, err := url.Parse(baseURL)
		if err != nil {
			return nil, err
		}
		existing := u.Query()
		if parsed, err := url.ParseQuery(query); err == nil {
			for k, vals := range parsed {
				for _, v := range vals {
					existing.Add(k, v)
				}
			}
		}
		u.RawQuery = existing.Encode()
		return &ForwardRequest{
			Method:      WebhookMethodGET,
			URL:         u.String(),
			Body:        nil,
			SignPayload: []byte(u.RawQuery),
			ContentType: "",
		}, nil
	default:
		bodyTmpl := effectiveTemplate(row.DLRWebhookBodyTemplate, DefaultBodyTemplate)
		bodyStr := injectTemplate(bodyTmpl, vars, false)
		if bodyStr == "" {
			payload := forwardPayload{
				Event:            "dlr",
				MessageID:        row.MessageID,
				ClientRef:        row.ClientRef,
				To:               row.ToNumber,
				From:             row.FromNumber,
				DLRStatus:        dlrStatus,
				Carrier:          row.CarrierName,
				FailoverSequence: row.FailoverSequence,
				ReceivedAt:       row.ReceivedAt.UTC().Format(time.RFC3339),
				DLRReceivedAt:    now.UTC().Format(time.RFC3339),
				Segments:         row.Segments,
				Charged:          row.TotalCharged,
				SourceAddrTON:    row.SourceAddrTON,
				SourceAddrNPI:    row.SourceAddrNPI,
				DestAddrTON:      row.DestAddrTON,
				DestAddrNPI:      row.DestAddrNPI,
			}
			b, err := json.Marshal(payload)
			if err != nil {
				return nil, err
			}
			bodyStr = string(b)
		}
		body := []byte(bodyStr)
		ct := "application/json"
		trim := strings.TrimSpace(bodyStr)
		if !strings.HasPrefix(trim, "{") && !strings.HasPrefix(trim, "[") {
			ct = "application/x-www-form-urlencoded"
		}
		return &ForwardRequest{
			Method:      WebhookMethodPOST,
			URL:         baseURL,
			Body:        body,
			SignPayload: body,
			ContentType: ct,
		}, nil
	}
}
