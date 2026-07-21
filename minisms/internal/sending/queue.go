// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package sending

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/minisms/minisms/internal/billing"
	"github.com/minisms/minisms/internal/carrier"
	"github.com/minisms/minisms/internal/db"
	"github.com/minisms/minisms/internal/smslog"
)

// enqueue accepts a message for asynchronous delivery: it reserves the client
// charge under SELECT FOR UPDATE (preserving no-oversell), writes a 'queued'
// sms_log with a validity/expiry, and returns immediately. A QueueRunner worker
// dispatches it later, so a momentary egress rebind never rejects the message.
// The client is refunded if the message ultimately cannot be delivered.
func (s *Service) enqueue(ctx context.Context, p SubmitParams) SubmitOutcome {
	client := p.Client
	msg := p.Message
	if msg.IngressTransport == "" {
		msg.IngressTransport = IngressHTTP
	}
	if client.RateGroupID == nil || *client.RateGroupID == "" {
		return SubmitOutcome{Kind: OutcomeNoRate, NoRate: "client has no rate group"}
	}
	rateEntry, err := billing.LookupRate(ctx, s.Pool, *client.RateGroupID, msg.To)
	if err != nil {
		return SubmitOutcome{Kind: OutcomeNoRate, NoRate: "no matching rate"}
	}
	encoding, segments := billing.SegmentInfo(msg.Body)
	totalCharge, err := mulNumeric(ctx, s.Pool, rateEntry.RatePerSMS, segments)
	if err != nil {
		return SubmitOutcome{Kind: OutcomeTemporaryUnavailable, TemporaryUnavailable: "failed to compute total charge"}
	}
	routeEntry, err := s.lookupRouteEntry(ctx, client.RoutingGroupID, msg.To)
	if err != nil || routeEntry == nil {
		return SubmitOutcome{Kind: OutcomeNoRoute, NoRoute: "no matching route"}
	}

	ttl := time.Duration(s.Config.SendMessageTTLSecs) * time.Second
	if ttl <= 0 {
		ttl = time.Hour
	}

	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return SubmitOutcome{Kind: OutcomeTemporaryUnavailable, TemporaryUnavailable: "database unavailable"}
	}
	defer func() { _ = tx.Rollback(ctx) }()

	balance, enough, err := lockAndCheckBalance(ctx, tx, client.ClientID, totalCharge)
	if err != nil {
		return SubmitOutcome{Kind: OutcomeTemporaryUnavailable, TemporaryUnavailable: "failed to read balance"}
	}
	if !enough {
		return SubmitOutcome{Kind: OutcomeInsufficientBalance, InsufficientBalance: &BalanceShortfall{Balance: balance, Required: totalCharge}}
	}

	logID, err := db.CreateSMSLog(ctx, tx, db.SMSLog{
		ClientID:         client.ClientID,
		ClientRef:        optionalString(msg.ClientRef),
		ToNumber:         msg.To,
		FromNumber:       optionalString(msg.From),
		MessageBody:      msg.Body,
		MessageLength:    len([]rune(msg.Body)),
		Segments:         segments,
		Encoding:         encoding,
		RateGroupID:      client.RateGroupID,
		PrefixMatched:    optionalString(rateEntry.Prefix),
		RateApplied:      rateEntry.RatePerSMS,
		TotalCharged:     totalCharge,
		Currency:         client.Currency,
		RoutingGroupID:   client.RoutingGroupID,
		RouteEntryID:     optionalString(routeEntry.RouteEntryID),
		Status:           "queued",
		DLRRequested:     msg.DLRRequested,
		DLRWebhookURL:    msg.DLRWebhookURL,
		SenderIDSource:   p.SenderID.Source,
		IngressTransport: ingressTransportString(msg.IngressTransport),
	})
	if err != nil {
		return SubmitOutcome{Kind: OutcomeTemporaryUnavailable, TemporaryUnavailable: "failed to create sms log"}
	}
	if err := db.StampQueued(ctx, tx, logID, ttl); err != nil {
		return SubmitOutcome{Kind: OutcomeTemporaryUnavailable, TemporaryUnavailable: "failed to queue message"}
	}
	remaining, err := billing.DeductClientBalance(ctx, tx, client.ClientID, totalCharge, logID, client.Currency)
	if err != nil {
		return SubmitOutcome{Kind: OutcomeInsufficientBalance, InsufficientBalance: &BalanceShortfall{Balance: balance, Required: totalCharge}}
	}
	timeline := []smslog.TimelineEvent{
		{At: time.Now().UTC().Format(time.RFC3339), Kind: smslog.EventRequestReceived, Title: "SMS request received",
			Detail: fmt.Sprintf("Ingress: %s", strings.ToUpper(ingressTransportString(msg.IngressTransport))),
			Meta:   map[string]any{"to": msg.To, "from": msg.From, "segments": segments}},
		{At: time.Now().UTC().Format(time.RFC3339), Kind: smslog.EventCarrierDispatch, Title: "Queued for delivery",
			Detail: fmt.Sprintf("Charge reserved; will dispatch and retry for up to %s", ttl)},
	}
	_ = db.SetSMSEventTimeline(ctx, tx, logID, timeline)
	if err := tx.Commit(ctx); err != nil {
		return SubmitOutcome{Kind: OutcomeTemporaryUnavailable, TemporaryUnavailable: "transaction commit failed"}
	}

	return SubmitOutcome{
		Kind: OutcomeAccepted,
		Accepted: &AcceptedPayload{
			MessageID:        logID,
			ClientRef:        msg.ClientRef,
			SenderID:         p.SenderID.Value,
			SenderIDSource:   p.SenderID.Source,
			Segments:         segments,
			Charged:          totalCharge,
			BalanceRemaining: remaining,
			Carrier:          "queued",
			DLRRequested:     msg.DLRRequested,
			DLRWebhookURL:    msg.DLRWebhookURL,
		},
	}
}

