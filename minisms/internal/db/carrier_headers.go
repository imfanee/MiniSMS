package db

import (
	"context"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgxpool"
)

// AuthHeaderRow is one auth header (decrypted value in memory for display assembly in handler).
type AuthHeaderRow struct {
	HeaderID   string
	HeaderName string
	Value      string
}

// ListAuthHeaders fetches and decrypts values for a carrier.
func ListAuthHeaders(ctx context.Context, pool *pgxpool.Pool, carrierID string, key32 []byte) ([]AuthHeaderRow, error) {
	rows, err := pool.Query(ctx, `
		SELECT header_id::text, header_name, header_value_enc
		FROM carrier_auth_headers
		WHERE carrier_id = $1::uuid
		ORDER BY header_name ASC`, carrierID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AuthHeaderRow
	for rows.Next() {
		var r AuthHeaderRow
		var enc string
		if e := rows.Scan(&r.HeaderID, &r.HeaderName, &enc); e != nil {
			return nil, e
		}
		pt, err := DecryptValue(key32, enc)
		if err != nil {
			return nil, err
		}
		r.Value = pt
		out = append(out, r)
	}
	return out, rows.Err()
}

// CreateAuthHeader inserts a header with encrypted value.
func CreateAuthHeader(ctx context.Context, pool *pgxpool.Pool, carrierID, headerName, plainValue string, key32 []byte) (string, error) {
	enc, err := EncryptValue(key32, plainValue)
	if err != nil {
		return "", err
	}
	var id string
	err = pool.QueryRow(ctx, `
		INSERT INTO carrier_auth_headers (carrier_id, header_name, header_value_enc)
		VALUES ($1::uuid, $2, $3)
		RETURNING header_id::text`, carrierID, headerName, enc,
	).Scan(&id)
	return id, err
}

// DeleteAuthHeader removes one header.
func DeleteAuthHeader(ctx context.Context, pool *pgxpool.Pool, carrierID, headerID string) (bool, error) {
	ct, err := pool.Exec(ctx, `
		DELETE FROM carrier_auth_headers
		WHERE carrier_id = $1::uuid AND header_id = $2::uuid`, carrierID, headerID,
	)
	if err != nil {
		return false, err
	}
	return ct.RowsAffected() > 0, nil
}

// MaskedHeaderValue last 4 + dots per spec: •••• + last4 (at least 8 mask chars for short secrets).
func MaskedHeaderValue(s string) (masked, last4 string) {
	if s == "" {
		return "••••", ""
	}
	if utf8.RuneCountInString(s) <= 4 {
		return "••••" + s, s
	}
	runes := []rune(s)
	n := len(runes)
	last4 = string(runes[n-4:])
	maskLen := 8
	if n < 12 {
		maskLen = n - 4
	}
	if maskLen < 4 {
		maskLen = 4
	}
	return string(makeRune('•', maskLen)) + last4, last4
}

func makeRune(c rune, n int) []rune {
	b := make([]rune, n)
	for i := range b {
		b[i] = c
	}
	return b
}

// GetAuthHeaderRow fetches a single row after create.
func GetAuthHeaderRow(ctx context.Context, pool *pgxpool.Pool, carrierID, headerID string, key32 []byte) (*AuthHeaderRow, error) {
	var r AuthHeaderRow
	var enc string
	err := pool.QueryRow(ctx, `
		SELECT header_id::text, header_name, header_value_enc
		FROM carrier_auth_headers
		WHERE carrier_id = $1::uuid AND header_id = $2::uuid`, carrierID, headerID,
	).Scan(&r.HeaderID, &r.HeaderName, &enc)
	if err != nil {
		return nil, err
	}
	pt, derr := DecryptValue(key32, enc)
	if derr != nil {
		return nil, derr
	}
	r.Value = pt
	return &r, nil
}
