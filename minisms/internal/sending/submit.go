// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package sending

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/minisms/minisms/internal/billing"
	"github.com/minisms/minisms/internal/carrier"
	"github.com/minisms/minisms/internal/db"
	"github.com/minisms/minisms/internal/smslog"
)

// SubmitParams is the input to the shared send pipeline.
type SubmitParams struct {
	Client          *db.Client
	Message         AcceptedMessage
	SenderID        carrier.SenderIDResolution
	DispatchTimeout time.Duration
}

// Submit runs the full prepaid send transaction (pending log → dispatch → debit → accept).
// When the async send queue is enabled it instead reserves the charge and queues
// the message for a worker to dispatch, so a transient egress rebind never rejects
// the message (see enqueue / QueueRunner in queue.go).
func (s *Service) Submit(ctx context.Context, p SubmitParams) SubmitOutcome {
	if s.Config != nil && s.Config.SendQueueEnabled {
		return s.enqueue(ctx, p)
	}
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

	timeout := p.DispatchTimeout
	if timeout <= 0 {
		timeoutS, _ := strconv.Atoi(db.Setting(ctx, s.Pool, "carrier_dispatch_timeout_s", strconv.Itoa(s.Config.CarrierDispatchTimeoutSecs)))
		if timeoutS < 1 {
			timeoutS = s.Config.CarrierDispatchTimeoutSecs
		}
		timeout = time.Duration(timeoutS) * time.Second
	}

	// Phase 1 - reserve: debit the client under a short SELECT FOR UPDATE tx and write a 'sending'
	// log, then COMMIT. The pool connection and the client-row lock are released here, BEFORE any
	// carrier contact, so sends never serialize on the client row and a slow carrier never ties up a
	// connection (that is the whole reason the queue path exists; the sync path now behaves the same).
	// No-oversell is preserved by the FOR UPDATE + the DB CHECK(balance>=0); a crash before finalize
	// leaves a 'sending' row that RecoverStuckSyncSends refunds on the next boot.
	logID, remaining, reserveErr := s.reserveSend(ctx, client, msg, rateEntry, encoding, segments, totalCharge, routeEntry, p.SenderID.Source)
	if reserveErr != nil {
		return *reserveErr
	}

	// Phase 2 - dispatch with NO transaction and NO row lock held.
	win, dispatchErr := s.dispatchWithFailover(ctx, logID, client.ClientID, msg, routeEntry, rateEntry.RatePerSMS, p.SenderID, timeout)

	// Phase 3 - finalize billing in a short tx.
	if dispatchErr != nil {
		return s.finalizeSendFailed(ctx, logID, client, totalCharge, win)
	}
	return s.finalizeSendAccepted(ctx, logID, client, msg, segments, totalCharge, remaining, rateEntry.RatePerSMS, win, p)
}

