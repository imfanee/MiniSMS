// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package sending

import (
	"github.com/minisms/minisms/internal/carrier"
	"github.com/minisms/minisms/internal/smslog"
)

// OutcomeKind classifies Submit results for ingress adapters (REST, SMPP).
type OutcomeKind int

const (
	OutcomeAccepted OutcomeKind = iota
	OutcomeInsufficientBalance
	OutcomeNoRate
	OutcomeNoRoute
	OutcomeNoEligibleCarrier
	OutcomeCarrierFailure
	OutcomeTemporaryUnavailable
)

// SubmitOutcome is the result of sending.Submit.
type SubmitOutcome struct {
	Kind OutcomeKind

	Accepted *AcceptedPayload

	InsufficientBalance *BalanceShortfall
	CarrierFailure      *CarrierFailurePayload
	TemporaryUnavailable string
	NoRate               string
	NoRoute              string
	NoEligibleCarrier    string
}

type AcceptedPayload struct {
	MessageID        string
	ClientRef        string
	SenderID         string
	SenderIDSource   string
	Segments         int
	Charged          string
	BalanceRemaining string
	Carrier          string
	FailoverSequence int
	SourceAddrTON    *int16
	SourceAddrNPI    *int16
	DestAddrTON      *int16
	DestAddrNPI      *int16
	DLRRequested     bool
	DLRWebhookURL    *string
}

type BalanceShortfall struct {
	Balance  string
	Required string
}

type CarrierFailurePayload struct {
	MessageID string
}

// dispatchOutcome is the internal winner from carrier failover.
type dispatchOutcome struct {
	CarrierID        string
	CarrierName      string
	FailoverSequence int
	StatusCode       int
	CarrierMessageID string
	LastCode         *int
	LastBody         string
	LastBodyText     string
	SkipReasons      []carrier.CarrierSkipReason
	Timeline         []smslog.TimelineEvent
	SourceAddrTON    *int16
	SourceAddrNPI    *int16
	DestAddrTON      *int16
	DestAddrNPI      *int16
	Egress           EgressTransport
}
