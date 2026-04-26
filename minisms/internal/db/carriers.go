package db

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// CarrierRow is a carrier for list and row templates.
type CarrierRow struct {
	CarrierID     string
	Name          string
	EndpointURL   string
	HTTPMethod    string
	Status        string
	Currency      string
	Balance       string
	RateGroupName *string
	RateGroupID   *string
	Notes         *string
}

// CarrierFull is a carrier for detail and updates.
type CarrierFull struct {
	CarrierID              string
	Name                   string
	EndpointURL            string
	HTTPMethod             string
	Status                 string
	Currency               string
	Balance                string
	RateGroupID            *string
	Notes                  *string
	DLRCallbackURLTemplate *string
	DLRFieldName           *string
	DLRInboundSecret       *string
	DLRMessageIDField      *string
	DLRStatusField         *string
	DLRStatusMap           *string
	SMPPSourceAddrTON      string
	SMPPSourceAddrNPI      string
	SMPPDestAddrTON        string
	SMPPDestAddrNPI        string
	UpdatedAt              *string
}

// ListCarriers returns carriers with optional rate group name, ordered by name.
func ListCarriers(ctx context.Context, pool *pgxpool.Pool) ([]CarrierRow, error) {
	rows, err := pool.Query(ctx, `
		SELECT c.carrier_id::text, c.name, c.endpoint_url, c.http_method, c.status,
			c.currency::text, c.balance::text, rg.name, c.rate_group_id::text, c.notes
		FROM carriers c
		LEFT JOIN rate_groups rg ON rg.rate_group_id = c.rate_group_id
		ORDER BY c.name ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CarrierRow
	for rows.Next() {
		var r CarrierRow
		var rgn, rgid, notes *string
		if e := rows.Scan(
			&r.CarrierID, &r.Name, &r.EndpointURL, &r.HTTPMethod, &r.Status,
			&r.Currency, &r.Balance, &rgn, &rgid, &notes,
		); e != nil {
			return nil, e
		}
		r.RateGroupName, r.RateGroupID, r.Notes = rgn, rgid, notes
		out = append(out, r)
	}
	return out, rows.Err()
}

// GetCarrier returns one carrier for detail, or pgx.ErrNoRows.
func GetCarrier(ctx context.Context, pool *pgxpool.Pool, id string) (*CarrierFull, error) {
	var c CarrierFull
	var rgid, notes *string
	var uat *time.Time
	err := pool.QueryRow(ctx, `
		SELECT c.carrier_id::text, c.name, c.endpoint_url, c.http_method, c.status, c.currency::text,
			c.balance::text, c.rate_group_id::text, c.notes, c.dlr_callback_url_template, c.dlr_field_name, c.dlr_inbound_secret,
			c.dlr_message_id_field, c.dlr_status_field, c.dlr_status_map::text,
			c.smpp_source_addr_ton, c.smpp_source_addr_npi, c.smpp_dest_addr_ton, c.smpp_dest_addr_npi, c.updated_at
		FROM carriers c
		WHERE c.carrier_id = $1::uuid`, id,
	).Scan(&c.CarrierID, &c.Name, &c.EndpointURL, &c.HTTPMethod, &c.Status, &c.Currency, &c.Balance, &rgid, &notes,
		&c.DLRCallbackURLTemplate, &c.DLRFieldName, &c.DLRInboundSecret, &c.DLRMessageIDField, &c.DLRStatusField, &c.DLRStatusMap,
		&c.SMPPSourceAddrTON, &c.SMPPSourceAddrNPI, &c.SMPPDestAddrTON, &c.SMPPDestAddrNPI, &uat)
	if err != nil {
		return nil, err
	}
	c.RateGroupID, c.Notes = rgid, notes
	if uat != nil {
		s := uat.UTC().Format(time.RFC3339)
		c.UpdatedAt = &s
	}
	return &c, nil
}

type CarrierDLRSettings struct {
	DLRCallbackURLTemplate *string
	DLRFieldName           *string
	DLRInboundSecret       *string
	DLRMessageIDField      *string
	DLRStatusField         *string
	DLRStatusMap           *string
	SMPPSourceAddrTON      string
	SMPPSourceAddrNPI      string
	SMPPDestAddrTON        string
	SMPPDestAddrNPI        string
}

func UpdateCarrierDLRSettings(ctx context.Context, pool *pgxpool.Pool, carrierID string, s CarrierDLRSettings) error {
	_, err := pool.Exec(ctx, `
		UPDATE carriers
		SET dlr_callback_url_template=$1, dlr_field_name=$2, dlr_inbound_secret=$3, dlr_message_id_field=$4, dlr_status_field=$5, dlr_status_map=$6::jsonb,
			smpp_source_addr_ton=$7, smpp_source_addr_npi=$8, smpp_dest_addr_ton=$9, smpp_dest_addr_npi=$10, updated_at=now()
		WHERE carrier_id=$11::uuid`,
		s.DLRCallbackURLTemplate, s.DLRFieldName, s.DLRInboundSecret, s.DLRMessageIDField, s.DLRStatusField, s.DLRStatusMap,
		s.SMPPSourceAddrTON, s.SMPPSourceAddrNPI, s.SMPPDestAddrTON, s.SMPPDestAddrNPI, carrierID)
	return err
}

// RateGroupOption is id+name for selects.
type RateGroupOption struct {
	ID   string
	Name string
}

// ListRateGroupsIDName returns all rate groups for dropdowns.
func ListRateGroupsIDName(ctx context.Context, pool *pgxpool.Pool) ([]RateGroupOption, error) {
	rows, err := pool.Query(ctx, `SELECT rate_group_id::text, name FROM rate_groups ORDER BY name ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RateGroupOption
	for rows.Next() {
		var o RateGroupOption
		if e := rows.Scan(&o.ID, &o.Name); e != nil {
			return nil, e
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// RateGroupExists returns true if id exists in rate_groups.
func RateGroupExists(ctx context.Context, pool *pgxpool.Pool, id string) (bool, error) {
	var n int
	err := pool.QueryRow(ctx, `SELECT 1 FROM rate_groups WHERE rate_group_id = $1::uuid`, id).Scan(&n)
	if err == pgx.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// CreateCarrierParams for insert.
type CreateCarrierParams struct {
	Name        string
	EndpointURL string
	HTTPMethod  string
	Status      string
	Currency    string
	RateGroupID *string
	Notes       *string
}

// ErrDuplicateCarrierName is returned on unique violation on name.
var ErrDuplicateCarrierName = errors.New("duplicate carrier name")

// CreateCarrier inserts a carrier, returns the new id.
func CreateCarrier(ctx context.Context, pool *pgxpool.Pool, p CreateCarrierParams) (string, error) {
	var rgid any
	if p.RateGroupID != nil {
		rgid = *p.RateGroupID
	}
	var id string
	err := pool.QueryRow(ctx, `
		INSERT INTO carriers (name, endpoint_url, http_method, status, currency, rate_group_id, notes)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING carrier_id::text`,
		p.Name, p.EndpointURL, p.HTTPMethod, p.Status, p.Currency, rgid, p.Notes,
	).Scan(&id)
	if err == nil {
		return id, nil
	}
	var pe *pgconn.PgError
	if errors.As(err, &pe) && pe.Code == "23505" {
		return "", ErrDuplicateCarrierName
	}
	return "", err
}

// UpdateCarrierParams for update.
type UpdateCarrierParams struct {
	Name        string
	EndpointURL string
	HTTPMethod  string
	Status      string
	Currency    string
	RateGroupID *string
	Notes       *string
}

// UpdateCarrier updates a carrier. ErrDuplicate on name unique violation.
func UpdateCarrier(ctx context.Context, pool *pgxpool.Pool, carrierID string, p UpdateCarrierParams) error {
	var rgid any
	if p.RateGroupID != nil {
		rgid = *p.RateGroupID
	}
	ct, err := pool.Exec(ctx, `
		UPDATE carriers SET
			name = $1, endpoint_url = $2, http_method = $3, status = $4, currency = $5, rate_group_id = $6, notes = $7, updated_at = now()
		WHERE carrier_id = $8::uuid`,
		p.Name, p.EndpointURL, p.HTTPMethod, p.Status, p.Currency, rgid, p.Notes, carrierID,
	)
	if err != nil {
		var pe *pgconn.PgError
		if errors.As(err, &pe) && pe.Code == "23505" {
			return ErrDuplicateCarrierName
		}
		return err
	}
	if ct.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

// ToggleCarrierStatus flips between active and inactive.
func ToggleCarrierStatus(ctx context.Context, pool *pgxpool.Pool, carrierID string) (string, error) {
	var s string
	err := pool.QueryRow(ctx, `
		UPDATE carriers SET
			status = CASE WHEN status = 'active' THEN 'inactive' ELSE 'active' END, updated_at = now()
		WHERE carrier_id = $1::uuid
		RETURNING status`, carrierID,
	).Scan(&s)
	if err != nil {
		return "", err
	}
	return s, nil
}

// GetCarrierRow fetches a single list row (after create/update) by id.
func GetCarrierRow(ctx context.Context, pool *pgxpool.Pool, carrierID string) (*CarrierRow, error) {
	var r CarrierRow
	var rgn, rgid, notes *string
	err := pool.QueryRow(ctx, `
		SELECT c.carrier_id::text, c.name, c.endpoint_url, c.http_method, c.status, c.currency::text, c.balance::text,
			rg.name, c.rate_group_id::text, c.notes
		FROM carriers c
		LEFT JOIN rate_groups rg ON rg.rate_group_id = c.rate_group_id
		WHERE c.carrier_id = $1::uuid`, carrierID,
	).Scan(&r.CarrierID, &r.Name, &r.EndpointURL, &r.HTTPMethod, &r.Status, &r.Currency, &r.Balance, &rgn, &rgid, &notes)
	if err != nil {
		return nil, err
	}
	r.RateGroupName, r.RateGroupID, r.Notes = rgn, rgid, notes
	return &r, nil
}
