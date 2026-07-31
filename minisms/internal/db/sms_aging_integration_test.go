// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package db

import (
	"context"
	"testing"
	"time"
)

// TestAgeOutAcceptedNoDLR verifies the aging reaper's data path: a stale accepted + DLR-requested
// message is listed and closed out as failed/undelivered, while a message with a final DLR or one that
// did not request a DLR is left untouched.
func TestAgeOutAcceptedNoDLR(t *testing.T) {
	pool := testPoolOrSkip(t)
	ctx := context.Background()

	var clientID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO clients (name, email, status, currency)
		VALUES ('age-test', 'age@test.example', 'active', 'GBP')
		RETURNING client_id::text`).Scan(&clientID); err != nil {
		t.Fatalf("insert client: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, `DELETE FROM clients WHERE client_id=$1::uuid`, clientID) })

	mk := func(status string, dlrReq bool, age time.Duration) string {
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		id, err := CreateSMSLog(ctx, tx, SMSLog{
			ClientID: clientID, ToNumber: "+14155550110", MessageBody: "x", MessageLength: 1, Segments: 1,
			Encoding: "GSM7", RateApplied: "0.010000", TotalCharged: "0.010000", Currency: "GBP",
			Status: status, SenderIDSource: "client_default",
		})
		if err != nil {
			_ = tx.Rollback(ctx)
			t.Fatalf("create log: %v", err)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatalf("commit: %v", err)
		}
		_, _ = pool.Exec(ctx, `UPDATE sms_logs SET dlr_requested=$2, received_at=now()-$3::interval WHERE message_id=$1::uuid`,
			id, dlrReq, age.String())
		t.Cleanup(func() { _, _ = pool.Exec(ctx, `DELETE FROM sms_logs WHERE message_id=$1::uuid`, id) })
		return id
	}

	stale := mk("accepted", true, 2*time.Hour)  // should age
	fresh := mk("accepted", true, 1*time.Minute) // too new
	noReq := mk("accepted", false, 2*time.Hour)  // no DLR requested
	done := mk("delivered", true, 2*time.Hour)   // already final

	ids, err := ListStuckAcceptedNoDLR(ctx, pool, time.Now().Add(-time.Hour), 100)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	got := map[string]bool{}
	for _, id := range ids {
		got[id] = true
	}
	if !got[stale] {
		t.Fatalf("stale message not listed for aging")
	}
	if got[fresh] || got[noReq] || got[done] {
		t.Fatalf("wrongly listed: fresh=%v noReq=%v done=%v", got[fresh], got[noReq], got[done])
	}

	applied, err := AgeOutAcceptedNoDLR(ctx, pool, stale)
	if err != nil || !applied {
		t.Fatalf("age out stale: applied=%v err=%v", applied, err)
	}
	var status, dlrStatus string
	var failedAt *time.Time
	if err := pool.QueryRow(ctx, `SELECT status, dlr_status, failed_at FROM sms_logs WHERE message_id=$1::uuid`, stale).
		Scan(&status, &dlrStatus, &failedAt); err != nil {
		t.Fatalf("select: %v", err)
	}
	if status != "failed" || dlrStatus != "undelivered" || failedAt == nil {
		t.Fatalf("aged state: status=%q dlr_status=%q failed_at=%v", status, dlrStatus, failedAt)
	}

	// Idempotent: a second age-out is a no-op because status is no longer 'accepted'.
	if applied, _ := AgeOutAcceptedNoDLR(ctx, pool, stale); applied {
		t.Fatalf("second age-out should be a no-op")
	}
}
