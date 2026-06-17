// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package smslog

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const (
	EventRequestReceived  = "request_received"
	EventCarrierDispatch  = "carrier_dispatch"
	EventCarrierSkipped   = "carrier_skipped"
	EventCarrierResponse  = "carrier_response"
	EventDispatchAccepted = "dispatch_accepted"
	EventDispatchFailed   = "dispatch_failed"
	EventDLRReceived      = "dlr_received"
	EventDLRForward       = "dlr_forward"
)

// TimelineEvent is one auditable step for an SMS message (stored in sms_logs.event_timeline).
type TimelineEvent struct {
	At     string         `json:"at"`
	Kind   string         `json:"kind"`
	Title  string         `json:"title"`
	Detail string         `json:"detail,omitempty"`
	Meta   map[string]any `json:"meta,omitempty"`
}

// TimelineEventView is formatted for admin templates.
type TimelineEventView struct {
	AtDisplay       string
	Kind            string
	Title           string
	Detail          string
	MetaJSON        string
	Badge           string
	MappedStatus    string
	HTTPMethod      string
	RequestPath     string
	QueryParamsJSON string
	RequestBody     string
	SMPPReceiptRef  string
	SMPPReceiptStat string
	Channel         string
}

func NewEvent(kind, title, detail string, meta map[string]any) TimelineEvent {
	return TimelineEvent{
		At:     time.Now().UTC().Format(time.RFC3339),
		Kind:   kind,
		Title:  title,
		Detail: detail,
		Meta:   meta,
	}
}

func FailoverLabel(n int) string {
	switch n {
	case 0:
		return "Primary"
	case 1:
		return "Failover 1"
	case 2:
		return "Failover 2"
	default:
		return fmt.Sprintf("Failover %d", n)
	}
}

func ParseTimeline(raw []byte) []TimelineEvent {
	if len(raw) == 0 || string(raw) == "[]" || string(raw) == "null" {
		return nil
	}
	var out []TimelineEvent
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil
	}
	return out
}

func FormatViews(events []TimelineEvent) []TimelineEventView {
	out := make([]TimelineEventView, 0, len(events))
	for _, e := range events {
		v := TimelineEventView{
			AtDisplay: formatAt(e.At),
			Kind:      e.Kind,
			Title:     e.Title,
			Detail:    e.Detail,
			Badge:     timelineBadge(e.Kind),
		}
		if e.Kind == EventDLRReceived {
			populateDLRReceivedView(&v, e.Meta)
		} else if len(e.Meta) > 0 {
			if b, err := json.MarshalIndent(e.Meta, "", "  "); err == nil {
				v.MetaJSON = string(b)
			}
		}
		out = append(out, v)
	}
	return out
}

func populateDLRReceivedView(v *TimelineEventView, meta map[string]any) {
	if meta == nil {
		return
	}
	if s, ok := metaString(meta, "mapped_status"); ok {
		v.MappedStatus = s
	}
	if s, ok := metaString(meta, "channel"); ok {
		v.Channel = s
	}
	if s, ok := metaString(meta, "http_method"); ok {
		v.HTTPMethod = s
	}
	if s, ok := metaString(meta, "request_path"); ok {
		v.RequestPath = s
	}
	if s, ok := metaString(meta, "request_body"); ok {
		v.RequestBody = s
	}
	if s, ok := metaString(meta, "smpp_receipt_ref"); ok {
		v.SMPPReceiptRef = s
	}
	if s, ok := metaString(meta, "smpp_receipt_stat"); ok {
		v.SMPPReceiptStat = s
	}
	if qp, ok := meta["query_params"]; ok {
		if b, err := json.MarshalIndent(qp, "", "  "); err == nil {
			v.QueryParamsJSON = string(b)
		}
	}
}

func metaString(meta map[string]any, key string) (string, bool) {
	v, ok := meta[key]
	if !ok || v == nil {
		return "", false
	}
	switch t := v.(type) {
	case string:
		s := strings.TrimSpace(t)
		if s == "" {
			return "", false
		}
		return s, true
	default:
		return fmt.Sprint(t), true
	}
}

func timelineBadge(kind string) string {
	switch kind {
	case EventRequestReceived:
		return "primary"
	case EventCarrierDispatch, EventCarrierResponse:
		return "info"
	case EventCarrierSkipped:
		return "warning"
	case EventDispatchAccepted:
		return "success"
	case EventDispatchFailed:
		return "danger"
	case EventDLRReceived:
		return "info"
	case EventDLRForward:
		return "secondary"
	default:
		return "secondary"
	}
}

func formatAt(iso string) string {
	iso = strings.TrimSpace(iso)
	if iso == "" {
		return "-"
	}
	t, err := time.Parse(time.RFC3339, iso)
	if err != nil {
		return iso
	}
	return t.UTC().Format("2006-01-02 15:04:05 UTC")
}

