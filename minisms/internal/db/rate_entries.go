package db

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type RateEntry struct {
	RateEntryID   string
	RateGroupID   string
	Prefix        string
	Description   *string
	RatePerSMS    string
	EffectiveFrom string
	EffectiveTo   *string
}

type UpsertRateEntryParams struct {
	Prefix        string
	Description   *string
	RatePerSMS    string
	EffectiveFrom time.Time
	EffectiveTo   *time.Time
}

var ErrDuplicateRateEntry = errors.New("duplicate rate entry")

func ListEntries(ctx context.Context, pool *pgxpool.Pool, rateGroupID string) ([]RateEntry, error) {
	rows, err := pool.Query(ctx, `
		SELECT rate_entry_id::text, rate_group_id::text, prefix, description, rate_per_sms::text,
			effective_from::text, CASE WHEN effective_to IS NULL THEN NULL ELSE effective_to::text END
		FROM rate_entries
		WHERE rate_group_id = $1::uuid
		ORDER BY
			CASE WHEN prefix = '*' THEN 1 ELSE 0 END ASC,
			CASE WHEN prefix = '*' THEN 0 ELSE char_length(prefix) END DESC,
			prefix ASC,
			effective_from DESC`, rateGroupID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RateEntry
	for rows.Next() {
		var e RateEntry
		if x := rows.Scan(&e.RateEntryID, &e.RateGroupID, &e.Prefix, &e.Description, &e.RatePerSMS, &e.EffectiveFrom, &e.EffectiveTo); x != nil {
			return nil, x
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func GetEntry(ctx context.Context, pool *pgxpool.Pool, rateGroupID, entryID string) (*RateEntry, error) {
	var e RateEntry
	err := pool.QueryRow(ctx, `
		SELECT rate_entry_id::text, rate_group_id::text, prefix, description, rate_per_sms::text,
			effective_from::text, CASE WHEN effective_to IS NULL THEN NULL ELSE effective_to::text END
		FROM rate_entries
		WHERE rate_group_id = $1::uuid AND rate_entry_id = $2::uuid`, rateGroupID, entryID,
	).Scan(&e.RateEntryID, &e.RateGroupID, &e.Prefix, &e.Description, &e.RatePerSMS, &e.EffectiveFrom, &e.EffectiveTo)
	if err != nil {
		return nil, err
	}
	return &e, nil
}

func ExistsEntryKey(ctx context.Context, pool *pgxpool.Pool, rateGroupID, prefix string, from time.Time, excludeEntryID *string) (bool, error) {
	var n int
	if excludeEntryID != nil && *excludeEntryID != "" {
		err := pool.QueryRow(ctx, `
			SELECT 1
			FROM rate_entries
			WHERE rate_group_id = $1::uuid AND prefix = $2 AND effective_from = $3::date
			  AND rate_entry_id <> $4::uuid
			LIMIT 1`, rateGroupID, prefix, from.Format("2006-01-02"), *excludeEntryID).Scan(&n)
		if err == pgx.ErrNoRows {
			return false, nil
		}
		return err == nil, err
	}
	err := pool.QueryRow(ctx, `
		SELECT 1
		FROM rate_entries
		WHERE rate_group_id = $1::uuid AND prefix = $2 AND effective_from = $3::date
		LIMIT 1`, rateGroupID, prefix, from.Format("2006-01-02")).Scan(&n)
	if err == pgx.ErrNoRows {
		return false, nil
	}
	return err == nil, err
}

func CreateEntry(ctx context.Context, pool *pgxpool.Pool, rateGroupID string, p UpsertRateEntryParams) (string, error) {
	var id string
	err := pool.QueryRow(ctx, `
		INSERT INTO rate_entries (rate_group_id, prefix, description, rate_per_sms, effective_from, effective_to)
		VALUES ($1::uuid, $2, $3, $4::numeric(18,6), $5::date, $6::date)
		RETURNING rate_entry_id::text`,
		rateGroupID, p.Prefix, p.Description, p.RatePerSMS, p.EffectiveFrom.Format("2006-01-02"), nullableDate(p.EffectiveTo),
	).Scan(&id)
	if err == nil {
		return id, nil
	}
	var pe *pgconn.PgError
	if errors.As(err, &pe) && pe.Code == "23505" {
		return "", ErrDuplicateRateEntry
	}
	return "", err
}

func UpdateEntry(ctx context.Context, pool *pgxpool.Pool, rateGroupID, entryID string, p UpsertRateEntryParams) error {
	ct, err := pool.Exec(ctx, `
		UPDATE rate_entries
		SET prefix = $1, description = $2, rate_per_sms = $3::numeric(18,6), effective_from = $4::date, effective_to = $5::date, updated_at = now()
		WHERE rate_group_id = $6::uuid AND rate_entry_id = $7::uuid`,
		p.Prefix, p.Description, p.RatePerSMS, p.EffectiveFrom.Format("2006-01-02"), nullableDate(p.EffectiveTo), rateGroupID, entryID,
	)
	if err != nil {
		var pe *pgconn.PgError
		if errors.As(err, &pe) && pe.Code == "23505" {
			return ErrDuplicateRateEntry
		}
		return err
	}
	if ct.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

func DeleteEntry(ctx context.Context, pool *pgxpool.Pool, rateGroupID, entryID string) (bool, error) {
	ct, err := pool.Exec(ctx, `DELETE FROM rate_entries WHERE rate_group_id = $1::uuid AND rate_entry_id = $2::uuid`, rateGroupID, entryID)
	if err != nil {
		return false, err
	}
	return ct.RowsAffected() > 0, nil
}

func CountCatchAllEntries(ctx context.Context, pool *pgxpool.Pool, rateGroupID string) (int64, error) {
	var n int64
	err := pool.QueryRow(ctx, `SELECT COUNT(*)::bigint FROM rate_entries WHERE rate_group_id = $1::uuid AND prefix = '*'`, rateGroupID).Scan(&n)
	return n, err
}

func nullableDate(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.Format("2006-01-02")
}
