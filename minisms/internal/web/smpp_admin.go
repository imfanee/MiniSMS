// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package web

import (
	"fmt"
	"net"
	"strconv"
	"strings"
)

func parseBoolCheckbox(v string) bool {
	return v == "on" || v == "true" || v == "1"
}

func validateInterconnectType(v string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "http", "smpp":
		return strings.ToLower(strings.TrimSpace(v)), true
	default:
		return "", false
	}
}

func validateCarrierBindMode(v string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "tx", "trx":
		return strings.ToLower(strings.TrimSpace(v)), true
	default:
		return "", false
	}
}

func validateDLRDeliveryMode(v string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "http", "smpp", "both":
		return strings.ToLower(strings.TrimSpace(v)), true
	default:
		return "", false
	}
}

func validateSMPPCIDRs(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	for _, part := range strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == '\n' || r == ';'
	}) {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if strings.Contains(part, "/") {
			if _, _, err := net.ParseCIDR(part); err != nil {
				return fmt.Errorf("invalid CIDR %q", part)
			}
			continue
		}
		if ip := net.ParseIP(part); ip == nil {
			return fmt.Errorf("invalid IP or CIDR %q", part)
		}
	}
	return nil
}

func parseOptionalInt16(v string) (*int16, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil, nil
	}
	n, err := strconv.ParseInt(v, 10, 16)
	if err != nil {
		return nil, err
	}
	i := int16(n)
	return &i, nil
}

func strPtrOrNil(s string) *string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	return &s
}
