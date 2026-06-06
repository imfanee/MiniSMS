// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package server

import (
	"bytes"
	"context"
	"net"
	"os"
	"testing"
	"time"

	"github.com/fiorix/go-smpp/v2/smpp"
	"github.com/fiorix/go-smpp/v2/smpp/pdu"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/minisms/minisms/internal/config"
	"github.com/minisms/minisms/internal/db"
	"github.com/minisms/minisms/internal/sending"
)

func TestServer_BindOK(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	key := []byte("0123456789abcdef0123456789abcdef")
	passPlain := "smpp-test-secret"
	passEnc, err := db.EncryptValue(key, passPlain)
	if err != nil {
		t.Fatal(err)
	}
	sfx := time.Now().UTC().Format("150405.000000000")
	systemID := "esme-" + sfx
	var clientID string
	cidr := "127.0.0.1/32"
	err = pool.QueryRow(ctx, `
		INSERT INTO clients (name, email, status, currency, smpp_ingress_enabled, smpp_system_id, smpp_password_enc, smpp_max_binds, smpp_allowed_cidrs, allowed_sender_ids_mode)
		VALUES ($2, $3, 'active', 'GBP', TRUE, $4, $1, 2, $5, 'any')
		RETURNING client_id::text`, passEnc, "smpp-srv-"+sfx, "smpp-srv-"+sfx+"@test.example", systemID, cidr).Scan(&clientID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, `DELETE FROM clients WHERE client_id=$1::uuid`, clientID) })

	cfg := &config.Config{
		SecretKey:      key,
		SMPPListenAddr: "127.0.0.1:0",
		SMPPSystemID:   "MiniSMS",
	}
	srv := New(pool, cfg, sending.New(pool, cfg))
	if err := srv.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer srv.Stop()

	tx := &smpp.Transmitter{Addr: srv.ln.Addr().String(), User: systemID, Passwd: passPlain}
	st := <-tx.Bind()
	if st.Error() != nil || st.Status() != smpp.Connected {
		t.Fatalf("bind: status=%v err=%v", st.Status(), st.Error())
	}
	_ = tx.Close()
}

func TestServer_RejectEnquireLinkBeforeBind(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	cfg := &config.Config{
		SecretKey:      []byte("0123456789abcdef0123456789abcdef"),
		SMPPListenAddr: "127.0.0.1:0",
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	srv := New(pool, cfg, sending.New(pool, cfg))
	if err := srv.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer srv.Stop()

	raw, err := net.Dial("tcp", srv.ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()

	el := pdu.NewEnquireLink()
	var buf bytes.Buffer
	if err := el.SerializeTo(&buf); err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Write(buf.Bytes()); err != nil {
		t.Fatal(err)
	}
	_ = raw.SetReadDeadline(time.Now().Add(2 * time.Second))
	readBuf := make([]byte, 512)
	n, readErr := raw.Read(readBuf)
	if n > 0 {
		t.Fatalf("expected connection closed without response, read %d bytes", n)
	}
	if readErr == nil {
		t.Fatal("expected read error after server closed unauthenticated enquire_link")
	}
}

func TestServer_BindBruteForceThrottle(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	key := []byte("0123456789abcdef0123456789abcdef")
	passEnc, err := db.EncryptValue(key, "real-secret")
	if err != nil {
		t.Fatal(err)
	}
	sfx := time.Now().UTC().Format("150405.000000000")
	systemID := "throttle-" + sfx
	var clientID string
	err = pool.QueryRow(ctx, `
		INSERT INTO clients (name, email, status, currency, smpp_ingress_enabled, smpp_system_id, smpp_password_enc, smpp_max_binds, allowed_sender_ids_mode)
		VALUES ($2, $3, 'active', 'GBP', TRUE, $4, $1, 2, 'any')
		RETURNING client_id::text`, passEnc, "throttle-"+sfx, "throttle-"+sfx+"@test.example", systemID).Scan(&clientID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, `DELETE FROM clients WHERE client_id=$1::uuid`, clientID) })

	cfg := &config.Config{SecretKey: key, SMPPListenAddr: "127.0.0.1:0"}
	srv := New(pool, cfg, sending.New(pool, cfg))
	if err := srv.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer srv.Stop()
	addr := srv.ln.Addr().String()

	for i := 0; i < bindMaxAttempts; i++ {
		tx := &smpp.Transmitter{Addr: addr, User: systemID, Passwd: "wrong-password"}
		st := <-tx.Bind()
		_ = tx.Close()
		if st.Error() == nil && st.Status() == smpp.Connected {
			t.Fatalf("bind %d should have failed", i+1)
		}
	}
	tx := &smpp.Transmitter{Addr: addr, User: systemID, Passwd: "wrong-password"}
	st := <-tx.Bind()
	_ = tx.Close()
	if st.Error() == nil && st.Status() == smpp.Connected {
		t.Fatal("expected throttle to block further bind attempts")
	}
}
