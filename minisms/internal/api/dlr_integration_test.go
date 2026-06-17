// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/minisms/minisms/internal/config"
	"github.com/minisms/minisms/internal/db"
	"github.com/minisms/minisms/internal/dlr"
	"github.com/minisms/minisms/internal/smslog"
)

func testHandlers(pool *pgxpool.Pool, key []byte) *Handlers {
	cfg := &config.Config{SecretKey: key}
	return &Handlers{
		Pool:   pool,
		Config: cfg,
		DLR:    &dlr.Processor{Pool: pool, SecretKey: key},
	}
}

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

type dlrFixture struct {
	clientID  string
	carrierID string
	messageID string
}

func insertDLRFixture(t *testing.T, pool *pgxpool.Pool, key []byte, dlrRequested bool, webhookURL *string, inboundSecret *string, clientWebhookSecret *string) dlrFixture {
	t.Helper()
	ctx := context.Background()
	sfx := time.Now().UTC().Format("150405.000000")

	var clientID string
	err := pool.QueryRow(ctx, `
		INSERT INTO clients (name, email, status, currency, dlr_webhook_secret)
		VALUES ($1, $2, 'active', 'GBP', $3)
		RETURNING client_id::text`,
		"dlr-client-"+sfx, "dlr-client-"+sfx+"@example.test", clientWebhookSecret,
	).Scan(&clientID)
	if err != nil {
		t.Fatalf("insert client: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, `DELETE FROM clients WHERE client_id=$1::uuid`, clientID) })

	var carrierID string
	err = pool.QueryRow(ctx, `
		INSERT INTO carriers (name, endpoint_url, http_method, status, currency, dlr_inbound_secret, dlr_status_field)
		VALUES ($1, 'https://carrier.example.test/sms', 'POST', 'active', 'GBP', $2, 'status')
		RETURNING carrier_id::text`,
		"dlr-carrier-"+sfx, inboundSecret,
	).Scan(&carrierID)
	if err != nil {
		t.Fatalf("insert carrier: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, `DELETE FROM carriers WHERE carrier_id=$1::uuid`, carrierID) })

	var messageID string
	err = pool.QueryRow(ctx, `
		INSERT INTO sms_logs (
			client_id, to_number, from_number, message_body, message_length, segments, encoding, rate_applied, total_charged, currency,
			status, carrier_id, dlr_requested, dlr_webhook_url, failover_sequence, source_addr_ton, source_addr_npi, dest_addr_ton, dest_addr_npi
		) VALUES (
			$1::uuid, '+447700900123', 'MiniSMS', 'Hello', 5, 1, 'GSM7', 0.010000, 0.010000, 'GBP',
			'accepted', $2::uuid, $3, $4, 0, 5, 0, 1, 1
		) RETURNING message_id::text`,
		clientID, carrierID, dlrRequested, webhookURL,
	).Scan(&messageID)
	if err != nil {
		t.Fatalf("insert sms_log: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, `DELETE FROM sms_logs WHERE message_id=$1::uuid`, messageID) })
	_ = key
	return dlrFixture{clientID: clientID, carrierID: carrierID, messageID: messageID}
}

func performDLRRequest(h *Handlers, messageID, query string, body string, headerSecret string) *httptest.ResponseRecorder {
	r := chi.NewRouter()
	r.MethodFunc(http.MethodPost, "/api/v1/dlr/{message_id}", h.HandleDLR())
	path := "/api/v1/dlr/" + messageID
	if query != "" {
		path += "?" + query
	}
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if headerSecret != "" {
		req.Header.Set("X-DLR-Secret", headerSecret)
	}
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	return rr
}

func TestHandleDLR_MessageNotFound(t *testing.T) {
	pool := testPoolOrSkip(t)
	key := []byte("0123456789abcdef0123456789abcdef")
	h := testHandlers(pool, key)
	rr := performDLRRequest(h, uuid.NewString(), "", `{"status":"DELIVRD"}`, "")
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

func TestHandleDLR_NoWebhookURL(t *testing.T) {
	pool := testPoolOrSkip(t)
	key := []byte("0123456789abcdef0123456789abcdef")
	h := testHandlers(pool, key)
	fx := insertDLRFixture(t, pool, key, true, nil, nil, nil)
	rr := performDLRRequest(h, fx.messageID, "carrier_status=DELIVRD", `{"status":"DELIVRD","detail":"ok"}`, "")
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var status string
	if err := pool.QueryRow(context.Background(), `SELECT COALESCE(dlr_forward_status,'') FROM sms_logs WHERE message_id=$1::uuid`, fx.messageID).Scan(&status); err != nil {
		t.Fatalf("query forward status: %v", err)
	}
	if status != "no_url" {
		t.Fatalf("expected no_url, got %q", status)
	}
	var timelineRaw []byte
	if err := pool.QueryRow(context.Background(), `SELECT event_timeline FROM sms_logs WHERE message_id=$1::uuid`, fx.messageID).Scan(&timelineRaw); err != nil {
		t.Fatalf("query timeline: %v", err)
	}
	events := smslog.ParseTimeline(timelineRaw)
	var dlrEvent *smslog.TimelineEvent
	for i := range events {
		if events[i].Kind == smslog.EventDLRReceived {
			dlrEvent = &events[i]
			break
		}
	}
	if dlrEvent == nil {
		t.Fatal("expected dlr_received timeline event")
	}
	if dlrEvent.Meta["mapped_status"] != "delivered" {
		t.Fatalf("mapped_status: %v", dlrEvent.Meta["mapped_status"])
	}
	qp, ok := dlrEvent.Meta["query_params"].(map[string]any)
	if !ok || qp["carrier_status"] != "DELIVRD" {
		t.Fatalf("query_params: %v", dlrEvent.Meta["query_params"])
	}
	if dlrEvent.Meta["request_body"] != `{"status":"DELIVRD","detail":"ok"}` {
		t.Fatalf("request_body: %v", dlrEvent.Meta["request_body"])
	}
}

func TestHandleDLR_SuccessfulForward(t *testing.T) {
	pool := testPoolOrSkip(t)
	key := []byte("0123456789abcdef0123456789abcdef")
	var gotSignature string
	webhook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSignature = r.Header.Get("X-MiniSMS-Signature")
		w.WriteHeader(http.StatusOK)
	}))
	defer webhook.Close()
	secretEnc, err := db.EncryptValue(key, "client-hook-secret")
	if err != nil {
		t.Fatalf("encrypt secret: %v", err)
	}
	fx := insertDLRFixture(t, pool, key, true, strPtr(webhook.URL), nil, &secretEnc)
	h := testHandlers(pool, key)
	rr := performDLRRequest(h, fx.messageID, "", `{"status":"DELIVRD"}`, "")
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if gotSignature == "" {
		t.Fatalf("expected webhook signature header")
	}
	var status string
	var attempts int
	if err := pool.QueryRow(context.Background(), `SELECT COALESCE(dlr_forward_status,''), dlr_forward_attempts FROM sms_logs WHERE message_id=$1::uuid`, fx.messageID).Scan(&status, &attempts); err != nil {
		t.Fatalf("query forward status: %v", err)
	}
	if status != "success" || attempts < 1 {
		t.Fatalf("expected success and attempts>=1, got status=%q attempts=%d", status, attempts)
	}
}

func TestHandleDLR_WebhookForwardFails(t *testing.T) {
	pool := testPoolOrSkip(t)
	key := []byte("0123456789abcdef0123456789abcdef")
	webhook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer webhook.Close()
	fx := insertDLRFixture(t, pool, key, true, strPtr(webhook.URL), nil, nil)
	h := testHandlers(pool, key)
	rr := performDLRRequest(h, fx.messageID, "", `{"status":"UNDELIV"}`, "")
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var status string
	if err := pool.QueryRow(context.Background(), `SELECT COALESCE(dlr_forward_status,'') FROM sms_logs WHERE message_id=$1::uuid`, fx.messageID).Scan(&status); err != nil {
		t.Fatalf("query forward status: %v", err)
	}
	if status != "failed" {
		t.Fatalf("expected failed, got %q", status)
	}
}

func TestHandleDLR_InboundSecretVerification(t *testing.T) {
	pool := testPoolOrSkip(t)
	key := []byte("0123456789abcdef0123456789abcdef")
	inboundEnc, err := db.EncryptValue(key, "carrier-secret")
	if err != nil {
		t.Fatalf("encrypt inbound secret: %v", err)
	}
	fx := insertDLRFixture(t, pool, key, true, nil, &inboundEnc, nil)
	h := testHandlers(pool, key)
	rrBad := performDLRRequest(h, fx.messageID, "", `{"status":"DELIVRD"}`, "wrong")
	if rrBad.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for wrong secret, got %d", rrBad.Code)
	}
	rrGood := performDLRRequest(h, fx.messageID, "", `{"status":"DELIVRD"}`, "carrier-secret")
	if rrGood.Code != http.StatusOK {
		t.Fatalf("expected 200 for correct secret, got %d", rrGood.Code)
	}
}
