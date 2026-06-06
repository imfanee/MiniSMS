// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package db

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type RateGroupListRow struct {
	RateGroupID  string
	Name         string
	Currency     string
	Description  *string
	EntryCount   int64
	CarrierCount int64
	ClientCount  int64
}

type RateGroup struct {
	RateGroupID string
	Name        string
	Currency    string
	Description *string
	UpdatedAt   *string
}

type CreateRateGroupParams struct {
	Name        string
	Currency    string
	Description *string
}

type UpdateRateGroupParams = CreateRateGroupParams

var ErrDuplicateRateGroupName = errors.New("duplicate rate group name")

func ListRateGroups(ctx context.Context, pool *pgxpool.Pool) ([]RateGroupListRow, error) {
	rows, err := pool.Query(ctx, `
		SELECT rg.rate_group_id::text, rg.name, rg.currency::text, rg.description,
			COUNT(DISTINCT re.rate_entry_id)::bigint AS entry_count,
			COUNT(DISTINCT c.carrier_id)::bigint AS carrier_count
		FROM rate_groups rg
		LEFT JOIN rate_entries re ON re.rate_group_id = rg.rate_group_id
		LEFT JOIN carriers c ON c.rate_group_id = rg.rate_group_id
		GROUP BY rg.rate_group_id, rg.name, rg.currency, rg.description
		ORDER BY rg.name ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RateGroupListRow
	for rows.Next() {
		var r RateGroupListRow
		if e := rows.Scan(&r.RateGroupID, &r.Name, &r.Currency, &r.Description, &r.EntryCount, &r.CarrierCount); e != nil {
			return nil, e
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		n, e := countRateGroupClients(ctx, pool, out[i].RateGroupID)
		if e != nil {
			return nil, e
		}
		out[i].ClientCount = n
	}
	return out, nil
}

func GetRateGroup(ctx context.Context, pool *pgxpool.Pool, id string) (*RateGroup, error) {
	var g RateGroup
	var desc *string
	var up *time.Time
	err := pool.QueryRow(ctx, `
		SELECT rate_group_id::text, name, currency::text, description, updated_at
		FROM rate_groups
		WHERE rate_group_id = $1::uuid`, id,
	).Scan(&g.RateGroupID, &g.Name, &g.Currency, &desc, &up)
	if err != nil {
		return nil, err
	}
	g.Description = desc
	if up != nil {
		s := up.UTC().Format(time.RFC3339)
		g.UpdatedAt = &s
	}
	return &g, nil
}

func CreateRateGroup(ctx context.Context, pool *pgxpool.Pool, p CreateRateGroupParams) (string, error) {
	var id string
	err := pool.QueryRow(ctx, `
		INSERT INTO rate_groups (name, currency, description)
		VALUES ($1, $2, $3)
		RETURNING rate_group_id::text`,
		p.Name, p.Currency, p.Description,
	).Scan(&id)
	if err == nil {
		return id, nil
	}
	var pe *pgconn.PgError
	if errors.As(err, &pe) && pe.Code == "23505" {
		return "", ErrDuplicateRateGroupName
	}
	return "", err
}

func UpdateRateGroup(ctx context.Context, pool *pgxpool.Pool, id string, p UpdateRateGroupParams) error {
	ct, err := pool.Exec(ctx, `
		UPDATE rate_groups
		SET name = $1, currency = $2, description = $3, updated_at = now()
		WHERE rate_group_id = $4::uuid`,
		p.Name, p.Currency, p.Description, id,
	)
	if err != nil {
		var pe *pgconn.PgError
		if errors.As(err, &pe) && pe.Code == "23505" {
			return ErrDuplicateRateGroupName
		}
		return err
	}
	if ct.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

func DeleteRateGroup(ctx context.Context, pool *pgxpool.Pool, id string) (bool, error) {
	ct, err := pool.Exec(ctx, `DELETE FROM rate_groups WHERE rate_group_id = $1::uuid`, id)
	if err != nil {
		return false, err
	}
	return ct.RowsAffected() > 0, nil
}

func CountRateGroupCarrierRefs(ctx context.Context, pool *pgxpool.Pool, id string) (int64, error) {
	var n int64
	err := pool.QueryRow(ctx, `SELECT COUNT(*)::bigint FROM carriers WHERE rate_group_id = $1::uuid`, id).Scan(&n)
	return n, err
}

func CountRateGroupClientRefs(ctx context.Context, pool *pgxpool.Pool, id string) (int64, error) {
	return countRateGroupClients(ctx, pool, id)
}

func countRateGroupClients(ctx context.Context, pool *pgxpool.Pool, id string) (int64, error) {
	var n int64
	err := pool.QueryRow(ctx, `SELECT COUNT(*)::bigint FROM clients WHERE rate_group_id = $1::uuid`, id).Scan(&n)
	if err == nil {
		return n, nil
	}
	var pe *pgconn.PgError
	if errors.As(err, &pe) && pe.Code == "42P01" {
		return 0, nil
	}
	return 0, err
}
