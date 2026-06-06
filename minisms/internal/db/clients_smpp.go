// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package db

import (
	"context"
	"crypto/subtle"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ClientSMPPIngress is the SMPP SMSC bind profile for a client.
type ClientSMPPIngress struct {
	ClientID            string
	Status              string
	SMPPSystemID        string
	SMPPPasswordEnc     string
	SMPPAllowedCIDRs    *string
	SMPPMaxBinds        int
	SMPPDefaultSrcTON   *int16
	SMPPDefaultSrcNPI   *int16
	SMPPThroughputPerS  int
	DLRDeliveryMode     string
	DLRWebhookURL       *string
}

var ErrSMPPAuthFailed = errors.New("smpp auth failed")

func LookupClientSMPPIngress(ctx context.Context, pool *pgxpool.Pool, systemID string) (*ClientSMPPIngress, error) {
	var c ClientSMPPIngress
	err := pool.QueryRow(ctx, `
		SELECT client_id::text, status, smpp_system_id, smpp_password_enc, smpp_allowed_cidrs,
			smpp_max_binds, smpp_default_src_ton, smpp_default_src_npi, smpp_throughput_per_s,
			dlr_delivery_mode, dlr_webhook_url
		FROM clients
		WHERE smpp_ingress_enabled = TRUE
		  AND smpp_system_id IS NOT NULL
		  AND trim(smpp_system_id) = trim($1)
		LIMIT 1`, systemID).Scan(
		&c.ClientID, &c.Status, &c.SMPPSystemID, &c.SMPPPasswordEnc, &c.SMPPAllowedCIDRs,
		&c.SMPPMaxBinds, &c.SMPPDefaultSrcTON, &c.SMPPDefaultSrcNPI, &c.SMPPThroughputPerS,
		&c.DLRDeliveryMode, &c.DLRWebhookURL,
	)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func VerifyClientSMPPPassword(secretKey []byte, encPassword, provided string) bool {
	expected, err := DecryptValue(secretKey, encPassword)
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(expected), []byte(provided)) == 1
}

func InsertSMPPBindEvent(ctx context.Context, pool *pgxpool.Pool, clientID, eventType, bindMode string, remoteAddr *string, commandStatus *int, detail string) error {
	_, err := pool.Exec(ctx, `
		INSERT INTO smpp_bind_events (entity_type, entity_id, remote_addr, bind_mode, event_type, command_status, detail)
		VALUES ('client', $1::uuid, $2::inet, $3, $4, $5, $6)`,
		clientID, remoteAddr, optionalBindMode(bindMode), eventType, commandStatus, optionalDetail(detail),
	)
	return err
}

func optionalBindMode(s string) *string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	v := strings.TrimSpace(s)
	return &v
}

func optionalDetail(s string) *string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	v := strings.TrimSpace(s)
	return &v
}

// CountClientSMPPBindEvents is a helper for tests; production uses in-memory session registry.
func CountActiveClientBinds(ctx context.Context, pool *pgxpool.Pool, clientID string) (int, error) {
	var n int
	err := pool.QueryRow(ctx, `
		SELECT COUNT(*)::int FROM smpp_bind_events
		WHERE entity_type = 'client' AND entity_id = $1::uuid
		  AND event_type = 'bind_ok'
		  AND created_at > now() - interval '24 hours'`, clientID).Scan(&n)
	if err != nil && err != pgx.ErrNoRows {
		return 0, err
	}
	return n, nil
}
