// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package db

import (
	"context"
	"testing"
)

// TestUpdateDLRReceived_FinalNotOverwritten verifies the multi-bit dlr-mask guard: an intermediate
// receipt may be upgraded to a final one, but once a final status is stored no later receipt
// (intermediate or a second final) overwrites it.
func TestUpdateDLRReceived_FinalNotOverwritten(t *testing.T) {
	pool := testPoolOrSkip(t)
	ctx := context.Background()

	var clientID string
	err := pool.QueryRow(ctx, `
		INSERT INTO clients (name, email, status, currency)
		VALUES ('dlr-cond-test', 'dlr-cond@test.example', 'active', 'GBP')
		RETURNING client_id::text`).Scan(&clientID)
	if err != nil {
		t.Fatalf("insert client: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, `DELETE FROM clients WHERE client_id=$1::uuid`, clientID) })

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	msgID, err := CreateSMSLog(ctx, tx, SMSLog{
		ClientID:       clientID,
		ToNumber:       "+243993873999",
		MessageBody:    "hi",
		MessageLength:  2,
		Segments:       1,
		Encoding:       "GSM7",
		RateApplied:    "0.010000",
		TotalCharged:   "0.010000",
		Currency:       "GBP",
		Status:         "accepted",
		SenderIDSource: "client_default",
	})
	if err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("create log: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, `DELETE FROM sms_logs WHERE message_id=$1::uuid`, msgID) })

	// Intermediate receipt (Kamex SMSC ACK normalizes to unknown) applies.
	if applied, err := UpdateDLRReceived(ctx, pool, msgID, "unknown"); err != nil || !applied {
		t.Fatalf("intermediate receipt: applied=%v err=%v, want applied=true", applied, err)
	}
	// Final receipt upgrades the stored non-final status.
	if applied, err := UpdateDLRReceived(ctx, pool, msgID, "delivered"); err != nil || !applied {
		t.Fatalf("final receipt: applied=%v err=%v, want applied=true", applied, err)
	}
	// A trailing intermediate must not overwrite the final status.
	if applied, err := UpdateDLRReceived(ctx, pool, msgID, "unknown"); err != nil || applied {
		t.Fatalf("trailing intermediate: applied=%v err=%v, want applied=false", applied, err)
	}
	// A second final must not overwrite the first either.
	if applied, _ := UpdateDLRReceived(ctx, pool, msgID, "undelivered"); applied {
		t.Fatalf("second final overwrote the first")
	}

	var dlrStatus, status string
	if err := pool.QueryRow(ctx, `SELECT dlr_status, status FROM sms_logs WHERE message_id=$1::uuid`, msgID).Scan(&dlrStatus, &status); err != nil {
		t.Fatalf("select: %v", err)
	}
	if dlrStatus != "delivered" || status != "delivered" {
		t.Fatalf("final state not preserved: dlr_status=%q status=%q", dlrStatus, status)
	}
}
