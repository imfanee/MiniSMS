// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package egress

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"math/rand"
	"strconv"
	"sync"
	"time"

	"github.com/fiorix/go-smpp/v2/smpp"
	"github.com/fiorix/go-smpp/v2/smpp/pdu"
	"github.com/fiorix/go-smpp/v2/smpp/pdu/pdufield"
	"github.com/minisms/minisms/internal/dlr"
	"github.com/minisms/minisms/internal/wirelog"
	"github.com/minisms/minisms/internal/smpp/egresslog"
	"github.com/minisms/minisms/internal/smpp/smppstatus"
	"golang.org/x/time/rate"
)

// Reconnect pacing. A carrier SMSC may cap concurrent binds and reap dropped
// sessions slowly, so a fast lock-step reconnect loop piles up ghost sessions
// that count against that cap and cause ESME_RBINDFAIL cascades. We therefore
// use a higher floor, exponential backoff that does NOT reset on a fast flap,
// and per-bind jitter so parallel binds do not stampede the SMSC together.
const (
	minReconnect     = 3 * time.Second
	maxReconnect     = 60 * time.Second
	stableResetAfter = 45 * time.Second
	// When the SMSC explicitly rejects the bind (RBINDFAIL / BindFailed) it is at
	// its concurrent-bind cap or still holding our stale sessions. Back off much
	// longer so it can reap them before we retry, instead of storming the cap.
	capRejectBackoff = 45 * time.Second
	maxCapBackoff    = 5 * time.Minute
)

type liveSession struct {
	cfg     CarrierConfig
	limiter *rate.Limiter
	dlr     *dlr.Processor
	hub     *egresslog.Hub
	idx     int

	mu      sync.RWMutex
	ready   bool
	status  <-chan smpp.ConnStatus
	tx      *smpp.Transmitter
	trx     *smpp.Transceiver
	cancel  context.CancelFunc
}

func newLiveSession(cfg CarrierConfig, dlrProc *dlr.Processor, hub *egresslog.Hub, idx int) *liveSession {
	lim := rate.NewLimiter(rate.Limit(cfg.ThroughputPerSecond), cfg.ThroughputPerSecond)
	if cfg.ThroughputPerSecond < 1 {
		lim = rate.NewLimiter(rate.Limit(50), 50)
	}
	return &liveSession{cfg: cfg, limiter: lim, dlr: dlrProc, hub: hub, idx: idx}
}

// logEvent appends a session-scoped event to the per-carrier log hub (no-op when
// no hub is wired, e.g. in unit tests). It never carries credentials.
func (s *liveSession) logEvent(level, msg string, kv ...string) {
	if s.hub == nil {
		return
	}
	args := append([]string{"bind", "#" + strconv.Itoa(s.idx)}, kv...)
	s.hub.Event(s.cfg.CarrierID, level, msg, args...)
}

// wire appends a directional PDU/lifecycle entry to the per-carrier SMPP wire log file (no-op unless
// wire logging is enabled). dir is ">>" sent, "<<" received, "--" lifecycle. Never carries the password.
func (s *liveSession) wire(dir, event string, kv ...string) {
	if !wirelog.Enabled() {
		return
	}
	args := append([]string{"bind", "#" + strconv.Itoa(s.idx)}, kv...)
	wirelog.Emit(s.cfg.Name, "smpp", dir, event, args...)
}

// logDebug emits a verbose (DEBUG-level) event only while an operator has turned
// on deep logging for this carrier in the SMPP log viewer. Off by default so the
// ring buffer keeps useful history and the hot path stays cheap.
func (s *liveSession) logDebug(msg string, kv ...string) {
	if s.hub == nil || !s.hub.Debug(s.cfg.CarrierID) {
		return
	}
	args := append([]string{"bind", "#" + strconv.Itoa(s.idx)}, kv...)
	s.hub.Event(s.cfg.CarrierID, "DEBUG", msg, args...)
}

