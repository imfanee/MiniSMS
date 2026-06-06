// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package db

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Setting returns system_settings.value for key, or def when missing or empty.
func Setting(ctx context.Context, pool *pgxpool.Pool, key, def string) string {
	var v string
	err := pool.QueryRow(ctx, `SELECT value FROM system_settings WHERE key = $1`, key).Scan(&v)
	if err != nil {
		return def
	}
	v = strings.TrimSpace(v)
	if v == "" {
		return def
	}
	return v
}
