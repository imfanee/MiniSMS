// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package carrier

import (
	"net/url"
	"strings"
)

// InjectVariables substitutes {{placeholders}} into a body template verbatim (no encoding).
// Use it for JSON/XML/form bodies where the template author controls escaping.
func InjectVariables(tmpl string, vars map[string]string) string {
	if tmpl == "" {
		return ""
	}
	r := strings.NewReplacer(
		"{{to}}", vars["to"],
		"{{from}}", vars["from"],
		"{{message}}", vars["message"],
		"{{message_id}}", vars["message_id"],
		"{{timestamp}}", vars["timestamp"],
		"{{client_id}}", vars["client_id"],
		"{{dlr_callback_url}}", vars["dlr_callback_url"],
		"{{dlr_callback_url_encoded}}", vars["dlr_callback_url_encoded"],
		"{{source_addr_ton}}", vars["source_addr_ton"],
		"{{source_addr_npi}}", vars["source_addr_npi"],
		"{{dest_addr_ton}}", vars["dest_addr_ton"],
		"{{dest_addr_npi}}", vars["dest_addr_npi"],
	)
	return r.Replace(tmpl)
}

// InjectQueryVariables substitutes {{placeholders}} into a URL query template, URL-encoding
// each substituted value so reserved characters (+, &, =, space, /) in from/to/text/etc.
// survive to the gateway intact. A literal "+" in a sender or recipient becomes "%2B" rather
// than decoding to a space at the carrier. The pre-encoded {{dlr_callback_url_encoded}} value
// is passed through unchanged to avoid double-encoding the callback URL.
func InjectQueryVariables(tmpl string, vars map[string]string) string {
	if tmpl == "" {
		return ""
	}
	pairs := make([]string, 0, len(vars)*2)
	for k, v := range vars {
		val := v
		if k != "dlr_callback_url_encoded" {
			val = url.QueryEscape(v)
		}
		pairs = append(pairs, "{{"+k+"}}", val)
	}
	return strings.NewReplacer(pairs...).Replace(tmpl)
}
