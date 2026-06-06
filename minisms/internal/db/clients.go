// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package db

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type ClientListRow struct {
	ClientID          string
	Name              string
	Email             string
	Status            string
	Balance           string
	Currency          string
	RateGroupID       *string
	RateGroupName     *string
	RateGroupCurrency *string
	RoutingGroupID    *string
	RoutingGroupName  *string
	APIKeyPrefix      *string
	Notes             *string
	DLRWebhookURL     *string
	DLRWebhookSecret  *string
}

type Client struct {
	ClientID              string
	Name                  string
	Email                 string
	Status                string
	Balance               string
	Currency              string
	RateGroupID           *string
	RateGroupName         *string
	RateGroupCurrency     *string
	RoutingGroupID        *string
	RoutingGroupName      *string
	APIKeyPrefix          *string
	Notes                 *string
	DefaultSenderIDValue  *string
	AllowedSenderIDsMode  string
	AllowInLossDelivery   bool
	DLRWebhookURL            *string
	DLRWebhookSecret         *string
	DLRWebhookMethod         string
	DLRWebhookQueryTemplate  *string
	DLRWebhookBodyTemplate   *string
	SMPPIngressEnabled       bool
	SMPPSystemID         *string
	SMPPPasswordEnc      *string
	SMPPAllowedCIDRs     *string
	SMPPMaxBinds         int
	SMPPDefaultSrcTON    *int16
	SMPPDefaultSrcNPI    *int16
	SMPPThroughputPerS   int
	DLRDeliveryMode      string
	UpdatedAt            *string
}

type UpsertClientParams struct {
	Name                 string
	Email                string
	Status               string
	RateGroupID          *string
	Currency             string
	RoutingGroupID       *string
	Notes                *string
	DefaultSenderIDValue *string
	AllowedSenderIDsMode string
	AllowInLossDelivery  bool
	DLRWebhookURL           *string
	DLRWebhookSecret        *string
	DLRWebhookMethod        string
	DLRWebhookQueryTemplate *string
	DLRWebhookBodyTemplate  *string
}

var (
	ErrDuplicateClientEmail = errors.New("duplicate client email")
)