// reserveSend creates the sms_log and reserves (debits) the client charge under one short
// FOR UPDATE transaction, then commits so neither the connection nor the client-row lock is held
// during carrier dispatch. Returns the message id and the client's remaining balance, or a non-nil
// SubmitOutcome describing why the reservation failed (insufficient balance / DB error).
func (s *Service) reserveSend(ctx context.Context, client *db.Client, msg AcceptedMessage, rateEntry *billing.RateEntry, encoding string, segments int, totalCharge string, routeEntry *RouteEntry, senderIDSource string) (string, string, *SubmitOutcome) {
	ttl := time.Duration(s.Config.SendMessageTTLSecs) * time.Second
	if ttl <= 0 {
		ttl = time.Hour
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return "", "", &SubmitOutcome{Kind: OutcomeTemporaryUnavailable, TemporaryUnavailable: "database unavailable"}
	}
	defer func() { _ = tx.Rollback(ctx) }()

	balance, enough, err := lockAndCheckBalance(ctx, tx, client.ClientID, totalCharge)
	if err != nil {
		return "", "", &SubmitOutcome{Kind: OutcomeTemporaryUnavailable, TemporaryUnavailable: "failed to read balance"}
	}
	if !enough {
		return "", "", &SubmitOutcome{Kind: OutcomeInsufficientBalance, InsufficientBalance: &BalanceShortfall{Balance: balance, Required: totalCharge}}
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
		Status:           "sending",
		DLRRequested:     msg.DLRRequested,
		DLRWebhookURL:    msg.DLRWebhookURL,
		SenderIDSource:   senderIDSource,
		IngressTransport: ingressTransportString(msg.IngressTransport),
	})
	if err != nil {
		return "", "", &SubmitOutcome{Kind: OutcomeTemporaryUnavailable, TemporaryUnavailable: "failed to create sms log"}
	}
	if err := db.StampSending(ctx, tx, logID, ttl); err != nil {
		return "", "", &SubmitOutcome{Kind: OutcomeTemporaryUnavailable, TemporaryUnavailable: "failed to reserve message"}
	}
	remaining, err := billing.DeductClientBalance(ctx, tx, client.ClientID, totalCharge, logID, client.Currency)
	if err != nil {
		return "", "", &SubmitOutcome{Kind: OutcomeInsufficientBalance, InsufficientBalance: &BalanceShortfall{Balance: balance, Required: totalCharge}}
	}
	_ = db.SetSMSEventTimeline(ctx, tx, logID, []smslog.TimelineEvent{{
		At:     time.Now().UTC().Format(time.RFC3339),
		Kind:   smslog.EventRequestReceived,
		Title:  "SMS request received",
		Detail: fmt.Sprintf("Ingress: %s", strings.ToUpper(ingressTransportString(msg.IngressTransport))),
		Meta:   map[string]any{"to": msg.To, "from": msg.From, "segments": segments, "client_ref": msg.ClientRef},
	}})
	if err := tx.Commit(ctx); err != nil {
		return "", "", &SubmitOutcome{Kind: OutcomeTemporaryUnavailable, TemporaryUnavailable: "transaction commit failed"}
	}
	return logID, remaining, nil
}

