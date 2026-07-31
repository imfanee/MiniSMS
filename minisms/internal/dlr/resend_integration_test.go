// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package dlr

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/minisms/minisms/internal/db"
)

// fakeDeliverer records DeliverDLR calls and reports a fixed success result, standing in for a bound
// client SMPP session.
type fakeDeliverer struct {
	calls   int
	lastID  string
	lastMsg string
	lastSt  string
	ok      bool
}

func (f *fakeDeliverer) DeliverDLR(clientID, messageID, dlrStatus string) bool {
	f.calls++
	f.lastID, f.lastMsg, f.lastSt = clientID, messageID, dlrStatus
	return f.ok
}

func resendTestPoolOrSkip(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connect db: %v", err)
	}
	t.Cleanup(func() { pool.Close() })
	return pool
}

// TestResendToClient_SMPPForwardOnly verifies the operator resend re-delivers the stored receipt over
// the client's SMPP channel without changing the stored status (forward-only, no re-rating).
func TestResendToClient_SMPPForwardOnly(t *testing.T) {
	pool := resendTestPoolOrSkip(t)
	ctx := context.Background()

	var clientID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO clients (name, email, status, currency, dlr_delivery_mode)
		VALUES ('dlr-resend-test', 'dlr-resend@test.example', 'active', 'GBP', 'smpp')
		RETURNING client_id::text`).Scan(&clientID); err != nil {
		t.Fatalf("insert client: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, `DELETE FROM clients WHERE client_id=$1::uuid`, clientID) })

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	msgID, err := db.CreateSMSLog(ctx, tx, db.SMSLog{
		ClientID:       clientID,
		ToNumber:       "+14155550104",
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

	// A rejected receipt has already been recorded.
	if applied, err := db.UpdateDLRReceived(ctx, pool, msgID, "rejected"); err != nil || !applied {
		t.Fatalf("seed rejected receipt: applied=%v err=%v", applied, err)
	}

	fake := &fakeDeliverer{ok: true}
	p := &Processor{Pool: pool, SecretKey: make([]byte, 32), SMPP: fake}

	status, err := p.ResendToClient(ctx, msgID)
	if err != nil {
		t.Fatalf("ResendToClient: %v", err)
	}
	if status != "smpp_ok" {
		t.Fatalf("forward status = %q, want smpp_ok", status)
	}
	if fake.calls != 1 || fake.lastID != clientID || fake.lastMsg != msgID || fake.lastSt != "rejected" {
		t.Fatalf("deliverer got calls=%d id=%q msg=%q stat=%q", fake.calls, fake.lastID, fake.lastMsg, fake.lastSt)
	}

	// Forward-only: the stored status stays rejected and the forward status is recorded.
	var status2, dlrStatus, fwd string
	if err := pool.QueryRow(ctx, `SELECT status, dlr_status, dlr_forward_status FROM sms_logs WHERE message_id=$1::uuid`, msgID).
		Scan(&status2, &dlrStatus, &fwd); err != nil {
		t.Fatalf("select: %v", err)
	}
	if status2 != "rejected" || dlrStatus != "rejected" || fwd != "smpp_ok" {
		t.Fatalf("post-resend state: status=%q dlr_status=%q forward=%q", status2, dlrStatus, fwd)
	}

	// A message with no stored receipt cannot be resent.
	tx2, _ := pool.Begin(ctx)
	emptyID, err := db.CreateSMSLog(ctx, tx2, db.SMSLog{
		ClientID: clientID, ToNumber: "+14155550105", MessageBody: "x", MessageLength: 1, Segments: 1,
		Encoding: "GSM7", RateApplied: "0.010000", TotalCharged: "0.010000", Currency: "GBP",
		Status: "accepted", SenderIDSource: "client_default",
	})
	if err != nil {
		_ = tx2.Rollback(ctx)
		t.Fatalf("create empty log: %v", err)
	}
	_ = tx2.Commit(ctx)
	t.Cleanup(func() { _, _ = pool.Exec(ctx, `DELETE FROM sms_logs WHERE message_id=$1::uuid`, emptyID) })
	if _, err := p.ResendToClient(ctx, emptyID); err == nil {
		t.Fatalf("expected error resending a message with no receipt")
	}
}
