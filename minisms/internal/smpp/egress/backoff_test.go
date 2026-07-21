// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package egress

import (
	"testing"
	"time"
)

func TestNextBackoff_EscalatesAndClamps(t *testing.T) {
	// Doubles from the floor, never below floor, never above ceiling.
	if got := nextBackoff(minReconnect, maxReconnect); got != 2*minReconnect {
		t.Fatalf("nextBackoff(min)=%v want %v", got, 2*minReconnect)
	}
	if got := nextBackoff(0, maxReconnect); got != minReconnect {
		t.Fatalf("nextBackoff(0)=%v want floor %v", got, minReconnect)
	}
	d := minReconnect
	for i := 0; i < 20; i++ {
		d = nextBackoff(d, maxReconnect)
	}
	if d != maxReconnect {
		t.Fatalf("backoff did not clamp to transport max: got %v want %v", d, maxReconnect)
	}
	// Cap-rejection backoff escalates toward the longer cap ceiling.
	d = capRejectBackoff
	for i := 0; i < 20; i++ {
		d = nextBackoff(d, maxCapBackoff)
	}
	if d != maxCapBackoff {
		t.Fatalf("cap backoff did not clamp to maxCapBackoff: got %v want %v", d, maxCapBackoff)
	}
}

func TestJitter_WithinBounds(t *testing.T) {
	base := 8 * time.Second
	lo, hi := base-base/4, base+base/4
	for i := 0; i < 1000; i++ {
		j := jitter(base)
		if j < lo || j > hi {
			t.Fatalf("jitter %v out of [%v,%v]", j, lo, hi)
		}
	}
	if jitter(0) != 0 {
		t.Fatal("jitter(0) should be 0")
	}
}

func TestStartupStagger_ByIndex(t *testing.T) {
	if startupStagger(1) != 0 {
		t.Fatal("bind #1 must not stagger")
	}
	// Bind #4 staggers around 3*400ms = 1.2s (+/-25%), always positive.
	for i := 0; i < 200; i++ {
		if d := startupStagger(4); d <= 0 || d > 2*time.Second {
			t.Fatalf("startupStagger(4)=%v out of expected range", d)
		}
	}
}
