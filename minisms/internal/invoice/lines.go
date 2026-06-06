// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package invoice

import (
	"context"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Line struct {
	ReceivedAt time.Time
	ToNumber   string
	FromNumber string
	Segments   int
	Cost       string
}

type Summary struct {
	Lines        []Line
	TotalRecords int
	TotalAmount  string
	Currency     string
}

func LoadClientLines(ctx context.Context, pool *pgxpool.Pool, clientID string, from, to time.Time) (*Summary, error) {
	rows, err := pool.Query(ctx, `
		SELECT sl.received_at, sl.to_number, COALESCE(sl.from_number, ''), sl.segments, sl.total_charged::text, sl.currency::text
		FROM sms_logs sl
		WHERE sl.client_id = $1::uuid
		  AND sl.received_at >= $2::date
		  AND sl.received_at < ($3::date + interval '1 day')
		  AND sl.status NOT IN ('pending', 'rejected')
		ORDER BY sl.received_at ASC`,
		clientID, from.Format("2006-01-02"), to.Format("2006-01-02"),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanLines(rows)
}

func LoadCarrierLines(ctx context.Context, pool *pgxpool.Pool, carrierID string, from, to time.Time) (*Summary, error) {
	rows, err := pool.Query(ctx, `
		SELECT sl.received_at, sl.to_number, COALESCE(sl.from_number, ''), sl.segments,
			COALESCE(cbe.amount, 0)::text, COALESCE(c.currency, 'USD')::text
		FROM sms_logs sl
		JOIN carriers c ON c.carrier_id = sl.carrier_id
		LEFT JOIN carrier_balance_entries cbe
			ON cbe.message_id = sl.message_id AND cbe.entry_type = 'charge'
		WHERE sl.carrier_id = $1::uuid
		  AND sl.received_at >= $2::date
		  AND sl.received_at < ($3::date + interval '1 day')
		  AND sl.status NOT IN ('pending', 'rejected')
		ORDER BY sl.received_at ASC`,
		carrierID, from.Format("2006-01-02"), to.Format("2006-01-02"),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanLines(rows)
}

type rowScanner interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}

func scanLines(rows rowScanner) (*Summary, error) {
	var out Summary
	var total float64
	for rows.Next() {
		var ln Line
		if err := rows.Scan(&ln.ReceivedAt, &ln.ToNumber, &ln.FromNumber, &ln.Segments, &ln.Cost, &out.Currency); err != nil {
			return nil, err
		}
		out.Lines = append(out.Lines, ln)
		if v, err := strconv.ParseFloat(ln.Cost, 64); err == nil {
			total += v
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out.TotalRecords = len(out.Lines)
	out.TotalAmount = formatAmount(total)
	if out.Currency == "" {
		out.Currency = "USD"
	}
	return &out, nil
}

func formatAmount(v float64) string {
	return strconv.FormatFloat(v, 'f', 6, 64)
}
