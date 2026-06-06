// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package db

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// AuditLogInsert is one immutable audit_log row.
type AuditLogInsert struct {
	SessionID   *string
	AdminUserID *string
	Action      string
	EntityType  string
	EntityID    *string
	EntityName  *string
	Payload     any
	IPAddress   *string
}

// InsertAuditLog appends an audit_log entry.
func InsertAuditLog(ctx context.Context, pool *pgxpool.Pool, in AuditLogInsert) error {
	var payload []byte
	if in.Payload != nil {
		b, err := json.Marshal(in.Payload)
		if err != nil {
			return fmt.Errorf("audit payload json: %w", err)
		}
		payload = b
	}
	_, err := pool.Exec(ctx, `
		INSERT INTO audit_log (session_id, admin_user_id, action, entity_type, entity_id, entity_name, payload, ip_address)
		VALUES ($1::uuid, $2::uuid, $3, $4, $5::uuid, $6, $7::jsonb, $8::inet)`,
		in.SessionID, in.AdminUserID, in.Action, in.EntityType, in.EntityID, in.EntityName, payload, nullableIPString(in.IPAddress),
	)
	if err != nil {
		return fmt.Errorf("insert audit_log: %w", err)
	}
	return nil
}
