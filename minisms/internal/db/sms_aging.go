// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package db

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ListStuckAcceptedNoDLR returns message IDs for messages the carrier accepted but never sent a final
// DLR for: still 'accepted', the client asked for a DLR, and old enough (received before olderThan).
// These are candidates for aging out to 'undelivered'.
func ListStuckAcceptedNoDLR(ctx context.Context, pool *pgxpool.Pool, olderThan time.Time, limit int) ([]string, error) {
	rows, err := pool.Query(ctx, `
		SELECT message_id::text
		FROM sms_logs
		WHERE status = 'accepted'
		  AND dlr_requested = true
		  AND received_at < $1
		ORDER BY received_at ASC
		LIMIT $2`, olderThan, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// AgeOutAcceptedNoDLR closes out a still-'accepted' message because no final DLR arrived within the
// validity window. It records the same terminal shape the system uses for a real UNDELIV receipt
// (status 'failed', dlr_status 'undelivered') so the UI stays consistent, and touches no ledger: the
// message was carrier-accepted and billed correctly, so a missing DLR is an unknown-final outcome, not
// a refund. The guard on status='accepted' makes it a no-op if a real DLR landed meanwhile. Returns
// whether it applied.
func AgeOutAcceptedNoDLR(ctx context.Context, pool *pgxpool.Pool, messageID string) (bool, error) {
	tag, err := pool.Exec(ctx, `
		UPDATE sms_logs
		SET status = 'failed',
			dlr_status = 'undelivered',
			dlr_received_at = COALESCE(dlr_received_at, now()),
			failed_at = COALESCE(failed_at, now())
		WHERE message_id = $1::uuid AND status = 'accepted'`, messageID)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}
