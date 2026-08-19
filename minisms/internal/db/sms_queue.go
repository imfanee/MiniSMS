// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package db

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// StampQueued marks a freshly-created sms_log as queued for async dispatch and
// sets its first-attempt and expiry timestamps. Runs inside the accept tx.
func StampQueued(ctx context.Context, tx pgx.Tx, messageID string, ttl time.Duration) error {
	_, err := tx.Exec(ctx, `
		UPDATE sms_logs
		SET status='queued', next_attempt_at=now(), expires_at=now() + $2::interval
		WHERE message_id=$1::uuid`,
		messageID, ttl.String())
	return err
}

// StampSending marks a freshly-created sms_log as in-flight for the SYNCHRONOUS send path and stamps
// its claim time and validity. The synchronous path has no background reaper, so a crash before finalize
// leaves a 'sending' row that RecoverStuckSyncSends (run at boot) refunds. Runs inside the reserve tx.
func StampSending(ctx context.Context, tx pgx.Tx, messageID string, ttl time.Duration) error {
	_, err := tx.Exec(ctx, `
		UPDATE sms_logs
		SET status='sending', claimed_at=now(), expires_at=now() + $2::interval
		WHERE message_id=$1::uuid`,
		messageID, ttl.String())
	return err
}

// StuckSendingRow identifies a message left in 'sending' by an interrupted synchronous send.
type StuckSendingRow struct {
	MessageID    string
	ClientID     string
	TotalCharged string
	Currency     string
}

// ListStuckSending returns messages still in 'sending' (used only at boot in synchronous mode, where any
// such row is a leftover from a crash, since no send is in flight yet).
func ListStuckSending(ctx context.Context, pool *pgxpool.Pool) ([]StuckSendingRow, error) {
	rows, err := pool.Query(ctx, `
		SELECT message_id::text, client_id::text, total_charged::text, currency::text
		FROM sms_logs WHERE status='sending'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []StuckSendingRow
	for rows.Next() {
		var r StuckSendingRow
		if err := rows.Scan(&r.MessageID, &r.ClientID, &r.TotalCharged, &r.Currency); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// QueuedSMS is the data a worker needs to dispatch a claimed message. The client
// charge is already reserved (total_charged), so dispatch never re-charges the
// client; it only debits the carrier on success.
type QueuedSMS struct {
	MessageID        string
	ClientID         string
	To               string
	From             string
	Body             string
	DLRRequested     bool
	DLRWebhookURL    *string
	RoutingGroupID   *string
	RateApplied      string
	TotalCharged     string
	Currency         string
	SenderIDSource   string
	IngressTransport string
	Attempts         int
	Expired          bool
}

// ClaimQueuedForSend atomically claims up to `limit` due queued messages for this
// worker (status queued -> sending) using FOR UPDATE SKIP LOCKED so concurrent
// workers never grab the same row. Rows past their validity are returned with
// Expired=true so the caller can mark them undelivered and refund.
func ClaimQueuedForSend(ctx context.Context, pool *pgxpool.Pool, limit int) ([]QueuedSMS, error) {
	rows, err := pool.Query(ctx, `
		UPDATE sms_logs SET status='sending', claimed_at=now(), attempts=attempts+1
		WHERE message_id IN (
			SELECT message_id FROM sms_logs
			WHERE status='queued' AND (next_attempt_at IS NULL OR next_attempt_at <= now())
			ORDER BY next_attempt_at NULLS FIRST, received_at
			FOR UPDATE SKIP LOCKED
			LIMIT $1
		)
		RETURNING message_id::text, client_id::text, to_number, COALESCE(from_number,''),
			message_body, dlr_requested, dlr_webhook_url, routing_group_id::text,
			rate_applied::text, total_charged::text, currency::text, sender_id_source,
			ingress_transport, attempts,
			(expires_at IS NOT NULL AND expires_at <= now()) AS expired`,
		limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []QueuedSMS
	for rows.Next() {
		var q QueuedSMS
		if err := rows.Scan(&q.MessageID, &q.ClientID, &q.To, &q.From, &q.Body,
			&q.DLRRequested, &q.DLRWebhookURL, &q.RoutingGroupID, &q.RateApplied,
			&q.TotalCharged, &q.Currency, &q.SenderIDSource, &q.IngressTransport,
			&q.Attempts, &q.Expired); err != nil {
			return nil, err
		}
		out = append(out, q)
	}
	return out, rows.Err()
}

// RequeueSMS returns a claimed message to the queue for a later retry, recording
// the last transient error for visibility.
func RequeueSMS(ctx context.Context, pool *pgxpool.Pool, messageID string, nextAttempt time.Time, lastCode *int, lastBody string) error {
	_, err := pool.Exec(ctx, `
		UPDATE sms_logs
		SET status='queued', next_attempt_at=$2, claimed_at=NULL,
			carrier_response_code=$3, carrier_response_body=$4
		WHERE message_id=$1::uuid AND status='sending'`,
		messageID, nextAttempt, lastCode, lastBody)
	return err
}

// ReapStuckSending returns rows left in 'sending' by a dead worker (claimed before
// the cutoff) back to the queue. Returns how many were reclaimed.
func ReapStuckSending(ctx context.Context, pool *pgxpool.Pool, olderThan time.Time) (int64, error) {
	tag, err := pool.Exec(ctx, `
		UPDATE sms_logs
		SET status='queued', next_attempt_at=now(), claimed_at=NULL
		WHERE status='sending' AND (claimed_at IS NULL OR claimed_at < $1)`,
		olderThan)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// MarkSMSUndelivered finalizes a message that could not be delivered within its
// validity (or hit a permanent error). Runs inside the refund tx.
func MarkSMSUndelivered(ctx context.Context, tx pgx.Tx, messageID string, responseCode *int, responseBody string) error {
	_, err := tx.Exec(ctx, `
		UPDATE sms_logs
		SET status='undelivered', carrier_response_code=$2, carrier_response_body=$3,
			claimed_at=NULL, failed_at=now()
		WHERE message_id=$1::uuid`,
		messageID, responseCode, responseBody)
	return err
}

// ForceMarkAccepted is the last-resort exit for a message the carrier already
// accepted but whose billing tx failed: it takes the row out of the queue so it
// is never re-dispatched (a rare carrier-billing miss is far safer than a
// double-send). Carrier balance may be under-charged; this is logged by the caller.
func ForceMarkAccepted(ctx context.Context, pool *pgxpool.Pool, messageID, carrierID, carrierMessageID, egressTransport string) error {
	_, err := pool.Exec(ctx, `
		UPDATE sms_logs
		SET status='accepted', carrier_id=$2::uuid, carrier_message_id=$3,
			egress_transport=COALESCE(NULLIF($4,''),'http'), claimed_at=NULL, dispatched_at=now()
		WHERE message_id=$1::uuid AND status='sending'`,
		messageID, carrierID, carrierMessageID, egressTransport)
	return err
}

// CountQueued reports how many messages are waiting to be dispatched (queued or
// in-flight), for observability.
func CountQueued(ctx context.Context, pool *pgxpool.Pool) (queued, sending int, err error) {
	err = pool.QueryRow(ctx, `
		SELECT
			COUNT(*) FILTER (WHERE status='queued'),
			COUNT(*) FILTER (WHERE status='sending')
		FROM sms_logs
		WHERE status IN ('queued','sending')`).Scan(&queued, &sending)
	return
}
