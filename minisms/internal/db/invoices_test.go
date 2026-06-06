// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package db

import "testing"

func TestInvoiceInitialStatus(t *testing.T) {
	tests := []struct {
		total          string
		wantStatus     string
		wantPending    string
	}{
		{"0", InvoiceStatusPaid, "0.000000"},
		{"0.000000", InvoiceStatusPaid, "0.000000"},
		{"0.000001", InvoiceStatusPending, "0.000001"},
		{"12.500000", InvoiceStatusPending, "12.500000"},
	}
	for _, tc := range tests {
		status, pending := invoiceInitialStatus(tc.total)
		if status != tc.wantStatus || pending != tc.wantPending {
			t.Fatalf("total %q: got status=%s pending=%s, want status=%s pending=%s",
				tc.total, status, pending, tc.wantStatus, tc.wantPending)
		}
	}
}
