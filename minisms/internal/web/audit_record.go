// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package web

import (
	"context"
	"net/http"

	"github.com/minisms/minisms/internal/db"
)

// recordAudit writes an audit_log row; failures are logged and do not fail the request.
func (h *Handlers) recordAudit(r *http.Request, action, entityType string, entityID, entityName *string, payload any) {
	h.recordAuditSession(r, nil, nil, action, entityType, entityID, entityName, payload)
}

func (h *Handlers) recordAuditSession(r *http.Request, sessionID, adminUserID *string, action, entityType string, entityID, entityName *string, payload any) {
	if h.Pool == nil {
		return
	}
	sid := sessionID
	if sid == nil {
		if s := SessionFromContext(r.Context()); s != nil && s.SessionID != "" {
			sid = &s.SessionID
		}
	}
	uid := adminUserID
	if uid == nil {
		uid = adminUserIDFromContext(r.Context())
	}
	ip := ClientIPString(r)
	var ipPtr *string
	if ip != "" {
		ipPtr = &ip
	}
	if err := db.InsertAuditLog(r.Context(), h.Pool, db.AuditLogInsert{
		SessionID:   sid,
		AdminUserID: uid,
		Action:      action,
		EntityType:  entityType,
		EntityID:    entityID,
		EntityName:  entityName,
		Payload:     payload,
		IPAddress:   ipPtr,
	}); err != nil && h.Log != nil {
		h.Log.Warn("audit log insert failed", "err", err, "action", action)
	}
}

func adminUserIDFromContext(ctx context.Context) *string {
	u := AdminFromContext(ctx)
	if u == nil || u.AdminUserID == "" {
		return nil
	}
	id := u.AdminUserID
	return &id
}
