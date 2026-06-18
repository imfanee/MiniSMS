// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package egress

import (
	"context"
	"sync/atomic"

	"github.com/minisms/minisms/internal/dlr"
	"github.com/minisms/minisms/internal/smpp/egresslog"
)

// sessionGroup supervises N parallel SMPP ESME sessions for one carrier and
// load-balances submit_sm across the ready sessions (round-robin). Several
// carriers (for example Airtel DRC) advise multiple parallel transceiver binds
// for throughput and to let the SMSC distribute delivery receipts. A deliver_sm
// may arrive on any of the sessions; correlation is carrier-wide (by
// carrier_message_id via dlr.HandleCarrierSMPP), so every session feeds the same
// DLR processor and the bind that receives a receipt does not have to be the one
// that submitted the message.
type sessionGroup struct {
	carrierID string
	bindCount int
	sessions  []*liveSession
	rr        uint64
}

func newSessionGroup(cc CarrierConfig, dlrProc *dlr.Processor, hub *egresslog.Hub) *sessionGroup {
	n := cc.BindCount
	if n < 1 {
		n = 1
	}
	g := &sessionGroup{carrierID: cc.CarrierID, bindCount: n, sessions: make([]*liveSession, 0, n)}
	for i := 0; i < n; i++ {
		g.sessions = append(g.sessions, newLiveSession(cc, dlrProc, hub, i+1))
	}
	return g
}

// start launches every session in the group. The carrier-level status callback
// is invoked with the aggregate status whenever any session changes state.
func (g *sessionGroup) start(ctx context.Context, onStatus func(string)) {
	for _, s := range g.sessions {
		sess := s
		go sess.run(ctx, func(string) { onStatus(g.aggregateStatus()) })
	}
}

func (g *sessionGroup) readyCount() int {
	n := 0
	for _, s := range g.sessions {
		if s.isReady() {
			n++
		}
	}
	return n
}

func (g *sessionGroup) aggregateStatus() string {
	if g.readyCount() > 0 {
		return "up"
	}
	return "down"
}

func (g *sessionGroup) ready() bool {
	return g.readyCount() > 0
}

// submit picks the next ready session round-robin and submits on it. It scans
// the whole group so a single down session does not strand a submit.
func (g *sessionGroup) submit(ctx context.Context, req SubmitRequest) (*SubmitResult, error) {
	n := len(g.sessions)
	if n == 0 {
		return nil, ErrSessionUnavailable
	}
	start := int(atomic.AddUint64(&g.rr, 1))
	for i := 0; i < n; i++ {
		s := g.sessions[(start+i)%n]
		if s.isReady() {
			return s.submit(ctx, req)
		}
	}
	return nil, ErrSessionUnavailable
}

func (g *sessionGroup) stop() {
	for _, s := range g.sessions {
		s.stop()
	}
}
