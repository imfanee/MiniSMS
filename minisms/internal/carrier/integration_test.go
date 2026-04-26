package carrier

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/minisms/minisms/internal/db"
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

func TestResolveSenderID_IntegrationAllowlist(t *testing.T) {
	pool := testPoolOrSkip(t)
	ctx := context.Background()
	sfx := time.Now().UTC().Format("150405.000000")

	var clientID string
	err := pool.QueryRow(ctx, `
		INSERT INTO clients (name, email, status, currency, allow_any_sender_id, allow_in_loss_delivery)
		VALUES ($1, $2, 'active', 'GBP', FALSE, TRUE)
		RETURNING client_id::text`,
		"it-client-"+sfx, "it-client-"+sfx+"@example.test",
	).Scan(&clientID)
	if err != nil {
		t.Fatalf("insert client: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM clients WHERE client_id = $1::uuid`, clientID)
	})

	allowedValue := "ALW" + sfx[len(sfx)-4:]
	var senderID string
	err = pool.QueryRow(ctx, `
		INSERT INTO sender_ids (value, sender_id_type, is_active)
		VALUES ($1, 'alpha', TRUE)
		RETURNING sender_id::text`, allowedValue,
	).Scan(&senderID)
	if err != nil {
		t.Fatalf("insert sender_id: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM sender_ids WHERE sender_id = $1::uuid`, senderID)
	})
	_, err = pool.Exec(ctx, `INSERT INTO client_sender_ids (client_id, sender_id, is_default) VALUES ($1::uuid, $2::uuid, FALSE)`, clientID, senderID)
	if err != nil {
		t.Fatalf("insert client_sender_ids: %v", err)
	}

	client := &db.Client{ClientID: clientID}
	got, err := ResolveSenderID(ctx, pool, client, allowedValue, "MiniSMS")
	if err != nil {
		t.Fatalf("resolve allowed failed: %v", err)
	}
	if got.Source != "client_provided" || got.Value != allowedValue {
		t.Fatalf("unexpected allowed result: %+v", got)
	}

	_, err = ResolveSenderID(ctx, pool, client, "NOT_ALLOWED_"+sfx[len(sfx)-3:], "MiniSMS")
	if !errors.Is(err, ErrSenderNotAllowed) {
		t.Fatalf("expected ErrSenderNotAllowed, got %v", err)
	}

	_, err = pool.Exec(ctx, `UPDATE clients SET default_sender_id_value = 'DEFSID' WHERE client_id = $1::uuid`, clientID)
	if err != nil {
		t.Fatalf("set client default sid: %v", err)
	}
	got, err = ResolveSenderID(ctx, pool, client, "", "MiniSMS")
	if err != nil {
		t.Fatalf("resolve empty with client default failed: %v", err)
	}
	if got.Source != "client_default" || got.Value != "DEFSID" {
		t.Fatalf("unexpected client default result: %+v", got)
	}
}

func TestCheckCarrierEligibility_IntegrationListAndInLoss(t *testing.T) {
	pool := testPoolOrSkip(t)
	ctx := context.Background()
	sfx := time.Now().UTC().Format("150405.000000")

	var rgID string
	err := pool.QueryRow(ctx, `
		INSERT INTO rate_groups (name, currency)
		VALUES ($1, 'GBP')
		RETURNING rate_group_id::text`, "it-rg-"+sfx).Scan(&rgID)
	if err != nil {
		t.Fatalf("insert rate_group: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM rate_groups WHERE rate_group_id = $1::uuid`, rgID)
	})
	_, err = pool.Exec(ctx, `
		INSERT INTO rate_entries (rate_group_id, prefix, rate_per_sms, effective_from)
		VALUES ($1::uuid, '4477', 0.020000, CURRENT_DATE - INTERVAL '1 day')`, rgID)
	if err != nil {
		t.Fatalf("insert rate_entry: %v", err)
	}

	var clientID string
	err = pool.QueryRow(ctx, `
		INSERT INTO clients (name, email, status, currency, allow_any_sender_id, allow_in_loss_delivery)
		VALUES ($1, $2, 'active', 'GBP', FALSE, FALSE)
		RETURNING client_id::text`,
		"it-client2-"+sfx, "it-client2-"+sfx+"@example.test",
	).Scan(&clientID)
	if err != nil {
		t.Fatalf("insert client: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM clients WHERE client_id = $1::uuid`, clientID)
	})

	var carrierID string
	err = pool.QueryRow(ctx, `
		INSERT INTO carriers (name, endpoint_url, http_method, status, currency, sender_id_policy, rate_group_id)
		VALUES ($1, 'https://example.test/sms', 'POST', 'active', 'GBP', 'list', $2::uuid)
		RETURNING carrier_id::text`, "it-carrier-"+sfx, rgID,
	).Scan(&carrierID)
	if err != nil {
		t.Fatalf("insert carrier: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM carriers WHERE carrier_id = $1::uuid`, carrierID)
	})

	senderValue := "BRN" + sfx[len(sfx)-4:]
	var sid string
	err = pool.QueryRow(ctx, `
		INSERT INTO sender_ids (value, sender_id_type, is_active)
		VALUES ($1, 'alpha', TRUE)
		RETURNING sender_id::text`, senderValue).Scan(&sid)
	if err != nil {
		t.Fatalf("insert sender_id: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM sender_ids WHERE sender_id = $1::uuid`, sid)
	})
	_, err = pool.Exec(ctx, `INSERT INTO carrier_sender_ids (carrier_id, sender_id, is_default) VALUES ($1::uuid, $2::uuid, TRUE)`, carrierID, sid)
	if err != nil {
		t.Fatalf("insert carrier_sender_ids: %v", err)
	}

	rg := rgID
	effectiveSID, eligible, reason, err := CheckCarrierEligibility(
		ctx, pool, carrierID, "it-carrier-"+sfx, "list", nil, &rg,
		SenderIDResolution{Value: senderValue, Source: "client_provided"},
		"0.010000", clientID, "447700123",
	)
	if err != nil {
		t.Fatalf("eligibility in-loss call failed: %v", err)
	}
	if eligible || reason != "in_loss" {
		t.Fatalf("expected in_loss skip, got eligible=%v reason=%q sid=%q", eligible, reason, effectiveSID)
	}

	_, eligible, reason, err = CheckCarrierEligibility(
		ctx, pool, carrierID, "it-carrier-"+sfx, "list", nil, &rg,
		SenderIDResolution{Value: senderValue, Source: "client_provided"},
		"0.030000", clientID, "447700123",
	)
	if err != nil {
		t.Fatalf("eligibility profitable call failed: %v", err)
	}
	if !eligible || reason != "" {
		t.Fatalf("expected eligible route, got eligible=%v reason=%q", eligible, reason)
	}

	_, eligible, reason, err = CheckCarrierEligibility(
		ctx, pool, carrierID, "it-carrier-"+sfx, "list", nil, &rg,
		SenderIDResolution{Value: "NOTINLIST", Source: "client_provided"},
		"0.030000", clientID, "447700123",
	)
	if err != nil {
		t.Fatalf("eligibility list enforcement call failed: %v", err)
	}
	if eligible || reason != "sender_id_policy" {
		t.Fatalf("expected sender_id_policy skip, got eligible=%v reason=%q", eligible, reason)
	}
}

