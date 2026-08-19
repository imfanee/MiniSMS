// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package sending

import (
	"context"
	"log/slog"

	"github.com/minisms/minisms/internal/billing"
	"github.com/minisms/minisms/internal/db"
	"github.com/minisms/minisms/internal/smslog"
)

// RecoverStuckSyncSends refunds and marks undelivered any messages left in 'sending' by a crash during
// a synchronous send. The synchronous path reserves the client charge, releases the DB connection to
// dispatch, then finalizes; a crash in that window would otherwise leave the client charged for an
// undelivered message. It must run ONCE at startup and ONLY when the async queue is disabled: in queue
// mode the QueueRunner owns 'sending' rows (in-flight worker claims) and its own reaper handles them, so
// running this then would wrongly refund live messages. At boot in sync mode no send is in flight yet,
// so every 'sending' row is necessarily a leftover from a previous process.
func RecoverStuckSyncSends(ctx context.Context, s *Service) (int, error) {
	rows, err := db.ListStuckSending(ctx, s.Pool)
	if err != nil {
		return 0, err
	}
	recovered := 0
	for _, r := range rows {
		tx, err := s.Pool.Begin(ctx)
		if err != nil {
			continue
		}
		if err := db.MarkSMSUndelivered(ctx, tx, r.MessageID, nil, "recovered after restart"); err != nil {
			_ = tx.Rollback(ctx)
			continue
		}
		if _, err := billing.CreditClientBalance(ctx, tx, r.ClientID, r.TotalCharged, r.Currency, "recovery_refund", "Refund: send interrupted by restart"); err != nil {
			_ = tx.Rollback(ctx)
			continue
		}
		_ = db.AppendSMSEventTimeline(ctx, tx, r.MessageID, smslog.NewEvent(
			smslog.EventDispatchFailed, "Recovered after restart", "reserved charge refunded", nil))
		if err := tx.Commit(ctx); err != nil {
			_ = tx.Rollback(ctx)
			continue
		}
		recovered++
	}
	if recovered > 0 {
		slog.Info("sync send recovery refunded interrupted messages", "count", recovered)
	}
	return recovered, nil
}
