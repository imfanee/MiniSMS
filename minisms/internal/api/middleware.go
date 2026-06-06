// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package api

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/minisms/minisms/internal/db"
	"golang.org/x/time/rate"
)

type contextKey string

const authedClientKey contextKey = "api.authedClient"

type keyLimiter struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

var limiterStore sync.Map

func APIClientFromContext(ctx context.Context) *db.Client {
	v := ctx.Value(authedClientKey)
	if v == nil {
		return nil
	}
	c, _ := v.(*db.Client)
	return c
}

func APIKeyAuth(pool *pgxpool.Pool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rawKey := apiKeyFromRequest(r)
			if rawKey == "" {
				writeJSONError(w, http.StatusUnauthorized, "SMS_ERR_UNAUTHORIZED", "missing api key")
				return
			}
			client, err := db.ValidateAPIKey(r.Context(), pool, rawKey)
			if err != nil || client == nil {
				writeJSONError(w, http.StatusUnauthorized, "SMS_ERR_UNAUTHORIZED", "invalid api key")
				return
			}
			if client.Status != "active" {
				writeJSONError(w, http.StatusForbidden, "SMS_ERR_FORBIDDEN", "client is not active")
				return
			}
			limit := readRateLimitPerMinute(r.Context(), pool)
			if !allowForClient(client.ClientID, limit) {
				writeJSONError(w, http.StatusTooManyRequests, "SMS_ERR_RATE_LIMITED", "rate limit exceeded")
				return
			}
			ctx := context.WithValue(r.Context(), authedClientKey, client)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func apiKeyFromRequest(r *http.Request) string {
	if v := strings.TrimSpace(r.Header.Get("X-API-Key")); v != "" {
		return v
	}
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if strings.HasPrefix(strings.ToLower(auth), "bearer ") {
		return strings.TrimSpace(auth[7:])
	}
	return ""
}

func readRateLimitPerMinute(ctx context.Context, pool *pgxpool.Pool) int {
	var v string
	err := pool.QueryRow(ctx, `SELECT value FROM system_settings WHERE key='api_rate_limit_per_minute'`).Scan(&v)
	if err != nil {
		return 60
	}
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || n <= 0 {
		return 60
	}
	return n
}

func allowForClient(clientID string, perMinute int) bool {
	now := time.Now()
	val, ok := limiterStore.Load(clientID)
	if !ok {
		lim := &keyLimiter{
			limiter:  rate.NewLimiter(rate.Every(time.Minute/time.Duration(perMinute)), perMinute),
			lastSeen: now,
		}
		limiterStore.Store(clientID, lim)
		cleanupLimiters(now)
		return lim.limiter.Allow()
	}
	kl := val.(*keyLimiter)
	kl.lastSeen = now
	return kl.limiter.Allow()
}

func cleanupLimiters(now time.Time) {
	limiterStore.Range(func(key, value any) bool {
		kl, _ := value.(*keyLimiter)
		if kl == nil {
			limiterStore.Delete(key)
			return true
		}
		if now.Sub(kl.lastSeen) > time.Hour {
			limiterStore.Delete(key)
		}
		return true
	})
}