// QueueRunner dispatches queued messages with a worker pool. Workers claim due
// rows via FOR UPDATE SKIP LOCKED, dispatch through the normal failover path,
// retry transient failures until a message's validity expires, and refund +
// notify on undelivered.
type QueueRunner struct {
	svc              *Service
	workers          int
	batch            int
	backoffBase      time.Duration
	stuck            time.Duration
	pollInterval     time.Duration
	NotifyUndelivered func(ctx context.Context, messageID string)

	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewQueueRunner builds a runner from service config.
func NewQueueRunner(s *Service) *QueueRunner {
	cfg := s.Config
	return &QueueRunner{
		svc:          s,
		workers:      cfg.SendQueueWorkers,
		batch:        cfg.SendQueueBatch,
		backoffBase:  time.Duration(cfg.SendRetryBackoffSecs) * time.Second,
		stuck:        time.Duration(cfg.SendStuckSecs) * time.Second,
		pollInterval: 500 * time.Millisecond,
	}
}

// Start launches the worker pool and the stuck-row reaper.
func (r *QueueRunner) Start(parent context.Context) {
	ctx, cancel := context.WithCancel(parent)
	r.cancel = cancel
	perWorker := r.batch / r.workers
	if perWorker < 1 {
		perWorker = 1
	}
	for i := 0; i < r.workers; i++ {
		r.wg.Add(1)
		go func() {
			defer r.wg.Done()
			r.worker(ctx, perWorker)
		}()
	}
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		r.reaper(ctx)
	}()
	slog.Info("send queue started", "workers", r.workers, "batch", r.batch)
}

// Stop signals workers to finish and waits briefly for them.
func (r *QueueRunner) Stop() {
	if r.cancel != nil {
		r.cancel()
	}
	done := make(chan struct{})
	go func() { r.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
	}
}

func (r *QueueRunner) worker(ctx context.Context, limit int) {
	for {
		if ctx.Err() != nil {
			return
		}
		claimed, err := db.ClaimQueuedForSend(ctx, r.svc.Pool, limit)
		if err != nil {
			slog.Warn("send queue claim", "error", err)
			if !sleepCtxQ(ctx, time.Second) {
				return
			}
			continue
		}
		if len(claimed) == 0 {
			if !sleepCtxQ(ctx, r.pollInterval) {
				return
			}
			continue
		}
		for _, q := range claimed {
			if ctx.Err() != nil {
				return
			}
			r.dispatchOne(ctx, q)
		}
	}
}

func (r *QueueRunner) dispatchOne(ctx context.Context, q db.QueuedSMS) {
	if q.Expired {
		r.finalizeUndelivered(ctx, q, "message validity expired before delivery")
		return
	}
	msg := AcceptedMessage{
		To: q.To, From: q.From, Body: q.Body,
		DLRRequested: q.DLRRequested, DLRWebhookURL: q.DLRWebhookURL,
		IngressTransport: IngressTransport(q.IngressTransport),
	}
	sid := carrier.SenderIDResolution{Value: q.From, Source: q.SenderIDSource}

	route, err := r.svc.lookupRouteEntry(ctx, q.RoutingGroupID, q.To)
	if err != nil || route == nil {
		r.finalizeUndelivered(ctx, q, "no matching route")
		return
	}

	timeoutS, _ := strconv.Atoi(db.Setting(ctx, r.svc.Pool, "carrier_dispatch_timeout_s", strconv.Itoa(r.svc.Config.CarrierDispatchTimeoutSecs)))
	if timeoutS < 1 {
		timeoutS = r.svc.Config.CarrierDispatchTimeoutSecs
	}
	timeout := time.Duration(timeoutS) * time.Second

	win, dispatchErr := r.svc.dispatchWithFailover(ctx, q.MessageID, q.ClientID, msg, route, q.RateApplied, sid, timeout)
	if dispatchErr != nil {
		// Transient (e.g. egress rebinding, carrier down): keep the charge reserved
		// and retry with backoff until the message's validity expires.
		next := time.Now().Add(r.retryBackoff(q.Attempts))
		if err := db.RequeueSMS(ctx, r.svc.Pool, q.MessageID, next, win.LastCode, win.LastBody); err != nil {
			slog.Warn("send queue requeue", "message_id", q.MessageID, "error", err)
		}
		return
	}
	// Carrier accepted the message: it must leave the queue (never re-dispatch).
	r.finalizeSent(ctx, q, win)
}