// LegacyDetail is used to synthesize a timeline when event_timeline is empty.
type LegacyDetail struct {
	ReceivedAt           time.Time
	DispatchedAt         *time.Time
	DeliveredAt          *time.Time
	FailedAt             *time.Time
	IngressTransport     string
	CarrierName          *string
	FailoverSequence     int
	CarrierResponseCode  *int
	CarrierResponseBody  *string
	CarrierMessageID     *string
	CarrierSkipReason    *string
	Status               string
	DLRRequested         bool
	DLRWebhookURL        *string
	DLRStatus            *string
	DLRReceivedAt        *time.Time
	DLRForwardedAt       *time.Time
	DLRForwardStatus     *string
	DLRForwardAttempts   int
}

func SynthesizeTimeline(d LegacyDetail) []TimelineEvent {
	var out []TimelineEvent
	ingress := strings.TrimSpace(d.IngressTransport)
	if ingress == "" {
		ingress = "http"
	}
	out = append(out, TimelineEvent{
		At:    d.ReceivedAt.UTC().Format(time.RFC3339),
		Kind:  EventRequestReceived,
		Title: "SMS request received",
		Detail: fmt.Sprintf("Ingress: %s", strings.ToUpper(ingress)),
	})

	if d.CarrierSkipReason != nil && strings.TrimSpace(*d.CarrierSkipReason) != "" {
		out = append(out, TimelineEvent{
			At:     timeAtOr(d.DispatchedAt, d.ReceivedAt),
			Kind:   EventCarrierSkipped,
			Title:  "Carrier(s) skipped",
			Detail: *d.CarrierSkipReason,
		})
	}

	if d.DispatchedAt != nil && d.CarrierName != nil {
		carrier := *d.CarrierName
		out = append(out, TimelineEvent{
			At:    d.DispatchedAt.UTC().Format(time.RFC3339),
			Kind:  EventCarrierDispatch,
			Title: fmt.Sprintf("Sent to %s (%s)", carrier, FailoverLabel(d.FailoverSequence)),
		})
		code := "-"
		if d.CarrierResponseCode != nil {
			code = fmt.Sprintf("%d", *d.CarrierResponseCode)
		}
		body := "-"
		if d.CarrierResponseBody != nil {
			body = *d.CarrierResponseBody
		}
		meta := map[string]any{"http_status": code}
		if d.CarrierMessageID != nil && *d.CarrierMessageID != "" {
			meta["carrier_message_id"] = *d.CarrierMessageID
		}
		out = append(out, TimelineEvent{
			At:     d.DispatchedAt.UTC().Format(time.RFC3339),
			Kind:   EventCarrierResponse,
			Title:  fmt.Sprintf("First response from %s", carrier),
			Detail: body,
			Meta:   meta,
		})
		if d.Status == "accepted" || d.Status == "sent" || d.Status == "delivered" {
			out = append(out, TimelineEvent{
				At:    d.DispatchedAt.UTC().Format(time.RFC3339),
				Kind:  EventDispatchAccepted,
				Title: fmt.Sprintf("Accepted by %s", carrier),
			})
		}
	}

	if d.FailedAt != nil && d.Status == "failed" {
		detail := "Dispatch failed"
		if d.CarrierResponseBody != nil {
			detail = *d.CarrierResponseBody
		}
		out = append(out, TimelineEvent{
			At:     d.FailedAt.UTC().Format(time.RFC3339),
			Kind:   EventDispatchFailed,
			Title:  "All carrier attempts failed",
			Detail: detail,
		})
	}

	if d.DLRReceivedAt != nil {
		status := "-"
		if d.DLRStatus != nil {
			status = *d.DLRStatus
		}
		out = append(out, TimelineEvent{
			At:     d.DLRReceivedAt.UTC().Format(time.RFC3339),
			Kind:   EventDLRReceived,
			Title:  "DLR received from carrier",
			Detail: fmt.Sprintf("Mapped status: %s", status),
		})
	}

	if d.DLRRequested {
		fallback := d.ReceivedAt
		if d.DLRReceivedAt != nil {
			fallback = *d.DLRReceivedAt
		}
		forwardAt := timeAtOr(d.DLRForwardedAt, fallback)
		status := "not attempted"
		if d.DLRForwardStatus != nil {
			status = *d.DLRForwardStatus
		}
		url := "none configured"
		if d.DLRWebhookURL != nil && strings.TrimSpace(*d.DLRWebhookURL) != "" {
			url = *d.DLRWebhookURL
		}
		title := "DLR forward to client"
		switch status {
		case "success", "smpp_ok":
			title = "DLR forwarded to client"
		case "no_url":
			title = "DLR not forwarded — no client webhook URL"
		case "failed", "smpp_no_bind":
			title = "DLR forward to client failed"
		}
		out = append(out, TimelineEvent{
			At:    forwardAt,
			Kind:  EventDLRForward,
			Title: title,
			Detail: fmt.Sprintf("Status: %s; webhook: %s; attempts: %d", status, url, d.DLRForwardAttempts),
		})
	}

	return out
}

func timeAtOr(primary *time.Time, fallback time.Time) string {
	if primary != nil {
		return primary.UTC().Format(time.RFC3339)
	}
	return fallback.UTC().Format(time.RFC3339)
}