func ListClients(ctx context.Context, pool *pgxpool.Pool) ([]ClientListRow, error) {
	rows, err := pool.Query(ctx, `
		SELECT c.client_id::text, c.name, c.email::text, c.status, c.balance::text, c.currency::text,
			c.rate_group_id::text, rg.name, rg.currency::text,
			c.routing_group_id::text, rog.name,
			ak.key_prefix::text, c.notes, c.dlr_webhook_url, c.dlr_webhook_secret
		FROM clients c
		LEFT JOIN rate_groups rg ON rg.rate_group_id = c.rate_group_id
		LEFT JOIN routing_groups rog ON rog.routing_group_id = c.routing_group_id
		LEFT JOIN LATERAL (
			SELECT key_prefix
			FROM client_api_keys
			WHERE client_id = c.client_id AND revoked_at IS NULL
			ORDER BY created_at DESC
			LIMIT 1
		) ak ON true
		ORDER BY c.name ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ClientListRow
	for rows.Next() {
		var x ClientListRow
		if e := rows.Scan(
			&x.ClientID, &x.Name, &x.Email, &x.Status, &x.Balance, &x.Currency,
			&x.RateGroupID, &x.RateGroupName, &x.RateGroupCurrency,
			&x.RoutingGroupID, &x.RoutingGroupName,
			&x.APIKeyPrefix, &x.Notes, &x.DLRWebhookURL, &x.DLRWebhookSecret,
		); e != nil {
			return nil, e
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

func GetClient(ctx context.Context, pool *pgxpool.Pool, id string) (*Client, error) {
	var x Client
	var up *time.Time
	err := pool.QueryRow(ctx, `
		SELECT c.client_id::text, c.name, c.email::text, c.status, c.balance::text, c.currency::text,
			c.rate_group_id::text, rg.name, rg.currency::text,
			c.routing_group_id::text, rog.name,
			ak.key_prefix::text, c.notes,
			c.default_sender_id_value, c.allowed_sender_ids_mode, c.allow_in_loss_delivery,
			c.dlr_webhook_url, c.dlr_webhook_secret,
			COALESCE(c.dlr_webhook_method, 'POST'), c.dlr_webhook_query_template, c.dlr_webhook_body_template,
			c.smpp_ingress_enabled, c.smpp_system_id, c.smpp_password_enc, c.smpp_allowed_cidrs,
			c.smpp_max_binds, c.smpp_default_src_ton, c.smpp_default_src_npi, c.smpp_throughput_per_s,
			c.dlr_delivery_mode, c.updated_at
		FROM clients c
		LEFT JOIN rate_groups rg ON rg.rate_group_id = c.rate_group_id
		LEFT JOIN routing_groups rog ON rog.routing_group_id = c.routing_group_id
		LEFT JOIN LATERAL (
			SELECT key_prefix
			FROM client_api_keys
			WHERE client_id = c.client_id AND revoked_at IS NULL
			ORDER BY created_at DESC
			LIMIT 1
		) ak ON true
		WHERE c.client_id = $1::uuid`, id,
	).Scan(
		&x.ClientID, &x.Name, &x.Email, &x.Status, &x.Balance, &x.Currency,
		&x.RateGroupID, &x.RateGroupName, &x.RateGroupCurrency,
		&x.RoutingGroupID, &x.RoutingGroupName,
		&x.APIKeyPrefix, &x.Notes,
		&x.DefaultSenderIDValue, &x.AllowedSenderIDsMode, &x.AllowInLossDelivery,
		&x.DLRWebhookURL, &x.DLRWebhookSecret,
		&x.DLRWebhookMethod, &x.DLRWebhookQueryTemplate, &x.DLRWebhookBodyTemplate,
		&x.SMPPIngressEnabled, &x.SMPPSystemID, &x.SMPPPasswordEnc, &x.SMPPAllowedCIDRs,
		&x.SMPPMaxBinds, &x.SMPPDefaultSrcTON, &x.SMPPDefaultSrcNPI, &x.SMPPThroughputPerS,
		&x.DLRDeliveryMode, &up,
	)
	if err != nil {
		return nil, err
	}
	if up != nil {
		s := up.UTC().Format(time.RFC3339)
		x.UpdatedAt = &s
	}
	return &x, nil
}

func CreateClient(ctx context.Context, pool *pgxpool.Pool, p UpsertClientParams) (string, error) {
	var id string
	err := pool.QueryRow(ctx, `
		INSERT INTO clients (name, email, status, rate_group_id, balance, currency, routing_group_id, notes,
			default_sender_id_value, allowed_sender_ids_mode, allow_in_loss_delivery,
			dlr_webhook_url, dlr_webhook_secret, dlr_webhook_method, dlr_webhook_query_template, dlr_webhook_body_template)
		VALUES ($1, $2, $3, $4::uuid, 0, $5, $6::uuid, $7, $8, $9, $10, $11, $12, $13, $14, $15)
		RETURNING client_id::text`,
		p.Name, p.Email, p.Status, p.RateGroupID, p.Currency, p.RoutingGroupID, p.Notes,
		p.DefaultSenderIDValue, p.AllowedSenderIDsMode, p.AllowInLossDelivery,
		p.DLRWebhookURL, p.DLRWebhookSecret, normalizeDLRWebhookMethod(p.DLRWebhookMethod),
		p.DLRWebhookQueryTemplate, p.DLRWebhookBodyTemplate,
	).Scan(&id)
	if err == nil {
		return id, nil
	}
	var pe *pgconn.PgError
	if errors.As(err, &pe) && pe.Code == "23505" {
		return "", ErrDuplicateClientEmail
	}
	return "", err
}

func UpdateClient(ctx context.Context, pool *pgxpool.Pool, id string, p UpsertClientParams) error {
	ct, err := pool.Exec(ctx, `
		UPDATE clients
		SET name = $1, email = $2, status = $3, rate_group_id = $4::uuid, currency = $5, routing_group_id = $6::uuid, notes = $7,
			default_sender_id_value = $8, allowed_sender_ids_mode = $9, allow_in_loss_delivery = $10,
			dlr_webhook_url = $11, dlr_webhook_secret = $12,
			dlr_webhook_method = $13, dlr_webhook_query_template = $14, dlr_webhook_body_template = $15,
			updated_at = now()
		WHERE client_id = $16::uuid`,
		p.Name, p.Email, p.Status, p.RateGroupID, p.Currency, p.RoutingGroupID, p.Notes,
		p.DefaultSenderIDValue, p.AllowedSenderIDsMode, p.AllowInLossDelivery,
		p.DLRWebhookURL, p.DLRWebhookSecret, normalizeDLRWebhookMethod(p.DLRWebhookMethod),
		p.DLRWebhookQueryTemplate, p.DLRWebhookBodyTemplate, id,
	)
	if err != nil {
		var pe *pgconn.PgError
		if errors.As(err, &pe) && pe.Code == "23505" {
			return ErrDuplicateClientEmail
		}
		return err
	}
	if ct.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

func normalizeDLRWebhookMethod(method string) string {
	switch strings.ToUpper(strings.TrimSpace(method)) {
	case "GET":
		return "GET"
	default:
		return "POST"
	}
}

func ToggleClientStatus(ctx context.Context, pool *pgxpool.Pool, id string) (string, error) {
	var s string
	err := pool.QueryRow(ctx, `
		UPDATE clients
		SET status = CASE WHEN status = 'active' THEN 'suspended' ELSE 'active' END, updated_at = now()
		WHERE client_id = $1::uuid
		RETURNING status`, id,
	).Scan(&s)
	return s, err
}

func RateGroupCurrency(ctx context.Context, pool *pgxpool.Pool, rateGroupID string) (*string, error) {
	var c string
	err := pool.QueryRow(ctx, `SELECT currency::text FROM rate_groups WHERE rate_group_id = $1::uuid`, rateGroupID).Scan(&c)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func RoutingGroupActiveExists(ctx context.Context, pool *pgxpool.Pool, routingGroupID string) (bool, error) {
	var n int
	err := pool.QueryRow(ctx, `SELECT 1 FROM routing_groups WHERE routing_group_id = $1::uuid AND status = 'active'`, routingGroupID).Scan(&n)
	if err == pgx.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func CountClientLedgerEntries(ctx context.Context, pool *pgxpool.Pool, clientID string) (int64, error) {
	var n int64
	err := pool.QueryRow(ctx, `SELECT COUNT(*)::bigint FROM ledger_entries WHERE client_id = $1::uuid`, clientID).Scan(&n)
	return n, err
}