func (s *liveSession) run(ctx context.Context, onStatus func(string)) {
	ctx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	defer cancel()

	// Stagger parallel binds at startup so a session group does not open all N
	// binds in the same instant (a thundering herd against a concurrent-bind cap).
	if d := startupStagger(s.idx); d > 0 {
		if !sleepCtx(ctx, d) {
			return
		}
	}

	backoff := minReconnect
	for {
		if ctx.Err() != nil {
			return
		}
		onStatus("binding")
		s.logEvent("INFO", "bind attempt", "addr", s.cfg.Addr, "mode", s.cfg.BindMode)
		s.logDebug("bind attempt detail", "addr", s.cfg.Addr, "system_id", s.cfg.SystemID, "mode", s.cfg.BindMode,
			"enquire_link_s", strconv.Itoa(int(s.cfg.EnquireLink/time.Second)), "window", strconv.Itoa(int(s.cfg.WindowSize)))
		s.wire(">>", "bind_"+s.cfg.BindMode, "addr", s.cfg.Addr, "system_id", s.cfg.SystemID,
			"password", wirelog.Mask(s.cfg.Password), "system_type", s.cfg.SystemType)
		if rejected, code, err := s.bind(ctx); err != nil {
			ceiling := maxReconnect
			if rejected {
				// SMSC answered and refused the bind: it is at its concurrent-bind
				// cap or still holding our stale sessions. Hold a long floor so it
				// can reap them, and escalate toward a multi-minute ceiling. Surface
				// the exact command_status so the reason is unambiguous (an SMPP
				// rejection, not a network/firewall block) and shareable.
				ceiling = maxCapBackoff
				if backoff < capRejectBackoff {
					backoff = capRejectBackoff
				}
				name, desc := smppstatus.Describe(code)
				slog.Warn("smpp egress bind rejected", "carrier_id", s.cfg.CarrierID, "addr", s.cfg.Addr, "command_status", fmt.Sprintf("0x%08X", uint32(code)), "code", name)
				s.logEvent("ERROR", "bind rejected by SMSC",
					"command_status", fmt.Sprintf("0x%08X", uint32(code)), "code", name, "detail", desc, "retry_in", backoff.String())
				s.wire("<<", "bind_resp_rejected",
					"command_status", fmt.Sprintf("0x%08X", uint32(code)), "code", name, "retry_in", backoff.String())
			} else {
				// No SMPP reply: TCP/transport failure (dial refused/timeout/reset).
				// Call this out explicitly so it is not mistaken for a bind rejection.
				slog.Warn("smpp egress bind failed", "carrier_id", s.cfg.CarrierID, "addr", s.cfg.Addr, "error", err)
				s.logEvent("ERROR", "bind failed (network/transport, SMPP bind not reached)",
					"error", err.Error(), "retry_in", backoff.String())
				s.wire("--", "bind_failed_transport", "addr", s.cfg.Addr, "error", err.Error(), "retry_in", backoff.String())
			}
			onStatus("down")
			if !sleepCtx(ctx, jitter(backoff)) {
				return
			}
			backoff = nextBackoff(backoff, ceiling)
			continue
		}
		s.logEvent("INFO", "bind established", "addr", s.cfg.Addr)
		s.wire("<<", "bind_resp_ok", "addr", s.cfg.Addr)
		onStatus("up")
		upSince := time.Now()
		s.mu.Lock()
		s.ready = true
		st := s.status
		s.mu.Unlock()

		disconnected := false
		for !disconnected {
			select {
			case <-ctx.Done():
				s.closeClient() // library Close() sends a clean unbind first
				return
			case c, ok := <-st:
				if !ok {
					disconnected = true
					break
				}
				if c.Error() != nil || c.Status() == smpp.Disconnected || c.Status() == smpp.ConnectionFailed || c.Status() == smpp.BindFailed {
					disconnected = true
				}
			}
		}
		s.mu.Lock()
		s.ready = false
		s.mu.Unlock()
		s.closeClient() // attempt graceful unbind before reconnecting

		// Only reset the backoff floor when the session stayed up long enough to
		// count as stable. A fast flap keeps escalating the delay so we do not
		// hammer the SMSC and pile up ghost sessions it has not yet reaped.
		uptime := time.Since(upSince)
		if uptime >= stableResetAfter {
			backoff = minReconnect
		} else {
			backoff = nextBackoff(backoff, maxReconnect)
		}
		s.logEvent("WARN", "session disconnected", "uptime", uptime.Truncate(time.Second).String(), "reconnect_in", backoff.String())
		s.wire("--", "disconnected", "uptime", uptime.Truncate(time.Second).String(), "reconnect_in", backoff.String())
		onStatus("down")
		if !sleepCtx(ctx, jitter(backoff)) {
			return
		}
	}
}

// sleepCtx waits for d or until ctx is cancelled. Returns false if cancelled.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// nextBackoff doubles the delay, clamped to [minReconnect, ceiling].
func nextBackoff(cur, ceiling time.Duration) time.Duration {
	next := cur * 2
	if next > ceiling {
		next = ceiling
	}
	if next < minReconnect {
		next = minReconnect
	}
	return next
}

