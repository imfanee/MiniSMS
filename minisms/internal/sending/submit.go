// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package sending

import (
	"context"
	"encoding/json"
	"fmt"
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
func (s *Service) Submit(ctx context.Context, p SubmitParams) SubmitOutcome {
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
		return SubmitOutcome{
			Kind: OutcomeInsufficientBalance,
			InsufficientBalance: &BalanceShortfall{Balance: balance, Required: totalCharge},
		}
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
		Status:           "pending",
		DLRRequested:     msg.DLRRequested,
		DLRWebhookURL:    msg.DLRWebhookURL,
		SenderIDSource:   p.SenderID.Source,
		IngressTransport: ingressTransportString(msg.IngressTransport),
	})
	if err != nil {
		return SubmitOutcome{Kind: OutcomeTemporaryUnavailable, TemporaryUnavailable: "failed to create sms log"}
	}

	timeline := []smslog.TimelineEvent{{
		At:     time.Now().UTC().Format(time.RFC3339),
		Kind:   smslog.EventRequestReceived,
		Title:  "SMS request received",
		Detail: fmt.Sprintf("Ingress: %s", strings.ToUpper(ingressTransportString(msg.IngressTransport))),
		Meta: map[string]any{
			"to": msg.To, "from": msg.From, "segments": segments, "client_ref": msg.ClientRef,
		},
	}}

	win, dispatchErr := s.dispatchWithFailover(ctx, logID, client.ClientID, msg, routeEntry, rateEntry.RatePerSMS, p.SenderID, timeout)
	timeline = append(timeline, win.Timeline...)
	if len(win.SkipReasons) > 0 {
		if skipJSON, err := json.Marshal(win.SkipReasons); err == nil {
			_ = updateSMSLogCarrierSkipReason(ctx, tx, logID, skipJSON)
		}
	}
	if dispatchErr != nil {
		timeline = append(timeline, smslog.NewEvent(
			smslog.EventDispatchFailed,
			"All carrier attempts failed",
			win.LastBody,
			map[string]any{"last_http_status": win.LastCode},
		))
		_ = db.SetSMSEventTimeline(ctx, tx, logID, timeline)
		if len(win.SkipReasons) > 0 && win.LastCode == nil {
			_ = db.MarkSMSFailed(ctx, tx, logID, nil, "all carriers skipped by policy")
			if err := tx.Commit(ctx); err != nil {
				return SubmitOutcome{Kind: OutcomeTemporaryUnavailable, TemporaryUnavailable: "failed to finalize skip state"}
			}
			return SubmitOutcome{Kind: OutcomeNoEligibleCarrier, NoEligibleCarrier: "All carriers skipped due to Sender ID policy or in-loss protection"}
		}
		_ = db.MarkSMSFailed(ctx, tx, logID, win.LastCode, win.LastBody)
		if strings.EqualFold(db.Setting(ctx, s.Pool, "refund_on_carrier_failure", "true"), "true") {
			_, _ = billing.CreditClientBalance(ctx, tx, client.ClientID, totalCharge, client.Currency, "carrier_failure_refund", "Automatic refund on carrier failure")
		}
		if err := tx.Commit(ctx); err != nil {
			return SubmitOutcome{Kind: OutcomeTemporaryUnavailable, TemporaryUnavailable: "failed to finalize failure state"}
		}
		return SubmitOutcome{
			Kind: OutcomeCarrierFailure,
			CarrierFailure: &CarrierFailurePayload{MessageID: logID},
		}
	}

	carrierCostRate, err := billing.LookupCarrierCost(ctx, s.Pool, win.CarrierID, msg.To, rateEntry.RatePerSMS)
	if err != nil {
		return SubmitOutcome{Kind: OutcomeTemporaryUnavailable, TemporaryUnavailable: "failed to determine carrier cost"}
	}
	carrierCostTotal, err := mulNumeric(ctx, s.Pool, carrierCostRate, segments)
	if err != nil {
		return SubmitOutcome{Kind: OutcomeTemporaryUnavailable, TemporaryUnavailable: "failed to compute carrier cost"}
	}

	remaining, err := billing.DeductClientBalance(ctx, tx, client.ClientID, totalCharge, logID, client.Currency)
	if err != nil {
		return SubmitOutcome{Kind: OutcomeInsufficientBalance, InsufficientBalance: &BalanceShortfall{Balance: balance, Required: totalCharge}}
	}
	if _, err := billing.DeductCarrierBalance(ctx, tx, win.CarrierID, carrierCostTotal, client.Currency, logID); err != nil {
		return SubmitOutcome{Kind: OutcomeTemporaryUnavailable, TemporaryUnavailable: "failed to deduct carrier balance"}
	}
	if err := billing.IncrementUsage(ctx, tx, win.CarrierID, segments, carrierCostTotal); err != nil {
		return SubmitOutcome{Kind: OutcomeTemporaryUnavailable, TemporaryUnavailable: "failed to increment carrier usage"}
	}
	var smppArr *[4]int16
	if win.SourceAddrTON != nil && win.SourceAddrNPI != nil && win.DestAddrTON != nil && win.DestAddrNPI != nil {
		tmp := [4]int16{*win.SourceAddrTON, *win.SourceAddrNPI, *win.DestAddrTON, *win.DestAddrNPI}
		smppArr = &tmp
	}
	if err := db.MarkSMSAccepted(ctx, tx, logID, win.CarrierID, win.FailoverSequence, win.CarrierMessageID, win.LastBodyText, win.StatusCode, smppArr, egressTransportString(win.Egress)); err != nil {
		return SubmitOutcome{Kind: OutcomeTemporaryUnavailable, TemporaryUnavailable: "failed to update sms log"}
	}
	if err := db.SetSMSEventTimeline(ctx, tx, logID, timeline); err != nil {
		return SubmitOutcome{Kind: OutcomeTemporaryUnavailable, TemporaryUnavailable: "failed to persist event timeline"}
	}
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
}
