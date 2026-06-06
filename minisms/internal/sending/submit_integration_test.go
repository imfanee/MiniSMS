// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package sending

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/minisms/minisms/internal/carrier"
	"github.com/minisms/minisms/internal/config"
	"github.com/minisms/minisms/internal/db"
)

func testPoolOrSkip(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	carrier.SetDispatchEndpointValidatorForTest(func(string) error { return nil })
	t.Cleanup(carrier.ResetDispatchEndpointValidator)
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connect db: %v", err)
	}
	t.Cleanup(func() { pool.Close() })
	return pool
}

type submitFixture struct {
	clientID      string
	carrierID     string
	rateGroupID   string
	routingGroupID string
}

func insertSubmitFixture(t *testing.T, pool *pgxpool.Pool, carrierURL string, clientBalance string) submitFixture {
	t.Helper()
	ctx := context.Background()
	sfx := time.Now().UTC().Format("150405.000000000")

	var rgID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO rate_groups (name, currency)
		VALUES ($1, 'GBP')
		RETURNING rate_group_id::text`, "submit-rg-"+sfx).Scan(&rgID); err != nil {
		t.Fatalf("rate_group: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, `DELETE FROM rate_groups WHERE rate_group_id = $1::uuid`, rgID) })
	if _, err := pool.Exec(ctx, `
		INSERT INTO rate_entries (rate_group_id, prefix, rate_per_sms, effective_from)
		VALUES ($1::uuid, '4477', 0.010000, CURRENT_DATE - INTERVAL '1 day')`, rgID); err != nil {
		t.Fatalf("rate_entry: %v", err)
	}

	var routID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO routing_groups (name)
		VALUES ($1)
		RETURNING routing_group_id::text`, "submit-rtg-"+sfx).Scan(&routID); err != nil {
		t.Fatalf("routing_group: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, `DELETE FROM routing_groups WHERE routing_group_id = $1::uuid`, routID) })

	var carrierID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO carriers (name, endpoint_url, http_method, status, currency, balance, sender_id_policy, egress_transport)
		VALUES ($1, $2, 'POST', 'active', 'GBP', 100.000000, 'any', 'http')
		RETURNING carrier_id::text`, "submit-carrier-"+sfx, carrierURL).Scan(&carrierID); err != nil {
		t.Fatalf("carrier: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM carrier_request_templates WHERE carrier_id = $1::uuid`, carrierID)
		_, _ = pool.Exec(ctx, `DELETE FROM carriers WHERE carrier_id = $1::uuid`, carrierID)
	})
	if err := db.UpsertRequestTemplate(ctx, pool, carrierID, "application/json",
		`{"to":"{{to}}","from":"{{from}}","text":"{{message}}"}`, ""); err != nil {
		t.Fatalf("carrier template: %v", err)
	}

	if _, err := pool.Exec(ctx, `
		INSERT INTO route_entries (routing_group_id, prefix, primary_carrier_id, status)
		VALUES ($1::uuid, '4477', $2::uuid, 'active')`, routID, carrierID); err != nil {
		t.Fatalf("route_entry: %v", err)
	}

	var clientID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO clients (name, email, status, currency, balance, rate_group_id, routing_group_id, allowed_sender_ids_mode)
		VALUES ($1, $2, 'active', 'GBP', $3::numeric(18,6), $4::uuid, $5::uuid, 'any')
		RETURNING client_id::text`,
		"submit-client-"+sfx, "submit-client-"+sfx+"@example.test", clientBalance, rgID, routID,
	).Scan(&clientID); err != nil {
		t.Fatalf("client: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM sms_logs WHERE client_id = $1::uuid`, clientID)
		_, _ = pool.Exec(ctx, `DELETE FROM clients WHERE client_id = $1::uuid`, clientID)
	})

	return submitFixture{clientID: clientID, carrierID: carrierID, rateGroupID: rgID, routingGroupID: routID}
}

