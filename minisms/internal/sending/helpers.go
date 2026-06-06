// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package sending

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5"
)

func optionalString(s string) *string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	v := s
	return &v
}

func derefOr(s *string, def string) string {
	if s == nil || strings.TrimSpace(*s) == "" {
		return def
	}
	return strings.TrimSpace(*s)
}

func mulNumeric(ctx context.Context, pool interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, perSMS string, segments int) (string, error) {
	var total string
	err := pool.QueryRow(ctx, `SELECT ($1::numeric(18,6) * $2::int)::numeric(18,6)::text`, perSMS, segments).Scan(&total)
	return total, err
}

func lockAndCheckBalance(ctx context.Context, tx pgx.Tx, clientID, required string) (string, bool, error) {
	var bal string
	var enough bool
	err := tx.QueryRow(ctx, `
		SELECT balance::text, (balance >= $2::numeric(18,6)) AS enough
		FROM clients
		WHERE client_id = $1::uuid
		FOR UPDATE`, clientID, required).Scan(&bal, &enough)
	return bal, enough, err
}

func updateSMSLogCarrierSkipReason(ctx context.Context, tx pgx.Tx, messageID string, skipJSON []byte) error {
	_, err := tx.Exec(ctx, `UPDATE sms_logs SET carrier_skip_reason = $2::jsonb WHERE message_id = $1::uuid`, messageID, string(skipJSON))
	return err
}

func ingressTransportString(t IngressTransport) string {
	if t == "" {
		return string(IngressHTTP)
	}
	return string(t)
}

func egressTransportString(t EgressTransport) string {
	if t == "" {
		return string(EgressHTTP)
	}
	return string(t)
}

// ResolveDLRWebhookURL picks the webhook URL for DLR forwarding (shared by REST and SMPP ingress).
func ResolveDLRWebhookURL(dlrRequested bool, reqDLRURL string, clientDLRURL *string) *string {
	if !dlrRequested {
		return nil
	}
	if strings.TrimSpace(reqDLRURL) != "" {
		return optionalString(reqDLRURL)
	}
	if clientDLRURL != nil && strings.TrimSpace(*clientDLRURL) != "" {
		return optionalString(strings.TrimSpace(*clientDLRURL))
	}
	return nil
}
