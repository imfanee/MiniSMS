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

type RoutingGroupListRow struct {
	RoutingGroupID      string
	Name                string
	Description         *string
	Status              string
	TotalRoutes         int64
	ActiveRoutes        int64
	HasFailoverCount    int64
	AssignedClientCount int64
}

type RoutingGroup struct {
	RoutingGroupID string
	Name           string
	Description    *string
	Status         string
	UpdatedAt      *string
}

type UpsertRoutingGroupParams struct {
	Name        string
	Description *string
	Status      string
}

var ErrDuplicateRoutingGroupName = errors.New("duplicate routing group name")

func ListRoutingGroups(ctx context.Context, pool *pgxpool.Pool) ([]RoutingGroupListRow, error) {
	rows, err := pool.Query(ctx, `
		SELECT rg.routing_group_id::text, rg.name, rg.description, rg.status,
			COALESCE(COUNT(re.route_entry_id),0)::bigint AS total_routes,
			COALESCE(SUM(CASE WHEN re.status = 'active' THEN 1 ELSE 0 END),0)::bigint AS active_routes,
			COALESCE(SUM(CASE WHEN re.failover1_carrier_id IS NOT NULL OR re.failover2_carrier_id IS NOT NULL THEN 1 ELSE 0 END),0)::bigint AS has_failover_count
		FROM routing_groups rg
		LEFT JOIN route_entries re ON re.routing_group_id = rg.routing_group_id
		GROUP BY rg.routing_group_id, rg.name, rg.description, rg.status
		ORDER BY rg.name ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RoutingGroupListRow
	for rows.Next() {
		var x RoutingGroupListRow
		if e := rows.Scan(&x.RoutingGroupID, &x.Name, &x.Description, &x.Status, &x.TotalRoutes, &x.ActiveRoutes, &x.HasFailoverCount); e != nil {
			return nil, e
		}
		out = append(out, x)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		n, e := countRoutingGroupClients(ctx, pool, out[i].RoutingGroupID)
		if e != nil {
			return nil, e
		}
		out[i].AssignedClientCount = n
	}
	return out, nil
}

func GetRoutingGroup(ctx context.Context, pool *pgxpool.Pool, id string) (*RoutingGroup, error) {
	var g RoutingGroup
	var up *time.Time
	err := pool.QueryRow(ctx, `
		SELECT routing_group_id::text, name, description, status, updated_at
		FROM routing_groups
		WHERE routing_group_id = $1::uuid`, id,
	).Scan(&g.RoutingGroupID, &g.Name, &g.Description, &g.Status, &up)
	if err != nil {
		return nil, err
	}
	if up != nil {
		s := up.UTC().Format(time.RFC3339)
		g.UpdatedAt = &s
	}
	return &g, nil
}

func CreateRoutingGroup(ctx context.Context, pool *pgxpool.Pool, p UpsertRoutingGroupParams) (string, error) {
	var id string
	err := pool.QueryRow(ctx, `
		INSERT INTO routing_groups (name, description, status)
		VALUES ($1, $2, $3)
		RETURNING routing_group_id::text`,
		p.Name, p.Description, p.Status,
	).Scan(&id)
	if err == nil {
		return id, nil
	}
	var pe *pgconn.PgError
	if errors.As(err, &pe) && pe.Code == "23505" {
		return "", ErrDuplicateRoutingGroupName
	}
	return "", err
}

func UpdateRoutingGroup(ctx context.Context, pool *pgxpool.Pool, id string, p UpsertRoutingGroupParams) error {
	ct, err := pool.Exec(ctx, `
		UPDATE routing_groups
		SET name = $1, description = $2, status = $3, updated_at = now()
		WHERE routing_group_id = $4::uuid`,
		p.Name, p.Description, p.Status, id,
	)
	if err != nil {
		var pe *pgconn.PgError
		if errors.As(err, &pe) && pe.Code == "23505" {
			return ErrDuplicateRoutingGroupName
		}
		return err
	}
	if ct.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

func ToggleRoutingGroupStatus(ctx context.Context, pool *pgxpool.Pool, id string) (string, error) {
	var s string
	err := pool.QueryRow(ctx, `
		UPDATE routing_groups
		SET status = CASE WHEN status = 'active' THEN 'inactive' ELSE 'active' END, updated_at = now()
		WHERE routing_group_id = $1::uuid
		RETURNING status`, id,
	).Scan(&s)
	return s, err
}

func CountRoutingGroupClientRefs(ctx context.Context, pool *pgxpool.Pool, id string) (int64, error) {
	return countRoutingGroupClients(ctx, pool, id)
}

func countRoutingGroupClients(ctx context.Context, pool *pgxpool.Pool, id string) (int64, error) {
	var n int64
	err := pool.QueryRow(ctx, `SELECT COUNT(*)::bigint FROM clients WHERE routing_group_id = $1::uuid`, id).Scan(&n)
	if err == nil {
		return n, nil
	}
	var pe *pgconn.PgError
	if errors.As(err, &pe) && pe.Code == "42P01" {
		return 0, nil
	}
	return 0, err
}
