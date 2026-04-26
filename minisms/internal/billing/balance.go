package billing

import (
	"context"

	"github.com/jackc/pgx/v5"
)

func DeductClientBalance(ctx context.Context, tx pgx.Tx, clientID, amount, messageID, currency string) (string, error) {
	var remaining string
	err := tx.QueryRow(ctx, `SELECT deduct_client_balance($1::uuid, $2::numeric(18,6), $3::uuid, $4::char(3))::text`,
		clientID, amount, messageID, currency).Scan(&remaining)
	return remaining, err
}

func CreditClientBalance(ctx context.Context, tx pgx.Tx, clientID, amount, currency, reference, notes string) (string, error) {
	var remaining string
	err := tx.QueryRow(ctx, `SELECT credit_client_balance($1::uuid, $2::numeric(18,6), $3::char(3), $4, $5)::text`,
		clientID, amount, currency, reference, notes).Scan(&remaining)
	return remaining, err
}
