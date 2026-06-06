// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package egress

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/fiorix/go-smpp/v2/smpp"
	"github.com/fiorix/go-smpp/v2/smpp/pdu"
	"github.com/fiorix/go-smpp/v2/smpp/pdu/pdufield"
	"github.com/fiorix/go-smpp/v2/smpp/pdu/pdutext"
	"golang.org/x/time/rate"
)

// SubmitRequest is one logical outbound message (may encode to multiple submit_sm PDUs).
type SubmitRequest struct {
	Src, Dst, Body string
	SourceTON      uint8
	SourceNPI      uint8
	DestTON        uint8
	DestNPI        uint8
	Encoding       string
	Segments       int
	Timeout        time.Duration
}

// SubmitResult is the outcome of an SMPP submit_sm (or long-message sequence).
type SubmitResult struct {
	CommandStatus    int
	CarrierMessageID string
	ResponseBody     string
}

type submitter interface {
	Submit(sm *smpp.ShortMessage) (*smpp.ShortMessage, error)
	SubmitLongMsg(sm *smpp.ShortMessage) ([]smpp.ShortMessage, error)
}

func submitOn(ctx context.Context, sub submitter, lim *rate.Limiter, req SubmitRequest) (*SubmitResult, error) {
	if lim != nil {
		if err := lim.Wait(ctx); err != nil {
			return nil, err
		}
	}
	text := encodeText(req.Encoding, req.Body)
	sm := &smpp.ShortMessage{
		Src:           req.Src,
		Dst:           stripPlus(req.Dst),
		Text:          text,
		SourceAddrTON: req.SourceTON,
		SourceAddrNPI: req.SourceNPI,
		DestAddrTON:   req.DestTON,
		DestAddrNPI:   req.DestNPI,
		Register:      pdufield.FinalDeliveryReceipt,
	}
	if req.Segments > 1 {
		return submitLong(ctx, sub, lim, sm, req)
	}
	return submitOne(ctx, sub, sm, req.Timeout)
}

func submitLong(ctx context.Context, sub submitter, lim *rate.Limiter, sm *smpp.ShortMessage, req SubmitRequest) (*SubmitResult, error) {
	parts, err := sub.SubmitLongMsg(sm)
	if err != nil {
		return mapSubmitError(err)
	}
	if len(parts) == 0 {
		return nil, fmt.Errorf("smpp long submit returned no parts")
	}
	last := parts[len(parts)-1]
	return &SubmitResult{
		CommandStatus:    0,
		CarrierMessageID: last.RespID(),
		ResponseBody:     fmt.Sprintf("smpp long submit ok parts=%d", len(parts)),
	}, nil
}

func submitOne(ctx context.Context, sub submitter, sm *smpp.ShortMessage, timeout time.Duration) (*SubmitResult, error) {
	type res struct {
		out *SubmitResult
		err error
	}
	ch := make(chan res, 1)
	go func() {
		out, err := sub.Submit(sm)
		if err != nil {
			ch <- res{err: err}
			return
		}
		ch <- res{out: &SubmitResult{
			CommandStatus:    0,
			CarrierMessageID: out.RespID(),
			ResponseBody:     "smpp submit_sm ok",
		}}
	}()
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case r := <-ch:
		if r.err != nil {
			return mapSubmitError(r.err)
		}
		return r.out, nil
	case <-time.After(timeout):
		return nil, smpp.ErrTimeout
	}
}

func mapSubmitError(err error) (*SubmitResult, error) {
	if st, ok := err.(pdu.Status); ok {
		return &SubmitResult{
			CommandStatus: int(st),
			ResponseBody:  fmt.Sprintf("smpp status %s", st),
		}, nil
	}
	return nil, err
}

func encodeText(encoding, body string) pdutext.Codec {
	if strings.EqualFold(encoding, "UCS2") {
		return pdutext.UCS2(body)
	}
	return pdutext.GSM7(body)
}

func stripPlus(e164 string) string {
	return strings.TrimPrefix(strings.TrimSpace(e164), "+")
}
