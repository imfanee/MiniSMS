// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package carrier

import "testing"

func TestInjectVariables(t *testing.T) {
	vars := map[string]string{
		"to":               "+447700900123",
		"from":             "MiniSMS",
		"message":          "hello",
		"message_id":       "m-1",
		"timestamp":        "2026-01-01T00:00:00Z",
		"client_id":        "c-1",
		"dlr_callback_url":           "https://example.com/api/v1/dlr/m-1",
		"dlr_callback_url_encoded":   "https%3A%2F%2Fexample.com%2Fapi%2Fv1%2Fdlr%2Fm-1",
		"source_addr_ton":  "5",
		"source_addr_npi":  "0",
		"dest_addr_ton":    "1",
		"dest_addr_npi":    "1",
	}

	got := InjectVariables(`{"to":"{{to}}","from":"{{from}}","m":"{{message}}","id":"{{message_id}}","ts":"{{timestamp}}","c":"{{client_id}}","cb":"{{dlr_callback_url}}","s_ton":"{{source_addr_ton}}","s_npi":"{{source_addr_npi}}","d_ton":"{{dest_addr_ton}}","d_npi":"{{dest_addr_npi}}","x":"{{unknown}}"}`, vars)
	want := `{"to":"+447700900123","from":"MiniSMS","m":"hello","id":"m-1","ts":"2026-01-01T00:00:00Z","c":"c-1","cb":"https://example.com/api/v1/dlr/m-1","s_ton":"5","s_npi":"0","d_ton":"1","d_npi":"1","x":"{{unknown}}"}`
	if got != want {
		t.Fatalf("unexpected replace result\nwant: %s\ngot:  %s", want, got)
	}

	if InjectVariables("", vars) != "" {
		t.Fatalf("expected empty template to remain empty")
	}
}
