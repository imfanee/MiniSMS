// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package web

import (
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/minisms/minisms/internal/carrier"
)

var reCurrency3 = regexp.MustCompile(`^[A-Z]{3}$`)
var reTemplateVar = regexp.MustCompile(`\{\{\s*([a-z0-9_]+)\s*\}}`)
var reHTTPHeaderName = regexp.MustCompile(`^[A-Za-z0-9-]+$`)

var templateAllowed = map[string]struct{}{
	"to":                         {},
	"from":                       {},
	"message":                    {},
	"message_id":                 {},
	"timestamp":                  {},
	"client_id":                  {},
	"dlr_callback_url":           {},
	"dlr_callback_url_encoded":   {},
	"source_addr_ton":            {},
	"source_addr_npi":            {},
	"dest_addr_ton":              {},
	"dest_addr_npi":              {},
}

var carrierSenderPolicies = map[string]struct{}{
	"any": {}, "numeric": {}, "e164": {}, "list": {}, "none": {},
}

func applyCarrierSenderFields(v url.Values, errs map[string]string) (policy string, defaultSID *string) {
	policy = strings.TrimSpace(v.Get("sender_id_policy"))
	if policy == "" {
		policy = "any"
	}
	if _, ok := carrierSenderPolicies[policy]; !ok {
		errs["sender_id_policy"] = "Invalid sender ID policy"
		policy = "any"
	}
	def := strings.TrimSpace(v.Get("default_sender_id_value"))
	if len([]rune(def)) > 15 {
		errs["default_sender_id_value"] = "Max 15 characters"
	}
	if def != "" {
		defaultSID = &def
	}
	return policy, defaultSID
}