// jitter returns d +/- up to 25% so parallel binds do not reconnect in lock-step.
func jitter(d time.Duration) time.Duration {
	if d <= 0 {
		return d
	}
	span := int64(d) / 4
	if span <= 0 {
		return d
	}
	return d + time.Duration(rand.Int63n(2*span)-span)
}

// startupStagger spreads binds #2, #3, ... over a short jittered window.
func startupStagger(idx int) time.Duration {
	if idx <= 1 {
		return 0
	}
	return jitter(time.Duration(idx-1) * 400 * time.Millisecond)
}

// bind opens one ESME session. It returns rejected=true when the SMSC answered
// and refused the bind (BindFailed / RBINDFAIL) so the caller can apply a long,
// cap-aware backoff. It ALWAYS tears down a failed client: the go-smpp Bind()
// spawns a self-reconnecting goroutine, so returning without Close() would orphan
// it and it would keep re-binding to the SMSC forever, piling up ghost sessions.
func (s *liveSession) bind(ctx context.Context) (rejected bool, code int, err error) {
	s.closeClient()
	respTimeout := 5 * time.Second
	enquire := s.cfg.EnquireLink
	if enquire < 5*time.Second {
		enquire = 30 * time.Second
	}
	var tlsCfg *tls.Config
	if s.cfg.TLS {
		tlsCfg = &tls.Config{MinVersion: tls.VersionTLS12}
	}
	handler := func(p pdu.Body) {
		if p == nil || p.Header().ID != pdu.DeliverSMID {
			return
		}
		s.handleDeliverSM(context.Background(), p)
	}
	switch s.cfg.BindMode {
	case "trx":
		trx := &smpp.Transceiver{
			Addr:        s.cfg.Addr,
			User:        s.cfg.SystemID,
			Passwd:      s.cfg.Password,
			SystemType:  s.cfg.SystemType,
			EnquireLink: enquire,
			RespTimeout: respTimeout,
			TLS:         tlsCfg,
			Handler:     handler,
			RateLimiter: s.limiter,
			WindowSize:  s.cfg.WindowSize,
		}
		status := trx.Bind()
		st := <-status
		if st.Status() != smpp.Connected {
			rej := st.Status() == smpp.BindFailed
			_ = trx.Close() // stop the library's self-reconnect goroutine + unbind
			return rej, bindStatusCode(st), bindErr(st)
		}
		s.mu.Lock()
		s.trx = trx
		s.tx = nil
		s.status = status
		s.mu.Unlock()
		return false, 0, nil
	default:
		tx := &smpp.Transmitter{
			Addr:        s.cfg.Addr,
			User:        s.cfg.SystemID,
			Passwd:      s.cfg.Password,
			SystemType:  s.cfg.SystemType,
			EnquireLink: enquire,
			RespTimeout: respTimeout,
			TLS:         tlsCfg,
			RateLimiter: s.limiter,
			WindowSize:  s.cfg.WindowSize,
		}
		status := tx.Bind()
		st := <-status
		if st.Status() != smpp.Connected {
			rej := st.Status() == smpp.BindFailed
			_ = tx.Close() // stop the library's self-reconnect goroutine + unbind
			return rej, bindStatusCode(st), bindErr(st)
		}
		s.mu.Lock()
		s.tx = tx
		s.trx = nil
		s.status = status
		s.mu.Unlock()
		return false, 0, nil
	}
}

// bindStatusCode extracts the SMPP command_status from a failed bind. The
// go-smpp library returns the bind response status as a pdu.Status error, so a
// rejection carries the real code (e.g. 0x0D ESME_RBINDFAIL); 0 otherwise.
func bindStatusCode(st smpp.ConnStatus) int {
	if ps, ok := st.Error().(pdu.Status); ok {
		return int(ps)
	}
	return 0
}

func bindErr(st smpp.ConnStatus) error {
	if e := st.Error(); e != nil {
		return e
	}
	return smpp.ErrNotBound
}

