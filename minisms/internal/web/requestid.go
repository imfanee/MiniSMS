package web

import (
	"context"
	"net/http"

	"github.com/google/uuid"
)

type requestIDKeyType int

const requestIDKey requestIDKeyType = 9

func WithRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := uuid.NewString()
		ctx := context.WithValue(r.Context(), requestIDKey, id)
		w.Header().Set("X-Request-ID", id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func RequestIDFromContext(ctx context.Context) string {
	v := ctx.Value(requestIDKey)
	if v == nil {
		return ""
	}
	s, _ := v.(string)
	return s
}
