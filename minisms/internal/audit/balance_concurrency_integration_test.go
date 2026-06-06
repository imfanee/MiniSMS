// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package audit

import (
	"context"
	"sync"
	"testing"
	"time"
)

// TestDeductClientBalance_SerializesUnderParallelLoad asserts prepaid debits do not overspend.
func TestDeductClientBalance_SerializesUnderParallelLoad(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	sfx := time.Now().UTC().Format("150405.000000000")
	var clientID string
	err = tx.QueryRow(ctx, `
		INSERT INTO clients (name, email, status, currency, balance)
		VALUES ($1, $2, 'active', 'GBP', 0.030000)
		RETURNING client_id::text`, "audit-concurrency-"+sfx, "audit-conc-"+sfx+"@test.example").Scan(&clientID)
	if err != nil {
		_ = tx.Rollback(ctx)
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM sms_logs WHERE client_id = $1::uuid`, clientID)
		_, _ = pool.Exec(ctx, `DELETE FROM clients WHERE client_id = $1::uuid`, clientID)
	})

	const workers = 10
	const amount = "0.010000"
	var wg sync.WaitGroup
	var mu sync.Mutex
	success := 0
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tx, err := pool.Begin(ctx)
			if err != nil {
				return
			}
			defer func() { _ = tx.Rollback(ctx) }()
			var remaining string
			err = tx.QueryRow(ctx, `SELECT deduct_client_balance($1::uuid, $2::numeric(18,6), NULL, 'GBP')::text`,
				clientID, amount).Scan(&remaining)
			if err != nil {
				return
			}
			if err := tx.Commit(ctx); err != nil {
				return
			}
			mu.Lock()
			success++
			mu.Unlock()
		}()
	}
	wg.Wait()

	var bal string
	if err := pool.QueryRow(ctx, `SELECT balance::text FROM clients WHERE client_id = $1::uuid`, clientID).Scan(&bal); err != nil {
		t.Fatal(err)
	}
	if success != 3 {
		t.Fatalf("expected exactly 3 successful debits from 0.03 balance, got %d", success)
	}
	if bal != "0.000000" {
		t.Fatalf("balance=%s want 0.000000", bal)
	}
}