func (s *liveSession) handleDeliverSM(ctx context.Context, p pdu.Body) {
	f := p.Fields()
	sm := f[pdufield.ShortMessage]
	if sm == nil {
		return
	}
	body := sm.Bytes()
	receipt, err := pdufield.ParseDeliveryReceipt(body)
	if err != nil {
		s.logEvent("WARN", "deliver_sm parse failed", "error", err.Error())
		slog.Warn("smpp egress deliver_sm parse failed", "carrier_id", s.cfg.CarrierID, "bind", s.idx, "error", err.Error())
		return
	}
	// Ring buffer (live SMPP logs window) plus a durable journald line, so every carrier receipt
	// survives a restart and can be reconciled against sms_logs later.
	s.logEvent("INFO", "deliver_sm receipt", "smsc_id", receipt.ID, "stat", receipt.State)
	slog.Info("smpp egress deliver_sm", "carrier_id", s.cfg.CarrierID, "bind", s.idx, "smsc_id", receipt.ID, "stat", receipt.State)
	s.wire("<<", "deliver_sm", "smsc_id", receipt.ID, "stat", receipt.State, "err", receipt.Err,
		"submitted", strconv.Itoa(receipt.Submitted), "delivered", strconv.Itoa(receipt.Delivered))
	s.logDebug("deliver_sm detail", "smsc_id", receipt.ID, "stat", receipt.State, "err", receipt.Err,
		"submitted", strconv.Itoa(receipt.Submitted), "delivered", strconv.Itoa(receipt.Delivered))
	if s.dlr == nil {
		return
	}
	switch s.dlr.HandleCarrierSMPP(ctx, s.cfg.CarrierID, receipt.ID, receipt.State) {
	case dlr.CarrierDLRUnmatched:
		// The carrier sent a receipt whose id maps to no message. Make it loud and countable instead
		// of dropping it: this is the definitive signal for "did we miss a DLR update".
		total := s.hub.IncUnmatched(s.cfg.CarrierID)
		s.logEvent("WARN", "deliver_sm UNMATCHED (no message for id)", "smsc_id", receipt.ID, "stat", receipt.State, "unmatched_total", strconv.FormatInt(total, 10))
		slog.Warn("smpp egress deliver_sm unmatched", "carrier_id", s.cfg.CarrierID, "bind", s.idx, "smsc_id", receipt.ID, "stat", receipt.State, "unmatched_total", total)
	case dlr.CarrierDLRError:
		s.logEvent("ERROR", "deliver_sm correlation lookup failed", "smsc_id", receipt.ID, "stat", receipt.State)
	}
}

func (s *liveSession) closeClient() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status = nil
	if s.trx != nil {
		_ = s.trx.Close()
		s.trx = nil
	}
	if s.tx != nil {
		_ = s.tx.Close()
		s.tx = nil
	}
}

func (s *liveSession) stop() {
	if s.cancel != nil {
		s.cancel()
	}
	s.closeClient()
}

func (s *liveSession) isReady() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.ready
}

func (s *liveSession) submit(ctx context.Context, req SubmitRequest) (*SubmitResult, error) {
	s.mu.RLock()
	tx, trx := s.tx, s.trx
	s.mu.RUnlock()
	if !s.isReady() {
		return nil, smpp.ErrNotBound
	}
	var res *SubmitResult
	var err error
	switch {
	case trx != nil:
		res, err = submitOn(ctx, trx, nil, req)
	case tx != nil:
		res, err = submitOn(ctx, tx, nil, req)
	default:
		return nil, smpp.ErrNotBound
	}
	s.wire(">>", "submit_sm", "src", req.Src, "dst", req.Dst, "segments", strconv.Itoa(req.Segments), "encoding", req.Encoding)
	// Surface submit problems for debugging without logging message content.
	if err != nil {
		s.logEvent("ERROR", "submit_sm error", "dst", req.Dst, "error", err.Error())
		s.wire("<<", "submit_sm_resp", "dst", req.Dst, "error", err.Error())
	} else if res != nil && res.CommandStatus != 0 {
		name, desc := smppstatus.Describe(res.CommandStatus)
		s.logEvent("ERROR", "submit_sm rejected", "dst", req.Dst,
			"command_status", fmt.Sprintf("0x%08X", uint32(res.CommandStatus)), "code", name, "detail", desc)
		s.wire("<<", "submit_sm_resp", "dst", req.Dst,
			"command_status", fmt.Sprintf("0x%08X", uint32(res.CommandStatus)), "code", name)
	} else if res != nil {
		s.logDebug("submit_sm accepted", "dst", req.Dst, "segments", strconv.Itoa(req.Segments),
			"smsc_id", res.CarrierMessageID, "command_status", "0x00000000 ESME_ROK")
		s.wire("<<", "submit_sm_resp", "dst", req.Dst, "smsc_id", res.CarrierMessageID, "command_status", "0x00000000 ESME_ROK")
	}
	return res, err
}
