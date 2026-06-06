// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package db

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// CarrierSMPPEgress is the configuration for an outbound SMPP ESME session.
type CarrierSMPPEgress struct {
	CarrierID           string
	EgressTransport     string
	SMPPHost            string
	SMPPPort            int
	SMPPSystemID        string
	SMPPPasswordEnc     string
	SMPPSystemType      *string
	SMPPBindMode        string
	SMPPTLS             bool
	SMPPEnquireLinkS    int
	SMPPWindowSize      int
	SMPPThroughputPerS int
}

func ListCarriersSMPPEgress(ctx context.Context, pool *pgxpool.Pool) ([]CarrierSMPPEgress, error) {
	rows, err := pool.Query(ctx, `
		SELECT carrier_id::text, egress_transport, smpp_host, smpp_port, smpp_system_id, smpp_password_enc,
			smpp_system_type, smpp_bind_mode, smpp_tls, smpp_enquire_link_s, smpp_window_size, smpp_throughput_per_s
		FROM carriers
		WHERE status = 'active'
		  AND egress_transport = 'smpp'
		  AND smpp_host IS NOT NULL AND trim(smpp_host) <> ''
		  AND smpp_port IS NOT NULL
		  AND smpp_system_id IS NOT NULL AND trim(smpp_system_id) <> ''
		  AND smpp_password_enc IS NOT NULL AND trim(smpp_password_enc) <> ''
		  AND smpp_bind_mode IN ('tx', 'trx')`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CarrierSMPPEgress
	for rows.Next() {
		var c CarrierSMPPEgress
		if err := rows.Scan(
			&c.CarrierID, &c.EgressTransport, &c.SMPPHost, &c.SMPPPort, &c.SMPPSystemID, &c.SMPPPasswordEnc,
			&c.SMPPSystemType, &c.SMPPBindMode, &c.SMPPTLS, &c.SMPPEnquireLinkS, &c.SMPPWindowSize, &c.SMPPThroughputPerS,
		); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func UpdateCarrierSMPPStatus(ctx context.Context, pool *pgxpool.Pool, carrierID, status string) error {
	_, err := pool.Exec(ctx, `
		UPDATE carriers SET smpp_status = $2, updated_at = now()
		WHERE carrier_id = $1::uuid`,
		carrierID, status)
	return err
}

func ResolveSMSLogMessageID(ctx context.Context, pool *pgxpool.Pool, carrierID, ref string) (string, error) {
	var messageID string
	err := pool.QueryRow(ctx, `
		SELECT message_id::text
		FROM sms_logs
		WHERE carrier_id = $1::uuid
		  AND (message_id::text = $2 OR carrier_message_id = $2)
		ORDER BY received_at DESC
		LIMIT 1`,
		carrierID, ref).Scan(&messageID)
	return messageID, err
}
