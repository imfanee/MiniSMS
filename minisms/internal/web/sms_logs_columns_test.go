// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package web

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func keysOf(cols []smsLogColumn) []string {
	out := make([]string, len(cols))
	for i, c := range cols {
		out[i] = c.Key
	}
	return out
}

func TestSelectedSMSLogColumns(t *testing.T) {
	allKeys := keysOf(smsLogColumns)

	cases := []struct {
		name string
		cols string
		want []string
	}{
		{"absent returns all", "", allKeys},
		{"subset preserved", "to,status", []string{"to", "status"}},
		{"honors request order (reordering)", "status,to", []string{"status", "to"}},
		{"custom order across many keys", "charged,to,received", []string{"charged", "to", "received"}},
		{"duplicates collapse to first position", "to,to,status", []string{"to", "status"}},
		{"unknown keys dropped", "to,bogus,charged", []string{"to", "charged"}},
		{"all-unknown falls back to full set", "nope,zzz", allKeys},
		{"blank entries tolerated", "to,,,status,", []string{"to", "status"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			url := "/admin/sms-logs/export.csv"
			if tc.cols != "" {
				url += "?cols=" + tc.cols
			}
			r := httptest.NewRequest("GET", url, nil)
			got := keysOf(selectedSMSLogColumns(r))
			if strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Fatalf("cols=%q: got %v want %v", tc.cols, got, tc.want)
			}
		})
	}
}

// TestSMSLogColumnsMatchDefaultCSVHeader guards the backward-compatibility promise: with no cols
// parameter the export must reproduce the historical CSV header order exactly.
func TestSMSLogColumnsMatchDefaultCSVHeader(t *testing.T) {
	want := "received_at,message_id,client,to,from,segments,total_charged,currency,carrier,failover_sequence,status"
	var heads []string
	for _, c := range smsLogColumns {
		heads = append(heads, c.CSVHead)
	}
	if got := strings.Join(heads, ","); got != want {
		t.Fatalf("default CSV header drifted:\n got  %s\n want %s", got, want)
	}
}
