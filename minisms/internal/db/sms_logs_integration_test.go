// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package db

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func testPoolOrSkip(t *testing.T) *pgxpool.Pool {
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

func TestCreateSMSLog_PersistsSenderIDSource(t *testing.T) {
	pool := testPoolOrSkip(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var clientID string
	err = tx.QueryRow(ctx, `
		INSERT INTO clients (name, email, status, currency)
		VALUES ('sid-src-test', 'sid-src@test.example', 'active', 'GBP')
		RETURNING client_id::text`).Scan(&clientID)
	if err != nil {
		t.Fatalf("insert client: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, `DELETE FROM clients WHERE client_id=$1::uuid`, clientID) })

	msgID, err := CreateSMSLog(ctx, tx, SMSLog{
		ClientID:       clientID,
		ToNumber:       "+447700900123",
		MessageBody:    "hi",
		MessageLength:  2,
		Segments:       1,
		Encoding:       "GSM7",
		RateApplied:    "0.010000",
		TotalCharged:   "0.010000",
		Currency:       "GBP",
		Status:         "pending",
		SenderIDSource: "client_default",
	})
	if err != nil {
		t.Fatalf("create log: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, `DELETE FROM sms_logs WHERE message_id=$1::uuid`, msgID) })

	var src string
	err = pool.QueryRow(ctx, `SELECT sender_id_source FROM sms_logs WHERE message_id=$1::uuid`, msgID).Scan(&src)
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if src != "client_default" {
		t.Fatalf("sender_id_source=%q want client_default", src)
	}
}
