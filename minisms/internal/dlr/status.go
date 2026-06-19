// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package dlr

import "strings"

// StandardStatus normalizes carrier DLR tokens to delivered|undelivered|rejected|unknown.
func StandardStatus(raw string) string {
	v := strings.ToLower(strings.TrimSpace(raw))
	switch v {
	case "delivered", "undelivered", "rejected", "unknown":
		return v
	case "delivrd", "ok", "success":
		return "delivered"
	case "failed", "undeliv", "expired", "deleted", "undeliverable":
		return "undelivered"
	case "reject":
		return "rejected"
	default:
		return "unknown"
	}
}

// IsFinalStatus reports whether a normalized DLR status is terminal (no further receipt expected).
// Intermediate Gateway events (SMSC ACK, queued) normalize to "unknown" and are not final.
func IsFinalStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "delivered", "undelivered", "rejected":
		return true
	}
	return false
}

func fieldValue(fields map[string]string, keys ...string) string {
	for _, k := range keys {
		if v := strings.TrimSpace(fields[strings.ToLower(strings.TrimSpace(k))]); v != "" {
			return v
		}
	}
	return ""
}

func mapRawStatus(raw string, statusMap map[string]string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if statusMap != nil {
		if mapped, ok := statusMap[raw]; ok {
			return StandardStatus(mapped)
		}
		if mapped, ok := statusMap[strings.ToUpper(raw)]; ok {
			return StandardStatus(mapped)
		}
	}
	return StandardStatus(raw)
}

// NormalizeFromFields applies carrier-specific field names and status maps.
// Gateway/Kannel DLR callbacks use numeric status (%d → query "status") and optional text (%A → "answer").
func NormalizeFromFields(fields map[string]string, statusField *string, statusMap map[string]string) string {
	primary := "status"
	if statusField != nil && strings.TrimSpace(*statusField) != "" {
		primary = strings.ToLower(strings.TrimSpace(*statusField))
	}
	raw := fieldValue(fields, primary, "status", "stat", "dlr", "answer", "sms_status", "message_status")
	if mapped := mapRawStatus(raw, statusMap); mapped != "" {
		return mapped
	}
	return "unknown"
}

// StatusFromSMPPReceipt maps SMPP delivery receipt stat tokens (Appendix B).
func StatusFromSMPPReceipt(stat string) string {
	return StandardStatus(stat)
}
