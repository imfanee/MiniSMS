// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
// Package audit holds security/financial integration tests (TEST_DATABASE_URL only).
package audit

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { pool.Close() })
	return pool
}

func TestLedgerTables_RejectUpdateDelete(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	t.Run("ledger_entries", func(t *testing.T) {
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback(ctx) }()

		var clientID string
		err = tx.QueryRow(ctx, `
			INSERT INTO clients (name, email, status, currency, balance)
			VALUES ('audit-ledger', 'audit-ledger@test.example', 'active', 'GBP', 1.000000)
			RETURNING client_id::text`).Scan(&clientID)
		if err != nil {
			t.Fatal(err)
		}
		var ledgerID string
		err = tx.QueryRow(ctx, `
			INSERT INTO ledger_entries (client_id, entry_type, amount, balance_before, balance_after, currency, reference)
			VALUES ($1::uuid, 'credit', 1.000000, 0, 1.000000, 'GBP', 'audit-test')
			RETURNING entry_id::text`, clientID).Scan(&ledgerID)
		if err != nil {
			t.Fatal(err)
		}

		_, err = tx.Exec(ctx, `UPDATE ledger_entries SET amount = 0 WHERE entry_id = $1::uuid`, ledgerID)
		if err == nil {
			t.Fatal("ledger_entries UPDATE should be denied by trigger")
		}
		_, err = tx.Exec(ctx, `DELETE FROM ledger_entries WHERE entry_id = $1::uuid`, ledgerID)
		if err == nil {
			t.Fatal("ledger_entries DELETE should be denied by trigger")
		}
	})

	t.Run("carrier_balance_entries", func(t *testing.T) {
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback(ctx) }()

		var carrierID string
		err = tx.QueryRow(ctx, `
			INSERT INTO carriers (name, endpoint_url, http_method, status, currency, balance, egress_transport)
			VALUES ('audit-carrier', 'https://example.test/sms', 'POST', 'active', 'GBP', 1.000000, 'http')
			RETURNING carrier_id::text`).Scan(&carrierID)
		if err != nil {
			t.Fatal(err)
		}
		var cbeID string
		err = tx.QueryRow(ctx, `
			INSERT INTO carrier_balance_entries (carrier_id, entry_type, amount, direction, balance_before, balance_after, currency)
			VALUES ($1::uuid, 'payment', 1.000000, 1, 0, 1.000000, 'GBP')
			RETURNING entry_id::text`, carrierID).Scan(&cbeID)
		if err != nil {
			t.Fatal(err)
		}

		_, err = tx.Exec(ctx, `UPDATE carrier_balance_entries SET amount = 0 WHERE entry_id = $1::uuid`, cbeID)
		if err == nil {
			t.Fatal("carrier_balance_entries UPDATE should be denied")
		}
		_, err = tx.Exec(ctx, `DELETE FROM carrier_balance_entries WHERE entry_id = $1::uuid`, cbeID)
		if err == nil {
			t.Fatal("carrier_balance_entries DELETE should be denied")
		}
	})

	t.Run("audit_log", func(t *testing.T) {
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback(ctx) }()

		var clientID string
		err = tx.QueryRow(ctx, `
			INSERT INTO clients (name, email, status, currency, balance)
			VALUES ('audit-log', 'audit-log@test.example', 'active', 'GBP', 0)
			RETURNING client_id::text`).Scan(&clientID)
		if err != nil {
			t.Fatal(err)
		}
		var auditID string
		err = tx.QueryRow(ctx, `
			INSERT INTO audit_log (action, entity_type, entity_id, payload)
			VALUES ('audit_test', 'client', $1::uuid, '{}'::jsonb)
			RETURNING audit_id::text`, clientID).Scan(&auditID)
		if err != nil {
			t.Fatal(err)
		}

		_, err = tx.Exec(ctx, `UPDATE audit_log SET action = 'x' WHERE audit_id = $1::uuid`, auditID)
		if err == nil {
			t.Fatal("audit_log UPDATE should be denied")
		}
		_, err = tx.Exec(ctx, `DELETE FROM audit_log WHERE audit_id = $1::uuid`, auditID)
		if err == nil {
			t.Fatal("audit_log DELETE should be denied")
		}
	})
}
