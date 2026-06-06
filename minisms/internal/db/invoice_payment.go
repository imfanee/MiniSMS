// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package db

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type OpenInvoiceOption struct {
	InvoiceID     string
	InvoiceNumber string
	PendingAmount string
	InvoiceDate   string
}

// ListOpenInvoices returns invoices with outstanding balance for payment allocation.
func ListOpenInvoices(ctx context.Context, pool *pgxpool.Pool, entityType, entityID string) ([]OpenInvoiceOption, error) {
	rows, err := pool.Query(ctx, `
		SELECT invoice_id::text, invoice_number, pending_amount::text, invoice_date::text
		FROM invoices
		WHERE entity_type = $1 AND entity_id = $2::uuid AND pending_amount > 0
		ORDER BY invoice_date DESC, created_at DESC`,
		entityType, entityID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []OpenInvoiceOption
	for rows.Next() {
		var o OpenInvoiceOption
		if e := rows.Scan(&o.InvoiceID, &o.InvoiceNumber, &o.PendingAmount, &o.InvoiceDate); e != nil {
			return nil, e
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

type querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// ApplyInvoicePayment subtracts payment from invoice pending_amount and updates status.
func ApplyInvoicePayment(ctx context.Context, q querier, entityType, entityID, invoiceID, paymentAmount string) (*Invoice, error) {
	var inv Invoice
	err := q.QueryRow(ctx, `
		UPDATE invoices SET
			pending_amount = GREATEST(0, pending_amount - $4::numeric(18,6)),
			status = CASE
				WHEN pending_amount - $4::numeric(18,6) <= 0 THEN 'paid'
				WHEN pending_amount - $4::numeric(18,6) < total_amount THEN 'partially_paid'
				ELSE 'pending'
			END,
			updated_at = now()
		WHERE invoice_id = $1::uuid
		  AND entity_type = $2
		  AND entity_id = $3::uuid
		  AND pending_amount > 0
		RETURNING invoice_id::text, entity_type, entity_id::text, invoice_number,
			invoice_date, from_date, to_date, total_records,
			total_amount::text, pending_amount::text, status, currency, pdf_path, created_at`,
		invoiceID, entityType, entityID, paymentAmount,
	).Scan(
		&inv.InvoiceID, &inv.EntityType, &inv.EntityID, &inv.InvoiceNumber,
		&inv.InvoiceDate, &inv.FromDate, &inv.ToDate, &inv.TotalRecords,
		&inv.TotalAmount, &inv.PendingAmount, &inv.Status, &inv.Currency, &inv.PDFPath, &inv.CreatedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("invoice not found or already paid")
	}
	if err != nil {
		return nil, err
	}
	return &inv, nil
}
