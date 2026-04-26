package db

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type APIKeyMeta struct {
	KeyID     string
	ClientID  string
	KeyPrefix string
	CreatedAt string
	RevokedAt *string
}

func GenerateAPIKey(ctx context.Context, pool *pgxpool.Pool, clientID string) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	rawKey := base64.RawURLEncoding.EncodeToString(raw)

	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	saltHex := hex.EncodeToString(salt)
	hashInput := append(salt, []byte(rawKey)...)
	sum := sha256.Sum256(hashInput)
	keyHash := hex.EncodeToString(sum[:])
	keyPrefix := rawKey
	if len(keyPrefix) > 8 {
		keyPrefix = keyPrefix[:8]
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	_, err = tx.Exec(ctx, `
		UPDATE client_api_keys
		SET revoked_at = now(), revoked_reason = 'superseded'
		WHERE client_id = $1::uuid AND revoked_at IS NULL`, clientID)
	if err != nil {
		return "", err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO client_api_keys (client_id, key_hash, key_salt, key_prefix, created_at)
		VALUES ($1::uuid, $2, $3, $4, now())`,
		clientID, keyHash, saltHex, keyPrefix,
	)
	if err != nil {
		return "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", err
	}
	return rawKey, nil
}

func GetActiveKey(ctx context.Context, pool *pgxpool.Pool, clientID string) (*APIKeyMeta, error) {
	var m APIKeyMeta
	var rt *time.Time
	err := pool.QueryRow(ctx, `
		SELECT key_id::text, client_id::text, key_prefix::text, to_char(created_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'), revoked_at
		FROM client_api_keys
		WHERE client_id = $1::uuid AND revoked_at IS NULL
		ORDER BY created_at DESC
		LIMIT 1`, clientID,
	).Scan(&m.KeyID, &m.ClientID, &m.KeyPrefix, &m.CreatedAt, &rt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if rt != nil {
		s := rt.UTC().Format(time.RFC3339)
		m.RevokedAt = &s
	}
	return &m, nil
}

func RevokeAPIKey(ctx context.Context, pool *pgxpool.Pool, clientID, reason string) error {
	if strings.TrimSpace(reason) == "" {
		reason = "admin_revoked"
	}
	_, err := pool.Exec(ctx, `
		UPDATE client_api_keys
		SET revoked_at = now(), revoked_reason = $2
		WHERE client_id = $1::uuid AND revoked_at IS NULL`, clientID, reason)
	return err
}

func ValidateAPIKey(ctx context.Context, pool *pgxpool.Pool, rawKey string) (*Client, error) {
	if len(rawKey) < 8 {
		return nil, errors.New("invalid api key")
	}
	prefix := rawKey[:8]
	rows, err := pool.Query(ctx, `
		SELECT cak.client_id::text, cak.key_hash, cak.key_salt
		FROM client_api_keys cak
		JOIN clients c ON c.client_id = cak.client_id
		WHERE cak.revoked_at IS NULL
		  AND cak.key_prefix = $1
		  AND c.status = 'active'`, prefix)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var clientID, keyHash, keySalt string
		if e := rows.Scan(&clientID, &keyHash, &keySalt); e != nil {
			return nil, e
		}
		salt, derr := hex.DecodeString(keySalt)
		if derr != nil {
			continue
		}
		sum := sha256.Sum256(append(salt, []byte(rawKey)...))
		expectedHash, derr := hex.DecodeString(keyHash)
		if derr != nil || len(expectedHash) != len(sum) {
			continue
		}
		if subtle.ConstantTimeCompare(sum[:], expectedHash) == 1 {
			return GetClient(ctx, pool, clientID)
		}
	}
	return nil, errors.New("invalid api key")
}
