package db

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrSenderIDInUse = errors.New("sender id referenced by client/carrier")

type SenderID struct {
	SenderID     string
	Value        string
	SenderIDType string
	Description  *string
	IsActive     bool
	CreatedAt    time.Time
	UpdatedAt    time.Time
	ClientCount  int
	CarrierCount int
}

func ListActiveSenderIDs(ctx context.Context, pool *pgxpool.Pool) ([]SenderID, error) {
	rows, err := pool.Query(ctx, `
		SELECT sender_id::text, value, sender_id_type, description, is_active, created_at, updated_at
		FROM sender_ids
		WHERE is_active = TRUE
		ORDER BY value`)
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

func ListAllSenderIDs(ctx context.Context, pool *pgxpool.Pool) ([]SenderID, error) {
	rows, err := pool.Query(ctx, `
		SELECT si.sender_id::text, si.value, si.sender_id_type, si.description, si.is_active, si.created_at, si.updated_at,
			COALESCE((SELECT COUNT(*) FROM client_sender_ids csi WHERE csi.sender_id = si.sender_id), 0)::int AS client_count,
			COALESCE((SELECT COUNT(*) FROM carrier_sender_ids csid WHERE csid.sender_id = si.sender_id), 0)::int AS carrier_count
		FROM sender_ids si
		ORDER BY si.value`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SenderID
	for rows.Next() {
		var s SenderID
		if err := rows.Scan(&s.SenderID, &s.Value, &s.SenderIDType, &s.Description, &s.IsActive, &s.CreatedAt, &s.UpdatedAt, &s.ClientCount, &s.CarrierCount); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func GetSenderID(ctx context.Context, pool *pgxpool.Pool, senderIDUUID string) (*SenderID, error) {
	var s SenderID
	err := pool.QueryRow(ctx, `
		SELECT sender_id::text, value, sender_id_type, description, is_active, created_at, updated_at
		FROM sender_ids
		WHERE sender_id = $1::uuid`, senderIDUUID).
		Scan(&s.SenderID, &s.Value, &s.SenderIDType, &s.Description, &s.IsActive, &s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func CreateSenderID(ctx context.Context, pool *pgxpool.Pool, in SenderID) (*SenderID, error) {
	var s SenderID
	err := pool.QueryRow(ctx, `
		INSERT INTO sender_ids (value, sender_id_type, description, is_active)
		VALUES ($1, $2, $3, COALESCE($4, TRUE))
		RETURNING sender_id::text, value, sender_id_type, description, is_active, created_at, updated_at`,
		in.Value, in.SenderIDType, in.Description, in.IsActive).
		Scan(&s.SenderID, &s.Value, &s.SenderIDType, &s.Description, &s.IsActive, &s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func UpdateSenderID(ctx context.Context, pool *pgxpool.Pool, in SenderID) error {
	_, err := pool.Exec(ctx, `
		UPDATE sender_ids
		SET value = $1, sender_id_type = $2, description = $3, updated_at = now()
		WHERE sender_id = $4::uuid`,
		in.Value, in.SenderIDType, in.Description, in.SenderID)
	return err
}

func ToggleSenderIDActive(ctx context.Context, pool *pgxpool.Pool, senderIDUUID string) error {
	var isActive bool
	if err := pool.QueryRow(ctx, `SELECT is_active FROM sender_ids WHERE sender_id = $1::uuid`, senderIDUUID).Scan(&isActive); err != nil {
		return err
	}
	if isActive {
		var refs int
		if err := pool.QueryRow(ctx, `
			SELECT
				COALESCE((SELECT COUNT(*) FROM client_sender_ids WHERE sender_id = $1::uuid),0) +
				COALESCE((SELECT COUNT(*) FROM carrier_sender_ids WHERE sender_id = $1::uuid),0)`,
			senderIDUUID).Scan(&refs); err != nil {
			return err
		}
		if refs > 0 {
			return ErrSenderIDInUse
		}
	}
	_, err := pool.Exec(ctx, `UPDATE sender_ids SET is_active = NOT is_active, updated_at = now() WHERE sender_id = $1::uuid`, senderIDUUID)
	return err
}
