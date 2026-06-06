// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package db

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

type CarrierSenderIDRow struct {
	CarrierSenderID      string
	CarrierID            string
	CarrierName          string
	SenderIDPolicy       string
	DefaultSenderIDValue *string
	SenderID             string
	SenderIDValue        string
	SenderIDType         string
	IsDefault            bool
	CreatedAt            string
}

func ListCarrierSenderIDs(ctx context.Context, pool *pgxpool.Pool, carrierID string) ([]CarrierSenderIDRow, error) {
	rows, err := pool.Query(ctx, `
		SELECT carrier_sender_id::text, carrier_id::text, carrier_name, sender_id_policy, default_sender_id_value, sender_id::text, sender_id_value, sender_id_type, is_default, created_at::text
		FROM v_carrier_sender_ids
		WHERE carrier_id = $1::uuid
		ORDER BY is_default DESC, sender_id_value ASC`, carrierID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CarrierSenderIDRow
	for rows.Next() {
		var x CarrierSenderIDRow
		if err := rows.Scan(&x.CarrierSenderID, &x.CarrierID, &x.CarrierName, &x.SenderIDPolicy, &x.DefaultSenderIDValue, &x.SenderID, &x.SenderIDValue, &x.SenderIDType, &x.IsDefault, &x.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

func AddCarrierSenderID(ctx context.Context, pool *pgxpool.Pool, carrierID, senderIDUUID string) error {
	_, err := pool.Exec(ctx, `INSERT INTO carrier_sender_ids (carrier_id, sender_id) VALUES ($1::uuid, $2::uuid)`, carrierID, senderIDUUID)
	return err
}

func RemoveCarrierSenderID(ctx context.Context, pool *pgxpool.Pool, carrierSenderIDUUID, carrierID string) error {
	_, err := pool.Exec(ctx, `DELETE FROM carrier_sender_ids WHERE carrier_sender_id = $1::uuid AND carrier_id = $2::uuid`, carrierSenderIDUUID, carrierID)
	return err
}

func SetCarrierSenderIDDefault(ctx context.Context, pool *pgxpool.Pool, carrierSenderIDUUID, carrierID string) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `UPDATE carrier_sender_ids SET is_default = FALSE WHERE carrier_id = $1::uuid`, carrierID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE carrier_sender_ids SET is_default = TRUE WHERE carrier_sender_id = $1::uuid AND carrier_id = $2::uuid`, carrierSenderIDUUID, carrierID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func GetAvailableSenderIDsForCarrier(ctx context.Context, pool *pgxpool.Pool, carrierID string) ([]SenderID, error) {
	rows, err := pool.Query(ctx, `
		SELECT si.sender_id::text, si.value, si.sender_id_type, si.description, si.is_active, si.created_at, si.updated_at
		FROM sender_ids si
		WHERE si.is_active = TRUE
		  AND NOT EXISTS (
		    SELECT 1 FROM carrier_sender_ids csid WHERE csid.carrier_id = $1::uuid AND csid.sender_id = si.sender_id
		  )
		ORDER BY si.value ASC`, carrierID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SenderID
	for rows.Next() {
		var s SenderID
		if err := rows.Scan(&s.SenderID, &s.Value, &s.SenderIDType, &s.Description, &s.IsActive, &s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

