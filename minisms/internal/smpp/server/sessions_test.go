// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package server

import "testing"

func TestSessionRegistry_CountRemoveCloseClient(t *testing.T) {
	r := newSessionRegistry()
	a := &session{clientID: "c1", mode: bindTRX}
	b := &session{clientID: "c1", mode: bindRX}
	other := &session{clientID: "c2", mode: bindTX}
	r.add(a)
	r.add(b)
	r.add(other)

	if got := r.bindCount("c1"); got != 2 {
		t.Fatalf("c1 bindCount=%d want 2", got)
	}
	if got := r.bindCount("c2"); got != 1 {
		t.Fatalf("c2 bindCount=%d want 1", got)
	}

	// remove reports presence and decrements; second remove is a no-op.
	if !r.remove(a) {
		t.Fatal("remove(a) should report present")
	}
	if r.remove(a) {
		t.Fatal("double remove(a) should report absent")
	}
	if got := r.bindCount("c1"); got != 1 {
		t.Fatalf("after remove c1 bindCount=%d want 1", got)
	}

	// closeClient on a client with a nil-conn session must not panic and leaves
	// counts intact (handleConn would remove the session on its own disconnect).
	r.closeClient("c1")
	if got := r.bindCount("c1"); got != 1 {
		t.Fatalf("closeClient changed count unexpectedly: %d", got)
	}
}
