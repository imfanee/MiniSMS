// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package db

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type DLRMessage struct {
	MessageID                string
	ClientID                 string
	ClientRef                *string
	DLRDeliveryMode          string
	ToNumber                 string
	FromNumber               *string
	Segments                 int
	TotalCharged             string
	CarrierName              *string
	FailoverSequence         int
	ReceivedAt               time.Time
	Status                   string
	DLRStatus                *string
	DLRReceivedAt            *time.Time
	DLRRequested             bool
	DLRWebhookURL            *string
	DLRWebhookMethod         string
	DLRWebhookQueryTemplate  *string
	DLRWebhookBodyTemplate   *string
	SourceAddrTON            *int16
	SourceAddrNPI            *int16
	DestAddrTON              *int16
	DestAddrNPI              *int16
	ClientWebhookSecret      *string
	CarrierInboundSecret     *string
	CarrierDLRMessageIDField *string
	CarrierDLRStatusField    *string
	CarrierDLRStatusMap      map[string]string
}

func GetSMSLogForDLR(ctx context.Context, pool *pgxpool.Pool, messageID string) (*DLRMessage, error) {
	var m DLRMessage
	var statusMapRaw []byte
	err := pool.QueryRow(ctx, `
		SELECT sl.message_id::text, sl.client_id::text, sl.client_ref, sl.to_number, sl.from_number, sl.segments, sl.total_charged::text,
			ca.name, sl.failover_sequence, sl.received_at, sl.status, sl.dlr_status, sl.dlr_received_at,
			sl.dlr_requested, sl.dlr_webhook_url,
			sl.source_addr_ton, sl.source_addr_npi, sl.dest_addr_ton, sl.dest_addr_npi,
			c.dlr_webhook_secret, c.dlr_delivery_mode,
			COALESCE(c.dlr_webhook_method, 'POST'), c.dlr_webhook_query_template, c.dlr_webhook_body_template,
			ca.dlr_inbound_secret, ca.dlr_message_id_field, ca.dlr_status_field, ca.dlr_status_map
		FROM sms_logs sl
		JOIN clients c ON c.client_id = sl.client_id
		LEFT JOIN carriers ca ON ca.carrier_id = sl.carrier_id
		WHERE sl.message_id = $1::uuid`,
		messageID,
	).Scan(
		&m.MessageID, &m.ClientID, &m.ClientRef, &m.ToNumber, &m.FromNumber, &m.Segments, &m.TotalCharged,
			&m.CarrierName, &m.FailoverSequence, &m.ReceivedAt, &m.Status, &m.DLRStatus, &m.DLRReceivedAt,
			&m.DLRRequested, &m.DLRWebhookURL,
			&m.SourceAddrTON, &m.SourceAddrNPI, &m.DestAddrTON, &m.DestAddrNPI,
			&m.ClientWebhookSecret, &m.DLRDeliveryMode,
			&m.DLRWebhookMethod, &m.DLRWebhookQueryTemplate, &m.DLRWebhookBodyTemplate,
			&m.CarrierInboundSecret, &m.CarrierDLRMessageIDField, &m.CarrierDLRStatusField, &statusMapRaw,
	)
	if err != nil {
		return nil, err
	}
	if len(statusMapRaw) > 0 {
		var mp map[string]string
		if err := json.Unmarshal(statusMapRaw, &mp); err == nil {
			m.CarrierDLRStatusMap = mp
		}
	}
	return &m, nil
}

func UpdateDLRReceived(ctx context.Context, pool *pgxpool.Pool, messageID, dlrStatus string) error {
	_, err := pool.Exec(ctx, `
		UPDATE sms_logs
		SET dlr_status = $2,
			dlr_received_at = now(),
			status = CASE
				WHEN $2 = 'delivered' THEN 'delivered'
				WHEN $2 = 'undelivered' THEN 'failed'
				ELSE status
			END,
			delivered_at = CASE WHEN $2 = 'delivered' THEN now() ELSE delivered_at END
		WHERE message_id = $1::uuid`,
		messageID, dlrStatus,
	)
	return err
}

func UpdateDLRForwardStatus(ctx context.Context, pool *pgxpool.Pool, messageID, forwardStatus string, success bool, countAttempt bool) error {
	if success {
		_, err := pool.Exec(ctx, `
			UPDATE sms_logs
			SET dlr_forward_status = $2,
				dlr_forwarded_at = now(),
				dlr_forward_attempts = dlr_forward_attempts + 1
			WHERE message_id = $1::uuid`,
			messageID, forwardStatus,
		)
		return err
	}
	if countAttempt {
		_, err := pool.Exec(ctx, `
			UPDATE sms_logs
			SET dlr_forward_status = $2,
				dlr_forward_attempts = dlr_forward_attempts + 1
			WHERE message_id = $1::uuid`,
			messageID, forwardStatus,
		)
		return err
	}
	_, err := pool.Exec(ctx, `
		UPDATE sms_logs
		SET dlr_forward_status = $2
		WHERE message_id = $1::uuid`,
		messageID, forwardStatus,
	)
	return err
}

func GetSMSLogByMessageID(ctx context.Context, pool *pgxpool.Pool, messageID string) (*SMSLog, error) {
	return GetSMSLogByID(ctx, pool, messageID)
}

func IsNotFound(err error) bool {
	return err == pgx.ErrNoRows
}