// finalizeSendAccepted bills the carrier and marks the message accepted in one short tx (dispatch has
// already happened with no tx held). If any billing step fails the message is force-marked accepted so
// a carrier-accepted message is never lost or re-dispatched (the client is already charged; a rare
// carrier under-charge is far safer than a double-send).
func (s *Service) finalizeSendAccepted(ctx context.Context, logID string, client *db.Client, msg AcceptedMessage, segments int, totalCharge, remaining, clientRate string, win *dispatchOutcome, p SubmitParams) SubmitOutcome {
	accepted := SubmitOutcome{
		Kind: OutcomeAccepted,
		Accepted: &AcceptedPayload{
			MessageID:        logID,
			ClientRef:        msg.ClientRef,
			SenderID:         p.SenderID.Value,
			SenderIDSource:   p.SenderID.Source,
			Segments:         segments,
			Charged:          totalCharge,
			BalanceRemaining: remaining,
			Carrier:          win.CarrierName,
			FailoverSequence: win.FailoverSequence,
			SourceAddrTON:    win.SourceAddrTON,
			SourceAddrNPI:    win.SourceAddrNPI,
			DestAddrTON:      win.DestAddrTON,
			DestAddrNPI:      win.DestAddrNPI,
			DLRRequested:     msg.DLRRequested,
			DLRWebhookURL:    msg.DLRWebhookURL,
		},
	}
	// Carrier cost is read on Pool BEFORE Begin, so the tx never acquires a second connection.
	carrierCostRate, err := billing.LookupCarrierCost(ctx, s.Pool, win.CarrierID, msg.To, clientRate)
	if err != nil {
		s.forceAccepted(ctx, logID, win, "carrier cost lookup failed")
		return accepted
	}
	carrierCostTotal, err := mulNumeric(ctx, s.Pool, carrierCostRate, segments)
	if err != nil {
		s.forceAccepted(ctx, logID, win, "carrier cost compute failed")
		return accepted
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		s.forceAccepted(ctx, logID, win, "billing tx begin failed")
		return accepted
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if len(win.SkipReasons) > 0 {
		if skipJSON, e := json.Marshal(win.SkipReasons); e == nil {
			_ = updateSMSLogCarrierSkipReason(ctx, tx, logID, skipJSON)
		}
	}
	var smppArr *[4]int16
	if win.SourceAddrTON != nil && win.SourceAddrNPI != nil && win.DestAddrTON != nil && win.DestAddrNPI != nil {
		tmp := [4]int16{*win.SourceAddrTON, *win.SourceAddrNPI, *win.DestAddrTON, *win.DestAddrNPI}
		smppArr = &tmp
	}
	_, e1 := billing.DeductCarrierBalance(ctx, tx, win.CarrierID, carrierCostTotal, client.Currency, logID)
	e2 := billing.IncrementUsage(ctx, tx, win.CarrierID, segments, carrierCostTotal)
	e3 := db.MarkSMSAccepted(ctx, tx, logID, win.CarrierID, win.FailoverSequence, win.CarrierMessageID, win.LastBodyText, win.StatusCode, smppArr, egressTransportString(win.Egress))
	e4 := db.AppendSMSEventTimeline(ctx, tx, logID, win.Timeline...)
	if e1 == nil && e2 == nil && e3 == nil && e4 == nil {
		if err := tx.Commit(ctx); err == nil {
			return accepted
		}
	}
	s.forceAccepted(ctx, logID, win, "billing tx failed")
	return accepted
}

func (s *Service) forceAccepted(ctx context.Context, logID string, win *dispatchOutcome, reason string) {
	slog.Warn("sync send finalize billing failed, forcing accepted", "message_id", logID, "carrier_id", win.CarrierID, "reason", reason)
	_ = db.ForceMarkAccepted(ctx, s.Pool, logID, win.CarrierID, win.CarrierMessageID, egressTransportString(win.Egress))
}

// finalizeSendFailed refunds the reserved client charge (when refund_on_carrier_failure is on), marks
// the message failed, and returns the failure outcome. Runs in a short tx after dispatch. If the tx
// cannot be committed the row stays 'sending' and boot recovery refunds it, so the client is never
// left charged for an undelivered message.
func (s *Service) finalizeSendFailed(ctx context.Context, logID string, client *db.Client, totalCharge string, win *dispatchOutcome) SubmitOutcome {
	refundOnFailure := db.Setting(ctx, s.Pool, "refund_on_carrier_failure", "true")
	allSkipped := len(win.SkipReasons) > 0 && win.LastCode == nil
	outcome := SubmitOutcome{Kind: OutcomeCarrierFailure, CarrierFailure: &CarrierFailurePayload{MessageID: logID}}
	if allSkipped {
		outcome = SubmitOutcome{Kind: OutcomeNoEligibleCarrier, NoEligibleCarrier: "All carriers skipped due to Sender ID policy or in-loss protection"}
	}

	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return outcome
	}
	defer func() { _ = tx.Rollback(ctx) }()

	events := append([]smslog.TimelineEvent{}, win.Timeline...)
	events = append(events, smslog.NewEvent(smslog.EventDispatchFailed, "All carrier attempts failed", win.LastBody, map[string]any{"last_http_status": win.LastCode}))
	_ = db.AppendSMSEventTimeline(ctx, tx, logID, events...)
	if len(win.SkipReasons) > 0 {
		if skipJSON, e := json.Marshal(win.SkipReasons); e == nil {
			_ = updateSMSLogCarrierSkipReason(ctx, tx, logID, skipJSON)
		}
	}
	if allSkipped {
		_ = db.MarkSMSFailed(ctx, tx, logID, nil, "all carriers skipped by policy")
	} else {
		_ = db.MarkSMSFailed(ctx, tx, logID, win.LastCode, win.LastBody)
	}
	if strings.EqualFold(refundOnFailure, "true") {
		_, _ = billing.CreditClientBalance(ctx, tx, client.ClientID, totalCharge, client.Currency, "carrier_failure_refund", "Automatic refund on carrier failure")
	}
	_ = tx.Commit(ctx)
	return outcome
}
