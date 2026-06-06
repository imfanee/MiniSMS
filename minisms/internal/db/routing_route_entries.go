// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package db

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type CarrierChoice struct {
	CarrierID string
	Name      string
	Status    string
	Balance   string
}

type RouteEntryDetail struct {
	RouteEntryID           string
	RoutingGroupID         string
	Prefix                 string
	Description            *string
	Priority               int
	Status                 string
	PrimaryCarrierID       string
	PrimaryCarrierName     string
	PrimaryCarrierStatus   string
	PrimaryBalance         string
	Failover1CarrierID     *string
	Failover1CarrierName   *string
	Failover1CarrierStatus *string
	Failover1Balance       *string
	Failover2CarrierID     *string
	Failover2CarrierName   *string
	Failover2CarrierStatus *string
	Failover2Balance       *string
}

type UpsertRouteEntryParams struct {
	Prefix             string
	Description        *string
	Priority           int
	Status             string
	PrimaryCarrierID   string
	Failover1CarrierID *string
	Failover2CarrierID *string
}

var ErrDuplicateRoutePrefix = errors.New("duplicate route prefix")

func ListCarrierChoices(ctx context.Context, pool *pgxpool.Pool) ([]CarrierChoice, error) {
	rows, err := pool.Query(ctx, `
		SELECT carrier_id::text, name, status, balance::text
		FROM carriers
		ORDER BY CASE WHEN status='active' THEN 0 ELSE 1 END, name ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CarrierChoice
	for rows.Next() {
		var c CarrierChoice
		if e := rows.Scan(&c.CarrierID, &c.Name, &c.Status, &c.Balance); e != nil {
			return nil, e
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func GetCarrierByID(ctx context.Context, pool *pgxpool.Pool, id string) (*CarrierChoice, error) {
	var c CarrierChoice
	err := pool.QueryRow(ctx, `SELECT carrier_id::text, name, status, balance::text FROM carriers WHERE carrier_id = $1::uuid`, id).
		Scan(&c.CarrierID, &c.Name, &c.Status, &c.Balance)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func ExistsRoutePrefix(ctx context.Context, pool *pgxpool.Pool, routingGroupID, prefix string, excludeID *string) (bool, error) {
	var n int
	if excludeID != nil && *excludeID != "" {
		err := pool.QueryRow(ctx, `
			SELECT 1 FROM route_entries
			WHERE routing_group_id = $1::uuid AND prefix = $2 AND route_entry_id <> $3::uuid
			LIMIT 1`, routingGroupID, prefix, *excludeID).Scan(&n)
		if err == pgx.ErrNoRows {
			return false, nil
		}
		return err == nil, err
	}
	err := pool.QueryRow(ctx, `SELECT 1 FROM route_entries WHERE routing_group_id = $1::uuid AND prefix = $2 LIMIT 1`, routingGroupID, prefix).Scan(&n)
	if err == pgx.ErrNoRows {
		return false, nil
	}
	return err == nil, err
}

func ListRouteEntries(ctx context.Context, pool *pgxpool.Pool, routingGroupID string) ([]RouteEntryDetail, error) {
	rows, err := pool.Query(ctx, `
		SELECT route_entry_id::text, routing_group_id::text, prefix, description, priority, status,
			primary_carrier_id::text, primary_carrier_name, primary_carrier_status, primary_carrier_balance::text,
			failover1_carrier_id::text, failover1_carrier_name, failover1_carrier_status, failover1_carrier_balance::text,
			failover2_carrier_id::text, failover2_carrier_name, failover2_carrier_status, failover2_carrier_balance::text
		FROM v_route_entries_detail
		WHERE routing_group_id = $1::uuid
		ORDER BY CASE WHEN prefix='*' THEN 1 ELSE 0 END ASC, length(prefix) DESC, priority ASC`, routingGroupID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RouteEntryDetail
	for rows.Next() {
		var x RouteEntryDetail
		if e := rows.Scan(
			&x.RouteEntryID, &x.RoutingGroupID, &x.Prefix, &x.Description, &x.Priority, &x.Status,
			&x.PrimaryCarrierID, &x.PrimaryCarrierName, &x.PrimaryCarrierStatus, &x.PrimaryBalance,
			&x.Failover1CarrierID, &x.Failover1CarrierName, &x.Failover1CarrierStatus, &x.Failover1Balance,
			&x.Failover2CarrierID, &x.Failover2CarrierName, &x.Failover2CarrierStatus, &x.Failover2Balance,
		); e != nil {
			return nil, e
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

func GetRouteEntryDetail(ctx context.Context, pool *pgxpool.Pool, routingGroupID, routeEntryID string) (*RouteEntryDetail, error) {
	var x RouteEntryDetail
	err := pool.QueryRow(ctx, `
		SELECT route_entry_id::text, routing_group_id::text, prefix, description, priority, status,
			primary_carrier_id::text, primary_carrier_name, primary_carrier_status, primary_carrier_balance::text,
			failover1_carrier_id::text, failover1_carrier_name, failover1_carrier_status, failover1_carrier_balance::text,
			failover2_carrier_id::text, failover2_carrier_name, failover2_carrier_status, failover2_carrier_balance::text
		FROM v_route_entries_detail
		WHERE routing_group_id = $1::uuid AND route_entry_id = $2::uuid`, routingGroupID, routeEntryID,
	).Scan(
		&x.RouteEntryID, &x.RoutingGroupID, &x.Prefix, &x.Description, &x.Priority, &x.Status,
		&x.PrimaryCarrierID, &x.PrimaryCarrierName, &x.PrimaryCarrierStatus, &x.PrimaryBalance,
		&x.Failover1CarrierID, &x.Failover1CarrierName, &x.Failover1CarrierStatus, &x.Failover1Balance,
		&x.Failover2CarrierID, &x.Failover2CarrierName, &x.Failover2CarrierStatus, &x.Failover2Balance,
	)
	if err != nil {
		return nil, err
	}
	return &x, nil
}

func CreateRouteEntry(ctx context.Context, pool *pgxpool.Pool, routingGroupID string, p UpsertRouteEntryParams) (string, error) {
	var id string
	err := pool.QueryRow(ctx, `
		INSERT INTO route_entries (
			routing_group_id, prefix, description, priority, status, primary_carrier_id, failover1_carrier_id, failover2_carrier_id
		) VALUES ($1::uuid,$2,$3,$4,$5,$6::uuid,$7::uuid,$8::uuid)
		RETURNING route_entry_id::text`,
		routingGroupID, p.Prefix, p.Description, p.Priority, p.Status, p.PrimaryCarrierID, p.Failover1CarrierID, p.Failover2CarrierID,
	).Scan(&id)
	if err == nil {
		return id, nil
	}
	var pe *pgconn.PgError
	if errors.As(err, &pe) && pe.Code == "23505" {
		return "", ErrDuplicateRoutePrefix
	}
	return "", err
}

func UpdateRouteEntry(ctx context.Context, pool *pgxpool.Pool, routingGroupID, routeEntryID string, p UpsertRouteEntryParams) error {
	ct, err := pool.Exec(ctx, `
		UPDATE route_entries
		SET prefix=$1, description=$2, priority=$3, status=$4, primary_carrier_id=$5::uuid, failover1_carrier_id=$6::uuid, failover2_carrier_id=$7::uuid, updated_at=now()
		WHERE routing_group_id=$8::uuid AND route_entry_id=$9::uuid`,
		p.Prefix, p.Description, p.Priority, p.Status, p.PrimaryCarrierID, p.Failover1CarrierID, p.Failover2CarrierID, routingGroupID, routeEntryID,
	)
	if err != nil {
		var pe *pgconn.PgError
		if errors.As(err, &pe) && pe.Code == "23505" {
			return ErrDuplicateRoutePrefix
		}
		return err
	}
	if ct.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

func DeleteRouteEntry(ctx context.Context, pool *pgxpool.Pool, routingGroupID, routeEntryID string) (bool, error) {
	ct, err := pool.Exec(ctx, `DELETE FROM route_entries WHERE routing_group_id = $1::uuid AND route_entry_id = $2::uuid`, routingGroupID, routeEntryID)
	if err != nil {
		return false, err
	}
	return ct.RowsAffected() > 0, nil
}
