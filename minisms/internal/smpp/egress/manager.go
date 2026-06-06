// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package egress

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/minisms/minisms/internal/config"
	"github.com/minisms/minisms/internal/db"
	"github.com/minisms/minisms/internal/dlr"
)

// Manager supervises per-carrier SMPP ESME sessions and executes submit_sm.
type Manager struct {
	pool      *pgxpool.Pool
	cfg       *config.Config
	dlr       *dlr.Processor
	mu        sync.RWMutex
	sessions  map[string]*liveSession
	runCancel context.CancelFunc
}

func NewManager(pool *pgxpool.Pool, cfg *config.Config, dlrProc *dlr.Processor) *Manager {
	return &Manager{
		pool:     pool,
		cfg:      cfg,
		dlr:      dlrProc,
		sessions: make(map[string]*liveSession),
	}
}

// Start launches supervisors for carriers with SMPP egress configured.
func (m *Manager) Start(parent context.Context) {
	ctx, cancel := context.WithCancel(parent)
	m.runCancel = cancel
	m.refresh(ctx)
	go func() {
		ticker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				m.refresh(ctx)
			}
		}
	}()
}

func (m *Manager) refresh(ctx context.Context) {
	rows, err := db.ListCarriersSMPPEgress(ctx, m.pool)
	if err != nil {
		slog.Warn("smpp egress list carriers", "error", err)
		return
	}
	active := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		password, err := db.DecryptValue(m.cfg.SecretKey, row.SMPPPasswordEnc)
		if err != nil {
			slog.Warn("smpp egress decrypt password", "carrier_id", row.CarrierID, "error", err)
			continue
		}
		cc := carrierConfigFromRow(row, password, m.cfg)
		active[row.CarrierID] = struct{}{}
		m.mu.Lock()
		if _, ok := m.sessions[row.CarrierID]; !ok {
			sess := newLiveSession(cc, m.dlr)
			m.sessions[row.CarrierID] = sess
			go sess.run(ctx, func(status string) {
				_ = db.UpdateCarrierSMPPStatus(context.Background(), m.pool, row.CarrierID, status)
			})
		}
		m.mu.Unlock()
	}
	m.mu.Lock()
	for id, sess := range m.sessions {
		if _, ok := active[id]; !ok {
			sess.stop()
			delete(m.sessions, id)
			_ = db.UpdateCarrierSMPPStatus(context.Background(), m.pool, id, "disabled")
		}
	}
	m.mu.Unlock()
}

// Stop shuts down all carrier sessions.
func (m *Manager) Stop() {
	if m.runCancel != nil {
		m.runCancel()
	}
	m.mu.Lock()
	for _, sess := range m.sessions {
		sess.stop()
	}
	m.sessions = make(map[string]*liveSession)
	m.mu.Unlock()
}

// Submit sends via the carrier's SMPP session when bound.
func (m *Manager) Submit(ctx context.Context, carrierID string, req SubmitRequest) (*SubmitResult, error) {
	m.mu.RLock()
	sess := m.sessions[carrierID]
	m.mu.RUnlock()
	if sess == nil {
		return nil, ErrSessionUnavailable
	}
	return sess.submit(ctx, req)
}

// Ready reports whether the carrier has an active SMPP session.
func (m *Manager) Ready(carrierID string) bool {
	m.mu.RLock()
	sess := m.sessions[carrierID]
	m.mu.RUnlock()
	if sess == nil {
		return false
	}
	return sess.isReady()
}
