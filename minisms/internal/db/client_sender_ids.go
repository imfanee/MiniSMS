package db

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

type ClientSenderIDRow struct {
	ClientSenderID string
	ClientID       string
	ClientName     string
	SenderID       string
	SenderIDValue  string
	SenderIDType   string
	IsDefault      bool
	CreatedAt      string
}

func ListClientSenderIDs(ctx context.Context, pool *pgxpool.Pool, clientID string) ([]ClientSenderIDRow, error) {
	rows, err := pool.Query(ctx, `
		SELECT client_sender_id::text, client_id::text, client_name, sender_id::text, sender_id_value, sender_id_type, is_default, created_at::text
		FROM v_client_sender_ids
		WHERE client_id = $1::uuid
		ORDER BY is_default DESC, sender_id_value ASC`, clientID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ClientSenderIDRow
	for rows.Next() {
		var x ClientSenderIDRow
		if err := rows.Scan(&x.ClientSenderID, &x.ClientID, &x.ClientName, &x.SenderID, &x.SenderIDValue, &x.SenderIDType, &x.IsDefault, &x.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

func AddClientSenderID(ctx context.Context, pool *pgxpool.Pool, clientID, senderIDUUID string) error {
	_, err := pool.Exec(ctx, `INSERT INTO client_sender_ids (client_id, sender_id) VALUES ($1::uuid, $2::uuid)`, clientID, senderIDUUID)
	return err
}

func RemoveClientSenderID(ctx context.Context, pool *pgxpool.Pool, clientSenderIDUUID, clientID string) error {
	_, err := pool.Exec(ctx, `DELETE FROM client_sender_ids WHERE client_sender_id = $1::uuid AND client_id = $2::uuid`, clientSenderIDUUID, clientID)
	return err
}

func SetClientSenderIDDefault(ctx context.Context, pool *pgxpool.Pool, clientSenderIDUUID, clientID string) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `UPDATE client_sender_ids SET is_default = FALSE WHERE client_id = $1::uuid`, clientID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE client_sender_ids SET is_default = TRUE WHERE client_sender_id = $1::uuid AND client_id = $2::uuid`, clientSenderIDUUID, clientID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func GetAvailableSenderIDsForClient(ctx context.Context, pool *pgxpool.Pool, clientID string) ([]SenderID, error) {
	rows, err := pool.Query(ctx, `
		SELECT si.sender_id::text, si.value, si.sender_id_type, si.description, si.is_active, si.created_at, si.updated_at
		FROM sender_ids si
		WHERE si.is_active = TRUE
		  AND NOT EXISTS (
		    SELECT 1 FROM client_sender_ids csi WHERE csi.client_id = $1::uuid AND csi.sender_id = si.sender_id
		  )
		ORDER BY si.value ASC`, clientID)
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