func TestSubmit_InsufficientBalance(t *testing.T) {
	pool := testPoolOrSkip(t)
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer mock.Close()

	fx := insertSubmitFixture(t, pool, mock.URL, "0.001000")
	client, err := db.GetClient(context.Background(), pool, fx.clientID)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{CarrierDispatchTimeoutSecs: 5}
	svc := New(pool, cfg)
	out := svc.Submit(context.Background(), SubmitParams{
		Client: client,
		Message: AcceptedMessage{
			To:      "+447700900123",
			From:    "MiniSMS",
			Body:    "hello",
			IngressTransport: IngressHTTP,
		},
		SenderID: carrier.SenderIDResolution{Value: "MiniSMS", Source: "system_default"},
	})
	if out.Kind != OutcomeInsufficientBalance {
		t.Fatalf("expected insufficient balance, got %v", out.Kind)
	}
	var n int
	err = pool.QueryRow(context.Background(), `
		SELECT COUNT(*) FROM ledger_entries WHERE client_id = $1::uuid`, fx.clientID).Scan(&n)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("expected no ledger rows, got %d", n)
	}
}

func TestSubmit_CarrierFailureRefund(t *testing.T) {
	pool := testPoolOrSkip(t)
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, `{"error":"down"}`)
	}))
	defer mock.Close()

	fx := insertSubmitFixture(t, pool, mock.URL, "5.000000")
	ctx := context.Background()
	_, _ = pool.Exec(ctx, `
		INSERT INTO system_settings (key, value, description)
		VALUES ('refund_on_carrier_failure', 'true', 'test')
		ON CONFLICT (key) DO UPDATE SET value = 'true'`)

	client, err := db.GetClient(ctx, pool, fx.clientID)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{CarrierDispatchTimeoutSecs: 5}
	out := New(pool, cfg).Submit(ctx, SubmitParams{
		Client: client,
		Message: AcceptedMessage{
			To: "+447700900123", From: "MiniSMS", Body: "fail case", IngressTransport: IngressHTTP,
		},
		SenderID: carrier.SenderIDResolution{Value: "MiniSMS", Source: "system_default"},
	})
	if out.Kind != OutcomeCarrierFailure {
		t.Fatalf("expected carrier failure, got %v", out.Kind)
	}
	var status string
	err = pool.QueryRow(ctx, `
		SELECT status FROM sms_logs WHERE client_id = $1::uuid ORDER BY received_at DESC LIMIT 1`, fx.clientID).Scan(&status)
	if err != nil {
		t.Fatal(err)
	}
	if status != "failed" {
		t.Fatalf("expected sms_log failed, got %q", status)
	}
	var debitCount int
	err = pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM ledger_entries
		WHERE client_id = $1::uuid AND entry_type = 'debit'`, fx.clientID).Scan(&debitCount)
	if err != nil {
		t.Fatal(err)
	}
	if debitCount != 0 {
		t.Fatalf("carrier failure before accept must not debit client, got %d debit rows", debitCount)
	}
}

func TestSubmit_AcceptedDebitsBalance(t *testing.T) {
	pool := testPoolOrSkip(t)
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method %s", r.Method)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer mock.Close()

	fx := insertSubmitFixture(t, pool, mock.URL, "5.000000")
	ctx := context.Background()
	client, err := db.GetClient(ctx, pool, fx.clientID)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{CarrierDispatchTimeoutSecs: 5}
	out := New(pool, cfg).Submit(ctx, SubmitParams{
		Client: client,
		Message: AcceptedMessage{
			To: "+447700900123", From: "MiniSMS", Body: "ok", IngressTransport: IngressHTTP,
		},
		SenderID: carrier.SenderIDResolution{Value: "MiniSMS", Source: "system_default"},
	})
	if out.Kind != OutcomeAccepted || out.Accepted == nil {
		t.Fatalf("expected accepted, got %+v", out)
	}
	var status, charged string
	err = pool.QueryRow(ctx, `
		SELECT status, total_charged::text FROM sms_logs WHERE message_id = $1::uuid`,
		out.Accepted.MessageID).Scan(&status, &charged)
	if err != nil {
		t.Fatal(err)
	}
	if status != "accepted" {
		t.Fatalf("status %q", status)
	}
	if !strings.HasPrefix(charged, "0.01") {
		t.Fatalf("charged %q", charged)
	}
	var debitCount int
	err = pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM ledger_entries
		WHERE client_id = $1::uuid AND entry_type = 'debit' AND message_id = $2::uuid`,
		fx.clientID, out.Accepted.MessageID).Scan(&debitCount)
	if err != nil {
		t.Fatal(err)
	}
	if debitCount != 1 {
		t.Fatalf("expected 1 debit ledger row, got %d", debitCount)
	}
	if out.Accepted.BalanceRemaining == "" {
		t.Fatal("expected balance remaining in outcome")
	}
}
