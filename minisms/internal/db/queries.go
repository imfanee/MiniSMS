// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package db

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/minisms/minisms/internal/models"
)

// NewSessionToken returns 32 random bytes and their SHA-256 hash as a hex string (for session_token column).
func NewSessionToken() (rawToken [32]byte, tokenHashHex string, err error) {
	if _, err = rand.Read(rawToken[:]); err != nil {
		return rawToken, "", fmt.Errorf("session entropy: %w", err)
	}
	sum := sha256.Sum256(rawToken[:])
	return rawToken, hex.EncodeToString(sum[:]), nil
}

// HashTokenHex returns the SHA-256 of the given raw token, hex-encoded (must match how sessions are looked up).
func HashTokenHex(raw [32]byte) string {
	sum := sha256.Sum256(raw[:])
	return hex.EncodeToString(sum[:])
}

// CreateAdminSession stores a new session; raw token is 32 bytes (cookie will carry hex of this).
// Returns the new session_id for audit logging.
func CreateAdminSession(ctx context.Context, pool *pgxpool.Pool, adminUserID, tokenHashHex string, sessionIdle time.Duration, clientIP, userAgent *string) (sessionID string, err error) {
	now := time.Now()
	exp := now.Add(sessionIdle)
	err = pool.QueryRow(ctx, `
		INSERT INTO admin_sessions (session_token, expires_at, last_active_at, ip_address, user_agent, admin_user_id)
		VALUES ($1, $2, $3, $4::inet, $5, $6::uuid)
		RETURNING session_id::text`,
		tokenHashHex, exp, now, nullableIPString(clientIP), userAgent, adminUserID,
	).Scan(&sessionID)
	if err != nil {
		return "", fmt.Errorf("insert session: %w", err)
	}
	return sessionID, nil
}

func nullableIPString(ip *string) *string {
	if ip == nil || *ip == "" {
		return nil
	}
	if p := net.ParseIP(*ip); p == nil {
		return nil
	}
	return ip
}


// GetSessionByTokenHash returns the session or nil, nil if not found / revoked (caller checks time).
func GetSessionByTokenHash(ctx context.Context, pool *pgxpool.Pool, tokenHashHex string) (*models.AdminSession, error) {
	var s models.AdminSession
	var ipStr *string
	var ua *string
	err := pool.QueryRow(ctx, `
		SELECT
			session_id::text,
			session_token,
			admin_user_id::text,
			created_at,
			expires_at,
			last_active_at,
			ip_address::text,
			user_agent,
			is_revoked
		FROM admin_sessions
		WHERE session_token = $1 AND is_revoked = false`,
		tokenHashHex,
	).Scan(
		&s.SessionID, &s.SessionToken, &s.AdminUserID, &s.CreatedAt, &s.ExpiresAt, &s.LastActiveAt, &ipStr, &ua, &s.IsRevoked,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if ipStr != nil && *ipStr != "" {
		s.IPAddress = ipStr
	}
	s.UserAgent = ua
	return &s, nil
}

// UpdateSessionLastActive sets last_active_at to now and extends expires_at to now+idle.
func UpdateSessionLastActive(ctx context.Context, pool *pgxpool.Pool, sessionID string, sessionIdle time.Duration) error {
	now := time.Now()
	exp := now.Add(sessionIdle)
	ct, err := pool.Exec(ctx, `
		UPDATE admin_sessions
		SET last_active_at = $1, expires_at = $2
		WHERE session_id = $3::uuid AND is_revoked = false`,
		now, exp, sessionID,
	)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

// RevokeSession marks a session as revoked.
func RevokeSession(ctx context.Context, pool *pgxpool.Pool, tokenHashHex string) error {
	_, err := pool.Exec(ctx, `
		UPDATE admin_sessions
		SET is_revoked = true
		WHERE session_token = $1`, tokenHashHex)
	return err
}

// CountCarriers returns row count in carriers.
func CountCarriers(ctx context.Context, pool *pgxpool.Pool) (int64, error) {
	return countOne(ctx, pool, `SELECT COUNT(*) FROM carriers`)
}

// CountRateGroups returns row count in rate_groups.
func CountRateGroups(ctx context.Context, pool *pgxpool.Pool) (int64, error) {
	return countOne(ctx, pool, `SELECT COUNT(*) FROM rate_groups`)
}

// CountClients returns row count in clients.
func CountClients(ctx context.Context, pool *pgxpool.Pool) (int64, error) {
	return countOne(ctx, pool, `SELECT COUNT(*) FROM clients`)
}

// CountSMSSentToday returns SMS rows received today (UTC) with a terminal "sent" status.
func CountSMSSentToday(ctx context.Context, pool *pgxpool.Pool) (int64, error) {
	return countOne(ctx, pool, `
		SELECT COUNT(*)
		FROM sms_logs
		WHERE (received_at AT TIME ZONE 'UTC')::date = (now() AT TIME ZONE 'UTC')::date
		  AND status IN ('sent', 'delivered', 'accepted')`)
}

func countOne(ctx context.Context, pool *pgxpool.Pool, q string) (int64, error) {
	var n int64
	err := pool.QueryRow(ctx, q).Scan(&n)
	if err != nil {
		return 0, err
	}
	return n, nil
}
