// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package carrier

import "strings"

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
