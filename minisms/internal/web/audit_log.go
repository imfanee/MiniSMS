package web

import (
	"net/http"
	"time"

	"github.com/gorilla/csrf"
)

type auditLogRow struct {
	AuditID    string
	ActorType  string
	ActorID    *string
	Action     string
	EntityType *string
	EntityID   *string
	Meta       *string
	CreatedAt  time.Time
}

type auditLogPage struct {
	Title       string
	CurrentPath string
	CSRFToken   string
	Flash       *Flash
	Rows        []auditLogRow
}

func (h *Handlers) ListAuditLog() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := h.Pool.Query(r.Context(), `
			SELECT audit_id::text, actor_type, actor_id::text, action, entity_type, entity_id::text, metadata::text, created_at
			FROM audit_log
			ORDER BY created_at DESC
			LIMIT 500`)
		if err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		defer rows.Close()

		var out []auditLogRow
		for rows.Next() {
			var x auditLogRow
			if err := rows.Scan(&x.AuditID, &x.ActorType, &x.ActorID, &x.Action, &x.EntityType, &x.EntityID, &x.Meta, &x.CreatedAt); err != nil {
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
		if err := execT(w, h.AuditT, "base", p); err != nil {
			ServerError(w, r, err, h.Log, h.T500)
		}
	}
}