func validateCarrierForm(v url.Values) (name, status, currency, rateGroup, notes string, errMap map[string]string) {
	errMap = map[string]string{}
	name = strings.TrimSpace(v.Get("name"))
	if name == "" {
		errMap["name"] = "Name is required"
	} else if len(name) > 200 {
		errMap["name"] = "Name must be 200 characters or less"
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

func sampleTemplateVars() map[string]string {
	return map[string]string{
		"to":                       "+447700900123",
		"from":                     "MiniSMS",
		"message":                  "Test message",
		"message_id":               "00000000-0000-4000-8000-000000000001",
		"timestamp":                "2026-04-20T10:00:00Z",
		"client_id":                "00000000-0000-4000-8000-000000000002",
		"dlr_callback_url":         "https://sms.telecotech.net/api/v1/dlr/00000000-0000-4000-8000-000000000001",
		"dlr_callback_url_encoded": "https%3A%2F%2Fsms.telecotech.net%2Fapi%2Fv1%2Fdlr%2F00000000-0000-4000-8000-000000000001",
		"source_addr_ton":          "5",
		"source_addr_npi":          "0",
		"dest_addr_ton":            "1",
		"dest_addr_npi":            "1",
	}
}

func validJSONAfterTemplateSubst(tmpl string) bool {
	return validateJSONSyntax(tmpl) == ""
}

func byteOffsetLineCol(b []byte, offset int) (line, col int) {
	if offset < 0 {
		offset = 0
	}
	if offset > len(b) {
		offset = len(b)
	}
	line, col = 1, 1
	for i := 0; i < offset && i < len(b); i++ {
		if b[i] == '\n' {
			line++
			col = 1
		} else {
			col++
		}
	}
	return line, col
}

func validateJSONSyntax(tmpl string) string {
	tmpl = strings.TrimSpace(tmpl)
	if tmpl == "" {
		return ""
	}
	sub := carrier.InjectVariables(tmpl, sampleTemplateVars())
	var v any
	if err := json.Unmarshal([]byte(sub), &v); err != nil {
		var syn *json.SyntaxError
		if errors.As(err, &syn) {
			line, col := byteOffsetLineCol([]byte(sub), int(syn.Offset))
			return fmt.Sprintf(
				"syntax error at line %d, column %d (%s). Put each {{variable}} inside double quotes, e.g. \"to\":\"{{to}}\".",
				line, col, jsonErrSummary(err),
			)
		}
		return fmt.Sprintf("%s. Put each {{variable}} inside double quotes, e.g. \"to\":\"{{to}}\".", err.Error())
	}
	return ""
}

func jsonErrSummary(err error) string {
	msg := err.Error()
	if idx := strings.Index(msg, " at "); idx > 0 {
		return msg[:idx]
	}
	return msg
}

func validateXMLSyntax(tmpl string) string {
	tmpl = strings.TrimSpace(tmpl)
	if tmpl == "" {
		return ""
	}
	sub := carrier.InjectVariables(tmpl, sampleTemplateVars())
	dec := xml.NewDecoder(strings.NewReader(sub))
	dec.Strict = false
	for {
		_, err := dec.Token()
		if err == io.EOF {
			return ""
		}
		if err != nil {
			return fmt.Sprintf(
				"%s. Use well-formed XML with a single root element, e.g. <submit><to>{{to}}</to><text>{{message}}</text></submit>.",
				err.Error(),
			)
		}
	}
}

func validateFormURLEncodedSyntax(tmpl string) string {
	tmpl = strings.TrimSpace(tmpl)
	if tmpl == "" {
		return ""
	}
	parts := strings.Split(tmpl, "&")
	for i, part := range parts {
		if part == "" && i == len(parts)-1 {
			continue
		}
		if part == "" {
			return "empty segment between && — use key=value pairs joined with &, e.g. to={{to}}&text={{message}}."
		}
		eq := strings.Index(part, "=")
		if eq < 0 {
			return fmt.Sprintf("segment %q must be key=value (missing =).", part)
		}
		if eq == 0 {
			return fmt.Sprintf("segment %q must have a non-empty key before =.", part)
		}
	}
	return ""
}

func validateTemplateSyntax(contentType, tmpl string) string {
	switch contentType {
	case "application/json":
		return validateJSONSyntax(tmpl)
	case "text/xml", "application/xml":
		return validateXMLSyntax(tmpl)
	case "application/x-www-form-urlencoded":
		return validateFormURLEncodedSyntax(tmpl)
	default:
		return ""
	}
}

func validateTemplateVarsIn(tmpl, fieldKey string, m map[string]string) {
	for _, match := range reTemplateVar.FindAllStringSubmatch(tmpl, -1) {
		if len(match) < 2 {
			continue
		}
		if _, ok := templateAllowed[match[1]]; !ok {
			m[fieldKey] = fmt.Sprintf("Unknown variable {{%s}}. Use the Insert variable list.", match[1])
			return
		}
	}
}

// resolveGETBodyTemplateForSave keeps a stored GET-carrier body when the form submits an empty body.
func resolveGETBodyTemplateForSave(httpMethod, submittedBody, storedBody string) string {
	if strings.ToUpper(strings.TrimSpace(httpMethod)) == "GET" && strings.TrimSpace(submittedBody) == "" {
		return storedBody
	}
	return submittedBody
}

// resolvePOSTQueryTemplateForSave keeps a stored query when POST hides the query editor (empty submit).
func resolvePOSTQueryTemplateForSave(httpMethod, submittedQuery, storedQuery string) string {
	if strings.ToUpper(strings.TrimSpace(httpMethod)) == "POST" && strings.TrimSpace(submittedQuery) == "" {
		return storedQuery
	}
	return submittedQuery
}

func validateRequestTemplate(httpMethod, contentType, body, query string) map[string]string {
	m := map[string]string{}
	allowedCT := map[string]struct{}{
		"application/json":                  {},
		"application/x-www-form-urlencoded": {},
		"text/xml":                          {},
		"application/xml":                   {},
	}
	if _, ok := allowedCT[contentType]; !ok {
		m["content_type"] = "Invalid content type"
		return m
	}
	httpMethod = strings.ToUpper(strings.TrimSpace(httpMethod))

	validateTemplateVarsIn(body, "body_template", m)
	if httpMethod == "GET" {
		validateTemplateVarsIn(query, "query_template", m)
	}

	formatLabel := map[string]string{
		"application/json":                  "JSON",
		"application/x-www-form-urlencoded": "form-urlencoded (key=value&…)",
		"text/xml":                          "XML",
		"application/xml":                   "XML",
	}[contentType]

	if httpMethod != "GET" && strings.TrimSpace(body) != "" {
		if msg := validateTemplateSyntax(contentType, body); msg != "" {
			m["body_template"] = fmt.Sprintf("%s body: %s", formatLabel, msg)
		}
	}
	// Query string is always key=value&… regardless of body Content-Type.
	if httpMethod == "GET" && strings.TrimSpace(query) != "" {
		if msg := validateFormURLEncodedSyntax(query); msg != "" {
			m["query_template"] = fmt.Sprintf("Query string: %s", msg)
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
