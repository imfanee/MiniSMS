// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package egress

import (
	"context"
	"crypto/tls"
	"log/slog"
	"sync"
	"time"

	"github.com/fiorix/go-smpp/v2/smpp"
	"github.com/fiorix/go-smpp/v2/smpp/pdu"
	"github.com/fiorix/go-smpp/v2/smpp/pdu/pdufield"
	"github.com/minisms/minisms/internal/dlr"
	"golang.org/x/time/rate"
)

type liveSession struct {
	cfg     CarrierConfig
	limiter *rate.Limiter
	dlr     *dlr.Processor

	mu      sync.RWMutex
	ready   bool
	status  <-chan smpp.ConnStatus
	tx      *smpp.Transmitter
	trx     *smpp.Transceiver
	cancel  context.CancelFunc
}

func newLiveSession(cfg CarrierConfig, dlrProc *dlr.Processor) *liveSession {
	lim := rate.NewLimiter(rate.Limit(cfg.ThroughputPerSecond), cfg.ThroughputPerSecond)
	if cfg.ThroughputPerSecond < 1 {
		lim = rate.NewLimiter(rate.Limit(50), 50)
	}
	return &liveSession{cfg: cfg, limiter: lim, dlr: dlrProc}
}

func (s *liveSession) run(ctx context.Context, onStatus func(string)) {
	ctx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	defer cancel()

	backoff := time.Second
	const maxBackoff = 60 * time.Second

	for {
		if ctx.Err() != nil {
			return
		}
		onStatus("binding")
		if err := s.bind(ctx); err != nil {
			slog.Warn("smpp egress bind failed", "carrier_id", s.cfg.CarrierID, "addr", s.cfg.Addr, "error", err)
			onStatus("down")
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			if backoff < maxBackoff {
				backoff *= 2
				if backoff > maxBackoff {
					backoff = maxBackoff
				}
			}
			continue
		}
		backoff = time.Second
		onStatus("up")
		s.mu.Lock()
		s.ready = true
		st := s.status
		s.mu.Unlock()

		disconnected := false
		for !disconnected {
			select {
			case <-ctx.Done():
				s.closeClient()
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
		s.closeClient()
		onStatus("down")
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
	}
}

func (s *liveSession) bind(ctx context.Context) error {
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
		if st.Error() != nil {
			return st.Error()
		}
		if st.Status() != smpp.Connected {
			if st.Error() != nil {
				return st.Error()
			}
			return smpp.ErrNotBound
		}
		s.mu.Lock()
		s.trx = trx
		s.tx = nil
		s.status = status
		s.mu.Unlock()
		return nil
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
		if st.Error() != nil {
			return st.Error()
		}
		if st.Status() != smpp.Connected {
			return st.Error()
		}
		s.mu.Lock()
		s.tx = tx
		s.trx = nil
		s.status = status
		s.mu.Unlock()
		return nil
	}
}

func (s *liveSession) handleDeliverSM(ctx context.Context, p pdu.Body) {
	if s.dlr == nil {
		return
	}
	f := p.Fields()
	sm := f[pdufield.ShortMessage]
	if sm == nil {
		return
	}
	body := sm.Bytes()
	receipt, err := pdufield.ParseDeliveryReceipt(body)
	if err != nil {
		return
	}
	s.dlr.HandleCarrierSMPP(ctx, s.cfg.CarrierID, receipt.ID, receipt.State)
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
	if trx != nil {
		return submitOn(ctx, trx, nil, req)
	}
	if tx != nil {
		return submitOn(ctx, tx, nil, req)
	}
	return nil, smpp.ErrNotBound
}
