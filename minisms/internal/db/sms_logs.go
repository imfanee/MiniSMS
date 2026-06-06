// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package db

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type SMSLog struct {
	MessageID           string
	ClientID            string
	ClientRef           *string
	ToNumber            string
	FromNumber          *string
	MessageBody         string
	MessageLength       int
	Segments            int
	Encoding            string
	RateGroupID         *string
	PrefixMatched       *string
	RateApplied         string
	TotalCharged        string
	Currency            string
	RoutingGroupID      *string
	RouteEntryID        *string
	FailoverSequence    int
	CarrierID           *string
	CarrierMessageID    *string
	CarrierResponseCode *int
	CarrierResponseBody *string
	Status              string
	DLRRequested        bool
	DLRWebhookURL       *string
	DLRStatus           *string
	DLRReceivedAt       *time.Time
	DLRForwardedAt      *time.Time
	DLRForwardStatus    *string
	DLRForwardAttempts  int
	SourceAddrTON       *int16
	SourceAddrNPI       *int16
	DestAddrTON         *int16
	DestAddrNPI         *int16
	SenderIDSource      string
	IngressTransport    string
	EgressTransport     string
}

func CreateSMSLog(ctx context.Context, tx pgx.Tx, in SMSLog) (string, error) {
	var id string
	err := tx.QueryRow(ctx, `
		INSERT INTO sms_logs (
			client_id, client_ref, to_number, from_number, message_body, message_length, segments, encoding,
			rate_group_id, prefix_matched, rate_applied, total_charged, currency,
			routing_group_id, route_entry_id, status, dlr_requested, dlr_webhook_url,
			source_addr_ton, source_addr_npi, dest_addr_ton, dest_addr_npi, sender_id_source,
			ingress_transport
		) VALUES (
			$1::uuid, $2, $3, $4, $5, $6, $7, $8,
			$9::uuid, $10, $11::numeric(18,6), $12::numeric(18,6), $13::char(3),
			$14::uuid, $15::uuid, $16, $17, $18,
			$19, $20, $21, $22, $23,
			COALESCE(NULLIF($24, ''), 'http')
		)
		RETURNING message_id::text`,
		in.ClientID, in.ClientRef, in.ToNumber, in.FromNumber, in.MessageBody, in.MessageLength, in.Segments, in.Encoding,
		in.RateGroupID, in.PrefixMatched, in.RateApplied, in.TotalCharged, in.Currency,
		in.RoutingGroupID, in.RouteEntryID, in.Status, in.DLRRequested, in.DLRWebhookURL,
		in.SourceAddrTON, in.SourceAddrNPI, in.DestAddrTON, in.DestAddrNPI, in.SenderIDSource,
		in.IngressTransport,
	).Scan(&id)
	return id, err
}

func MarkSMSAccepted(ctx context.Context, tx pgx.Tx, messageID, carrierID string, failoverSequence int, carrierMessageID, carrierResponseBody string, carrierResponseCode int, tonNPI *[4]int16, egressTransport string) error {
	var sourceTON, sourceNPI, destTON, destNPI *int16
	if tonNPI != nil {
		sourceTON, sourceNPI, destTON, destNPI = &tonNPI[0], &tonNPI[1], &tonNPI[2], &tonNPI[3]
	}
	_, err := tx.Exec(ctx, `
		UPDATE sms_logs
		SET status='accepted',
			carrier_id=$1::uuid,
			failover_sequence=$2,
			carrier_message_id=$3,
			carrier_response_code=$4,
			carrier_response_body=$5,
			source_addr_ton=$7,
			source_addr_npi=$8,
			dest_addr_ton=$9,
			dest_addr_npi=$10,
			egress_transport=COALESCE(NULLIF($11, ''), 'http'),
			dispatched_at=now()
		WHERE message_id=$6::uuid`,
		carrierID, failoverSequence, carrierMessageID, carrierResponseCode, carrierResponseBody, messageID,
		sourceTON, sourceNPI, destTON, destNPI, egressTransport)
	return err
}

func MarkSMSFailed(ctx context.Context, tx pgx.Tx, messageID string, responseCode *int, responseBody string) error {
	_, err := tx.Exec(ctx, `
		UPDATE sms_logs
		SET status='failed',
			carrier_response_code=$1,
			carrier_response_body=$2,
			failed_at=now()
		WHERE message_id=$3::uuid`,
		responseCode, responseBody, messageID)
	return err
}

func GetSMSLogByID(ctx context.Context, pool *pgxpool.Pool, messageID string) (*SMSLog, error) {
	var s SMSLog
	err := pool.QueryRow(ctx, `
		SELECT message_id::text, client_id::text, client_ref, to_number, from_number, message_body, message_length, segments, encoding,
			rate_group_id::text, prefix_matched, rate_applied::text, total_charged::text, currency::text,
			routing_group_id::text, route_entry_id::text, failover_sequence, carrier_id::text, carrier_message_id,
			carrier_response_code, carrier_response_body, status
		FROM sms_logs WHERE message_id = $1::uuid`, messageID).
		Scan(
			&s.MessageID, &s.ClientID, &s.ClientRef, &s.ToNumber, &s.FromNumber, &s.MessageBody, &s.MessageLength, &s.Segments, &s.Encoding,
			&s.RateGroupID, &s.PrefixMatched, &s.RateApplied, &s.TotalCharged, &s.Currency,
			&s.RoutingGroupID, &s.RouteEntryID, &s.FailoverSequence, &s.CarrierID, &s.CarrierMessageID,
			&s.CarrierResponseCode, &s.CarrierResponseBody, &s.Status,
		)
	if err != nil {
		return nil, err
	}
	return &s, nil
}
