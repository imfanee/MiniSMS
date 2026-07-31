// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package runtime

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/minisms/minisms/internal/db"
	"github.com/minisms/minisms/internal/dlr"
	"github.com/minisms/minisms/internal/smslog"
)

// ageRunner periodically closes out messages the carrier accepted but never sent a final DLR for.
// After the configured TTL a still-'accepted' + DLR-requested message is marked undelivered and the
// client is notified over its DLR channel. It never refunds: the message was carrier-accepted and
// billed, so a missing DLR is an unknown-final outcome, not a non-delivery.
type ageRunner struct {
	pool   *pgxpool.Pool
	dlr    *dlr.Processor
	ttl    time.Duration
	tick   time.Duration
	batch  int
	cancel context.CancelFunc
	done   chan struct{}
}

func startAgeRunner(ctx context.Context, pool *pgxpool.Pool, dlrProc *dlr.Processor, ttlSecs int) *ageRunner {
	rctx, cancel := context.WithCancel(ctx)
	r := &ageRunner{
		pool:   pool,
		dlr:    dlrProc,
		ttl:    time.Duration(ttlSecs) * time.Second,
		tick:   5 * time.Minute,
		batch:  200,
		cancel: cancel,
		done:   make(chan struct{}),
	}
	go r.loop(rctx)
	return r
}

func (r *ageRunner) loop(ctx context.Context) {
	defer close(r.done)
	// A short initial delay lets the process settle before the first sweep of the backlog.
	t := time.NewTimer(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			r.sweep(ctx)
			t.Reset(r.tick)
		}
	}
}

func (r *ageRunner) sweep(ctx context.Context) {
	cutoff := time.Now().Add(-r.ttl)
	ids, err := db.ListStuckAcceptedNoDLR(ctx, r.pool, cutoff, r.batch)
	if err != nil {
		slog.Warn("age reaper list failed", "error", err.Error())
		return
	}
	var aged int
	for _, id := range ids {
		if ctx.Err() != nil {
			return
		}
		applied, err := db.AgeOutAcceptedNoDLR(ctx, r.pool, id)
		if err != nil {
			slog.Warn("age reaper mark failed", "message_id", id, "error", err.Error())
			continue
		}
		if !applied {
			continue // a real DLR landed between the list and the update
		}
		_ = db.AppendSMSEventTimeline(ctx, r.pool, id, smslog.NewEvent(
			smslog.EventDLRReceived,
			"Aged out - no final DLR",
			"No delivery receipt arrived within the validity window; closed as undelivered (billing unchanged).",
			map[string]any{"reason": "no_dlr_within_ttl", "ttl_seconds": int(r.ttl.Seconds())},
		))
		// Notify the client over its configured DLR channel (http/smpp) with the undelivered receipt.
		if _, err := r.dlr.ResendToClient(ctx, id); err != nil {
			slog.Warn("age reaper client notify failed", "message_id", id, "error", err.Error())
		}
		aged++
	}
	if aged > 0 {
		slog.Info("age reaper closed stuck messages", "aged", aged, "ttl_seconds", int(r.ttl.Seconds()))
	}
}

func (r *ageRunner) stop() {
	if r == nil {
		return
	}
	r.cancel()
	<-r.done
}
