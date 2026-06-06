// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package db

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// LedgerEntryRow is one row in the carrier ledger.
type LedgerEntryRow struct {
	EntryID          string
	EntryType        string
	Amount           string
	Direction        int16
	BalanceAfter     string
	Currency         string
	PaymentReference *string
	InvoiceNumber    *string
	PaymentDate      *string
	Notes            *string
	CreatedAt        string
}

// ListLedgerEntries returns last 100 for carrier, newest first.
func ListLedgerEntries(ctx context.Context, pool *pgxpool.Pool, carrierID string) ([]LedgerEntryRow, error) {
	rows, err := pool.Query(ctx, `
		SELECT entry_id::text, entry_type, amount::text, direction, balance_after::text, currency::text,
			payment_reference, invoice_number,
			CASE WHEN payment_date IS NULL THEN NULL ELSE payment_date::text END,
			notes, to_char(created_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"')
		FROM carrier_balance_entries
		WHERE carrier_id = $1::uuid
		ORDER BY created_at DESC
		LIMIT 100`, carrierID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LedgerEntryRow
	for rows.Next() {
		var r LedgerEntryRow
		if e := rows.Scan(
			&r.EntryID, &r.EntryType, &r.Amount, &r.Direction, &r.BalanceAfter, &r.Currency,
			&r.PaymentReference, &r.InvoiceNumber, &r.PaymentDate, &r.Notes, &r.CreatedAt,
		); e != nil {
			return nil, e
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

type paymentRecorder interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// RecordPayment calls record_carrier_payment().
func RecordPayment(
	ctx context.Context, q paymentRecorder, carrierID string, amount, currency string,
	paymentRef, invoice *string, paymentDate time.Time, notes *string,
) (string, error) {
	var out string
	err := q.QueryRow(ctx, `SELECT record_carrier_payment($1::uuid, $2::numeric(18,6), $3::char(3), $4, $5, $6::date, $7)::text`,
		carrierID, amount, currency, paymentRef, invoice, paymentDate.Format("2006-01-02"), notes,
	).Scan(&out)
	return out, err
}

// UsageTotals from carrier_usage_totals, or zero values if no row.
type UsageTotals struct {
	TotalMessages  int64
	TotalSegments  int64
	TotalAmount    string
	LastMessageAt  *string
	UpdatedAt      *string
}

// GetUsageTotals fetches the row.
func GetUsageTotals(ctx context.Context, pool *pgxpool.Pool, carrierID string) (UsageTotals, error) {
	var u UsageTotals
	var lastAt, upAt *time.Time
	var ta *string
	err := pool.QueryRow(ctx, `
		SELECT total_messages, total_segments, total_amount::text, last_message_at, updated_at
		FROM carrier_usage_totals
		WHERE carrier_id = $1::uuid`, carrierID,
	).Scan(&u.TotalMessages, &u.TotalSegments, &ta, &lastAt, &upAt)
	if err == pgx.ErrNoRows {
		u.TotalAmount = "0.000000"
		return u, nil
	}
	if err != nil {
		return UsageTotals{}, err
	}
	if ta != nil {
		u.TotalAmount = *ta
	} else {
		u.TotalAmount = "0.000000"
	}
	if lastAt != nil {
		s := lastAt.UTC().Format(time.RFC3339)
		u.LastMessageAt = &s
	}
	if upAt != nil {
		s := upAt.UTC().Format(time.RFC3339)
		u.UpdatedAt = &s
	}
	return u, nil
}

// GetCarrier30DayChargeSum is spend on charges in last 30 days.
func GetCarrier30DayChargeSum(ctx context.Context, pool *pgxpool.Pool, carrierID string) (string, error) {
	var s string
	err := pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(amount) FILTER (WHERE entry_type = 'charge'), 0)::text
		FROM carrier_balance_entries
		WHERE carrier_id = $1::uuid
		  AND created_at >= (now() AT TIME ZONE 'utc' - interval '30 days')`, carrierID,
	).Scan(&s)
	return s, err
}