// finalizeSent bills the carrier and marks the message accepted in one tx. If the
// billing tx fails it forces the message out of the queue anyway (status accepted,
// carrier-billing miss logged) so a carrier-accepted message is never re-sent.
func (r *QueueRunner) finalizeSent(ctx context.Context, q db.QueuedSMS, win *dispatchOutcome) {
	tx, err := r.svc.Pool.Begin(ctx)
	if err == nil {
		defer func() { _ = tx.Rollback(ctx) }()
		carrierCostRate, e1 := billing.LookupCarrierCost(ctx, r.svc.Pool, win.CarrierID, q.To, q.RateApplied)
		carrierCostTotal, e2 := mulNumeric(ctx, r.svc.Pool, carrierCostRate, segmentsOf(q.Body))
		_, e3 := billing.DeductCarrierBalance(ctx, tx, win.CarrierID, carrierCostTotal, q.Currency, q.MessageID)
		e4 := billing.IncrementUsage(ctx, tx, win.CarrierID, segmentsOf(q.Body), carrierCostTotal)
		var smppArr *[4]int16
		if win.SourceAddrTON != nil && win.SourceAddrNPI != nil && win.DestAddrTON != nil && win.DestAddrNPI != nil {
			tmp := [4]int16{*win.SourceAddrTON, *win.SourceAddrNPI, *win.DestAddrTON, *win.DestAddrNPI}
			smppArr = &tmp
		}
		e5 := db.MarkSMSAccepted(ctx, tx, q.MessageID, win.CarrierID, win.FailoverSequence, win.CarrierMessageID, win.LastBodyText, win.StatusCode, smppArr, egressTransportString(win.Egress))
		e6 := db.AppendSMSEventTimeline(ctx, tx, q.MessageID, win.Timeline...)
		if e1 == nil && e2 == nil && e3 == nil && e4 == nil && e5 == nil && e6 == nil {
			if err := tx.Commit(ctx); err == nil {
				return
			}
		}
	}
	// Fallback: force the message out of the queue so it is never re-dispatched.
	slog.Warn("send queue finalize billing failed, forcing accepted", "message_id", q.MessageID, "carrier_id", win.CarrierID)
	_ = db.ForceMarkAccepted(ctx, r.svc.Pool, q.MessageID, win.CarrierID, win.CarrierMessageID, egressTransportString(win.Egress))
}

// finalizeUndelivered refunds the reserved charge, marks the message undelivered,
// and notifies the client (DLR) that delivery failed.
func (r *QueueRunner) finalizeUndelivered(ctx context.Context, q db.QueuedSMS, reason string) {
	tx, err := r.svc.Pool.Begin(ctx)
	if err != nil {
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := db.MarkSMSUndelivered(ctx, tx, q.MessageID, nil, reason); err != nil {
		return
	}
	if _, err := billing.CreditClientBalance(ctx, tx, q.ClientID, q.TotalCharged, q.Currency, "undelivered_refund", "Refund: "+reason); err != nil {
		return
	}
	_ = db.AppendSMSEventTimeline(ctx, tx, q.MessageID, smslog.TimelineEvent{
		At: time.Now().UTC().Format(time.RFC3339), Kind: smslog.EventDispatchFailed,
		Title: "Undelivered", Detail: reason + "; charge refunded",
	})
	if err := tx.Commit(ctx); err != nil {
		return
	}
	if r.NotifyUndelivered != nil {
		r.NotifyUndelivered(ctx, q.MessageID)
	}
}

func (r *QueueRunner) reaper(ctx context.Context) {
	t := time.NewTicker(r.stuck / 2)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			n, err := db.ReapStuckSending(ctx, r.svc.Pool, time.Now().Add(-r.stuck))
			if err == nil && n > 0 {
				slog.Warn("send queue reaped stuck rows", "count", n)
			}
		}
	}
}

func (r *QueueRunner) retryBackoff(attempts int) time.Duration {
	d := r.backoffBase * time.Duration(attempts)
	if d < r.backoffBase {
		d = r.backoffBase
	}
	if d > 60*time.Second {
		d = 60 * time.Second
	}
	return d
}

func segmentsOf(body string) int {
	_, seg := billing.SegmentInfo(body)
	return seg
}

func sleepCtxQ(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
