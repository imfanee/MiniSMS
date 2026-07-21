// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package egress

import (
	"context"
	"net"
	"runtime"
	"testing"
	"time"
)

// TestBind_FailureTearsDownNoOrphan is the regression for the ghost-session root
// cause: go-smpp Bind() spawns a self-reconnecting goroutine, so a failed bind
// that returns without Close() would orphan it and it would keep re-binding to
// the SMSC forever. bind() must classify a transport failure (not an SMSC
// rejection), leave the session unbound, and not leak goroutines across retries.
func TestBind_FailureTearsDownNoOrphan(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close() // nothing listening now -> connections refused

	sess := newLiveSession(CarrierConfig{
		CarrierID: "c", Addr: addr, SystemID: "u", Password: "p",
		BindMode: "trx", EnquireLink: 30 * time.Second, WindowSize: 10, ThroughputPerSecond: 50,
	}, nil, nil, 1)

	time.Sleep(200 * time.Millisecond)
	before := runtime.NumGoroutine()

	for i := 0; i < 8; i++ {
		rejected, err := sess.bind(context.Background())
		if err == nil {
			t.Fatal("expected bind error against a dead address")
		}
		if rejected {
			t.Fatal("a transport/connect failure must not be classified as an SMSC rejection")
		}
		if sess.isReady() {
			t.Fatal("session must not be ready after a failed bind")
		}
	}

	time.Sleep(500 * time.Millisecond) // let torn-down library goroutines exit
	after := runtime.NumGoroutine()
	if after-before > 3 {
		t.Fatalf("possible orphaned reconnect goroutine leak: before=%d after=%d", before, after)
	}
}
