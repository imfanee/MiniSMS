// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package web

import (
	"net/http"
	"time"

	"github.com/gorilla/csrf"
)

type auditLogRow struct {
	AuditID      string
	ActorName    string
	ActorUsername string
	SessionID    *string
	Action       string
	EntityType   string
	EntityID     *string
	EntityName   *string
	Payload      *string
	IPAddress    *string
	CreatedAt    time.Time
}

type auditLogPage struct {
	AdminView
	Title       string
	CurrentPath string
	CSRFToken   string
	Flash       *Flash
	Rows        []auditLogRow
}

func (h *Handlers) ListAuditLog() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := h.Pool.Query(r.Context(), `
			SELECT
				a.audit_id::text,
				COALESCE(NULLIF(TRIM(au.display_name), ''), au.username, '—'),
				COALESCE(au.username, '—'),
				a.session_id::text,
				a.action,
				a.entity_type,
				a.entity_id::text,
				a.entity_name,
				a.payload::text,
				a.ip_address::text,
				a.created_at
			FROM audit_log a
			LEFT JOIN admin_users au ON au.admin_user_id = COALESCE(a.admin_user_id, (
				SELECT s.admin_user_id FROM admin_sessions s WHERE s.session_id = a.session_id LIMIT 1
			))
			ORDER BY a.created_at DESC
			LIMIT 500`)
		if err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		defer rows.Close()

		var out []auditLogRow
		for rows.Next() {
			var x auditLogRow
			if err := rows.Scan(
				&x.AuditID, &x.ActorName, &x.ActorUsername, &x.SessionID, &x.Action, &x.EntityType,
				&x.EntityID, &x.EntityName, &x.Payload, &x.IPAddress, &x.CreatedAt,
			); err != nil {
				ServerError(w, r, err, h.Log, h.T500)
				return
			}
			out = append(out, x)
		}
		if err := rows.Err(); err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}

		p := auditLogPage{
			Title:       "Audit Log",
			CurrentPath: "/admin/audit-log",
			CSRFToken:   csrf.Token(r),
			Flash:       GetFlash(w, r, "/", h.Config.SecretKey, h.Config.IsProduction()),
			Rows:        out,
		}
		if err := execT(w, h.AuditT, "base", p, r); err != nil {
			ServerError(w, r, err, h.Log, h.T500)
		}
	}
}
