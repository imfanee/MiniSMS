// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package billing

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func LookupCarrierCost(ctx context.Context, pool *pgxpool.Pool, carrierID, destination, fallbackRate string) (string, error) {
	var rateGroupID *string
	err := pool.QueryRow(ctx, `SELECT rate_group_id::text FROM carriers WHERE carrier_id = $1::uuid`, carrierID).Scan(&rateGroupID)
	if err != nil {
		if err == pgx.ErrNoRows {
			return fallbackRate, nil
		}
		return "", err
	}
	if rateGroupID == nil || *rateGroupID == "" {
		return fallbackRate, nil
	}
	r, err := LookupRate(ctx, pool, *rateGroupID, destination)
	if err != nil {
		return fallbackRate, nil
	}
	return r.RatePerSMS, nil
}

func DeductCarrierBalance(ctx context.Context, tx pgx.Tx, carrierID, amount, currency, messageID string) (string, error) {
	var balance string
	err := tx.QueryRow(ctx, `SELECT deduct_carrier_balance($1::uuid, $2::numeric(18,6), $3::char(3), $4::uuid)::text`,
		carrierID, amount, currency, messageID).Scan(&balance)
	return balance, err
}

func IncrementUsage(ctx context.Context, tx pgx.Tx, carrierID string, segments int, amount string) error {
	_, err := tx.Exec(ctx, `SELECT increment_carrier_usage($1::uuid, $2, $3::numeric(18,6))`, carrierID, segments, amount)
	return err
}
