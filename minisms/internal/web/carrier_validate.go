package web

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var reCurrency3 = regexp.MustCompile(`^[A-Z]{3}$`)
var reTemplateVar = regexp.MustCompile(`\{\{\s*([a-z0-9_]+)\s*\}}`)
var reHTTPHeaderName = regexp.MustCompile(`^[A-Za-z0-9-]+$`)

var templateAllowed = map[string]struct{}{
	"to":         {},
	"from":       {},
	"message":    {},
	"message_id": {},
	"timestamp":  {},
	"client_id":  {},
}

func validateCarrierForm(v url.Values) (name, endpoint, method, status, currency, rateGroup, notes string, errMap map[string]string) {
	errMap = map[string]string{}
	name = strings.TrimSpace(v.Get("name"))
	if name == "" {
		errMap["name"] = "Name is required"
	} else if len(name) > 200 {
		errMap["name"] = "Name must be 200 characters or less"
	}
	endpoint = strings.TrimSpace(v.Get("endpoint_url"))
	if endpoint == "" {
		errMap["endpoint_url"] = "Endpoint URL is required"
	} else {
		u, e := url.Parse(endpoint)
		if e != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
			errMap["endpoint_url"] = "Enter a valid http or https URL with host"
		}
	}
	method = strings.ToUpper(strings.TrimSpace(v.Get("http_method")))
	if method != "GET" && method != "POST" {
		errMap["http_method"] = "Method must be GET or POST"
	}
	status = strings.TrimSpace(v.Get("status"))
	if status != "active" && status != "inactive" {
		errMap["status"] = "Invalid status"
	}
	currency = strings.ToUpper(strings.TrimSpace(v.Get("currency")))
	if !reCurrency3.MatchString(currency) {
		errMap["currency"] = "Currency must be 3 capital letters, e.g. GBP"
	}
	rateGroup = strings.TrimSpace(v.Get("rate_group_id"))
	notes = strings.TrimSpace(v.Get("notes"))
	return
}

func validateHeaderForm(hname, hval string) map[string]string {
	m := map[string]string{}
	if strings.TrimSpace(hname) == "" {
		m["header_name"] = "Name is required"
	} else if !reHTTPHeaderName.MatchString(hname) {
		m["header_name"] = "Use only letters, numbers, and hyphens"
	}
	if hval == "" {
		m["header_value"] = "Value is required"
	} else if len(hval) > 1000 {
		m["header_value"] = "Max 1000 characters"
	}
	return m
}

func validateRequestTemplate(contentType, body, query string) map[string]string {
	m := map[string]string{}
	allowedCT := map[string]struct{}{
		"application/json":                    {},
		"application/x-www-form-urlencoded": {},
		"text/xml":                            {},
		"application/xml":                    {},
	}
	if _, ok := allowedCT[contentType]; !ok {
		m["content_type"] = "Invalid content type"
	}
	if contentType == "application/json" && body != "" && !json.Valid([]byte(body)) {
		m["body_template"] = "Body must be valid JSON for this content type"
	}
	for _, match := range reTemplateVar.FindAllStringSubmatch(body+" "+query, -1) {
		if len(match) < 2 {
			continue
		}
		if _, ok := templateAllowed[match[1]]; !ok {
			m["body_template"] = fmt.Sprintf("Disallowed variable {{%s}}", match[1])
			return m
		}
	}
	return m
}

// validatePayment returns amount string, parsed date, and error map (empty = ok).
func validatePayment(amountStr, pdate string) (string, time.Time, map[string]string) {
	m := map[string]string{}
	amountStr = strings.TrimSpace(amountStr)
	if amountStr == "" {
		m["amount"] = "Required"
		return "", time.Time{}, m
	}
	f, err := strconv.ParseFloat(amountStr, 64)
	if err != nil {
		m["amount"] = "Invalid number"
		return "", time.Time{}, m
	}
	if f <= 0 {
		m["amount"] = "Amount must be greater than 0"
		return "", time.Time{}, m
	}
	if strings.TrimSpace(pdate) == "" {
		m["payment_date"] = "Date required"
		return "", time.Time{}, m
	}
	d, err := time.Parse("2006-01-02", strings.TrimSpace(pdate))
	if err != nil {
		m["payment_date"] = "Invalid date"
		return "", time.Time{}, m
	}
	tomorrow := time.Now().UTC().AddDate(0, 0, 1).Truncate(24 * time.Hour)
	if d.After(tomorrow) {
		m["payment_date"] = "Not more than one day in the future"
		return "", time.Time{}, m
	}
	return amountStr, d, nil
}
