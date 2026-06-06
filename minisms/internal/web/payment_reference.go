// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package web

import (
	"context"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/minisms/minisms/internal/db"
)

type parsedPaymentReference struct {
	RefType    string
	PaymentRef *string
	InvoiceID  string
}

func parsePaymentReference(r *http.Request, errs map[string]string) parsedPaymentReference {
	refType := strings.ToLower(strings.TrimSpace(r.FormValue("reference_type")))
	if refType == "" {
		refType = "other"
	}
	out := parsedPaymentReference{RefType: refType}
	switch refType {
	case "invoice":
		invoiceID := strings.TrimSpace(r.FormValue("invoice_id"))
		if invoiceID == "" {
			errs["invoice_id"] = "Select an invoice"
		} else {
			out.InvoiceID = invoiceID
		}
	case "other":
		ref := strings.TrimSpace(r.FormValue("payment_reference"))
		if ref == "" {
			errs["payment_reference"] = "Reference details required"
		} else {
			out.PaymentRef = &ref
		}
	default:
		errs["reference_type"] = "Invalid reference type"
	}
	return out
}

// resolvePaymentFields applies invoice allocation when needed and returns ledger reference fields.
func resolvePaymentFields(
	ctx context.Context,
	tx pgx.Tx,
	entityType, entityID, amount string,
	parsed parsedPaymentReference,
	errs map[string]string,
) (paymentRef, invoiceNumber *string, clientReference string, err error) {
	if parsed.RefType != "invoice" {
		if parsed.PaymentRef != nil {
			return parsed.PaymentRef, nil, *parsed.PaymentRef, nil
		}
		return nil, nil, "", nil
	}
	if parsed.InvoiceID == "" {
		return nil, nil, "", nil
	}
	inv, e := db.ApplyInvoicePayment(ctx, tx, entityType, entityID, parsed.InvoiceID, amount)
	if e != nil {
		if strings.Contains(e.Error(), "not found or already paid") {
			errs["invoice_id"] = "Invoice not found or already paid"
			return nil, nil, "", nil
		}
		return nil, nil, "", e
	}
	label := "Invoice"
	return &label, &inv.InvoiceNumber, inv.InvoiceNumber, nil
}
