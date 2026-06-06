// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package web

import (
	"context"

	"github.com/minisms/minisms/internal/models"
)

type ctxKey int

const sessionKey ctxKey = 1

// WithSession returns a context that carries the authenticated session.
func WithSession(ctx context.Context, s *models.AdminSession) context.Context {
	return context.WithValue(ctx, sessionKey, s)
}

// SessionFromContext returns the session or nil.
func SessionFromContext(ctx context.Context) *models.AdminSession {
	v := ctx.Value(sessionKey)
	if v == nil {
		return nil
	}
	s, _ := v.(*models.AdminSession)
	return s
}
