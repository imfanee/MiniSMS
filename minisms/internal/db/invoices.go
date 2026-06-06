// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package db

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	InvoiceEntityClient  = "client"
	InvoiceEntityCarrier = "carrier"

	InvoiceStatusPending        = "pending"
	InvoiceStatusPaid           = "paid"
	InvoiceStatusPartiallyPaid  = "partially_paid"
)

type Invoice struct {
	InvoiceID     string
	EntityType    string
	EntityID      string
	InvoiceNumber string
	InvoiceDate   time.Time
	FromDate      time.Time
	ToDate        time.Time
	TotalRecords  int
	TotalAmount   string
	PendingAmount string
	Status        string
	Currency      string
	PDFPath       string
	CreatedAt     time.Time
}

type CreateInvoiceParams struct {
	InvoiceNumber string
	EntityType    string
	EntityID      string
	FromDate      time.Time
	ToDate        time.Time
	TotalRecords  int
	TotalAmount   string
	Currency      string
	PDFPath       string
}

func NextInvoiceNumber(ctx context.Context, pool *pgxpool.Pool) (string, error) {
	var n int64
	err := pool.QueryRow(ctx, `SELECT nextval('invoice_number_seq')`).Scan(&n)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("INV-%s-%05d", time.Now().UTC().Format("20060102"), n), nil
}

func invoiceInitialStatus(totalAmount string) (status, pendingAmount string) {
	f, err := strconv.ParseFloat(strings.TrimSpace(totalAmount), 64)
	if err == nil && f <= 0 {
		return InvoiceStatusPaid, "0.000000"
	}
	return InvoiceStatusPending, totalAmount
}

func CreateInvoice(ctx context.Context, pool *pgxpool.Pool, p CreateInvoiceParams) (*Invoice, error) {
	num := strings.TrimSpace(p.InvoiceNumber)
	if num == "" {
		var err error
		num, err = NextInvoiceNumber(ctx, pool)
		if err != nil {
			return nil, err
		}
	}
	status, pending := invoiceInitialStatus(p.TotalAmount)
	var inv Invoice
	err := pool.QueryRow(ctx, `
		INSERT INTO invoices (
			entity_type, entity_id, invoice_number, invoice_date, from_date, to_date,
			total_records, total_amount, pending_amount, status, currency, pdf_path
		) VALUES (
			$1, $2::uuid, $3, CURRENT_DATE, $4::date, $5::date,
			$6, $7::numeric(18,6), $8::numeric(18,6), $9, $10, $11
		)
		RETURNING invoice_id::text, entity_type, entity_id::text, invoice_number,
			invoice_date, from_date, to_date, total_records,
			total_amount::text, pending_amount::text, status, currency, pdf_path, created_at`,
		p.EntityType, p.EntityID, num, p.FromDate.Format("2006-01-02"), p.ToDate.Format("2006-01-02"),
		p.TotalRecords, p.TotalAmount, pending, status, p.Currency, p.PDFPath,
	).Scan(
		&inv.InvoiceID, &inv.EntityType, &inv.EntityID, &inv.InvoiceNumber,
		&inv.InvoiceDate, &inv.FromDate, &inv.ToDate, &inv.TotalRecords,
		&inv.TotalAmount, &inv.PendingAmount, &inv.Status, &inv.Currency, &inv.PDFPath, &inv.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &inv, nil
}

func GetInvoice(ctx context.Context, pool *pgxpool.Pool, entityType, entityID, invoiceID string) (*Invoice, error) {
	var inv Invoice
	err := pool.QueryRow(ctx, `
		SELECT invoice_id::text, entity_type, entity_id::text, invoice_number,
			invoice_date, from_date, to_date, total_records,
			total_amount::text, pending_amount::text, status, currency, pdf_path, created_at
		FROM invoices
		WHERE invoice_id = $1::uuid AND entity_type = $2 AND entity_id = $3::uuid`,
		invoiceID, entityType, entityID,
	).Scan(
		&inv.InvoiceID, &inv.EntityType, &inv.EntityID, &inv.InvoiceNumber,
		&inv.InvoiceDate, &inv.FromDate, &inv.ToDate, &inv.TotalRecords,
		&inv.TotalAmount, &inv.PendingAmount, &inv.Status, &inv.Currency, &inv.PDFPath, &inv.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &inv, nil
}

type InvoiceStats struct {
	TotalCount   int
	PendingCount int
	UnpaidAmount string
	Currency     string
}

func GetInvoiceStats(ctx context.Context, pool *pgxpool.Pool, entityType, entityID string) (InvoiceStats, error) {
	var s InvoiceStats
	err := pool.QueryRow(ctx, `
		SELECT
			COUNT(*)::int,
			COUNT(*) FILTER (WHERE status IN ($3, $4))::int,
			COALESCE(SUM(pending_amount) FILTER (WHERE status IN ($3, $4)), 0)::text,
			COALESCE(MAX(currency), '')
		FROM invoices
		WHERE entity_type = $1 AND entity_id = $2::uuid`,
		entityType, entityID, InvoiceStatusPending, InvoiceStatusPartiallyPaid,
	).Scan(&s.TotalCount, &s.PendingCount, &s.UnpaidAmount, &s.Currency)
	return s, err
}

func ListInvoices(ctx context.Context, pool *pgxpool.Pool, entityType, entityID string, page, pageSize int) ([]Invoice, int, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	var total int
	err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM invoices WHERE entity_type = $1 AND entity_id = $2::uuid`,
		entityType, entityID,
	).Scan(&total)
	if err != nil {
		return nil, 0, err
	}
	rows, err := pool.Query(ctx, `
		SELECT invoice_id::text, entity_type, entity_id::text, invoice_number,
			invoice_date, from_date, to_date, total_records,
			total_amount::text, pending_amount::text, status, currency, pdf_path, created_at
		FROM invoices
		WHERE entity_type = $1 AND entity_id = $2::uuid
		ORDER BY invoice_date DESC, created_at DESC
		LIMIT $3 OFFSET $4`,
		entityType, entityID, pageSize, (page-1)*pageSize,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []Invoice
	for rows.Next() {
		var inv Invoice
		if e := rows.Scan(
			&inv.InvoiceID, &inv.EntityType, &inv.EntityID, &inv.InvoiceNumber,
			&inv.InvoiceDate, &inv.FromDate, &inv.ToDate, &inv.TotalRecords,
			&inv.TotalAmount, &inv.PendingAmount, &inv.Status, &inv.Currency, &inv.PDFPath, &inv.CreatedAt,
		); e != nil {
			return nil, 0, e
		}
		out = append(out, inv)
	}
	return out, total, rows.Err()
}

func InvoiceStatusLabel(status string) string {
	switch status {
	case InvoiceStatusPaid:
		return "Paid"
	case InvoiceStatusPartiallyPaid:
		return "Partially Paid"
	default:
		return "Pending"
	}
}

func IsNotFoundInvoice(err error) bool {
	return err == pgx.ErrNoRows
}
