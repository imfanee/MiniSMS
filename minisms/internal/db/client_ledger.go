// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package db

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type ClientLedgerEntry struct {
	EntryID      string
	EntryType    string
	Amount       string
	BalanceAfter string
	Currency     string
	Reference    *string
	MessageID    *string
	Notes        *string
	CreatedAt    string
}

func ListClientLedger(ctx context.Context, pool *pgxpool.Pool, clientID string) ([]ClientLedgerEntry, error) {
	rows, err := pool.Query(ctx, `
		SELECT entry_id::text, entry_type, amount::text, balance_after::text, currency::text, reference, message_id::text, notes,
			to_char(created_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"')
		FROM ledger_entries
		WHERE client_id = $1::uuid
		ORDER BY created_at DESC
		LIMIT 200`, clientID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ClientLedgerEntry
	for rows.Next() {
		var x ClientLedgerEntry
		if e := rows.Scan(&x.EntryID, &x.EntryType, &x.Amount, &x.BalanceAfter, &x.Currency, &x.Reference, &x.MessageID, &x.Notes, &x.CreatedAt); e != nil {
			return nil, e
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

type creditRecorder interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func CreditClientBalance(ctx context.Context, q creditRecorder, clientID, amount, currency, reference string, notes *string) (string, error) {
	var out string
	err := q.QueryRow(ctx, `SELECT credit_client_balance($1::uuid,$2::numeric(18,6),$3::char(3),$4,$5)::text`,
		clientID, amount, currency, reference, notes,
	).Scan(&out)
	return out, err
}
