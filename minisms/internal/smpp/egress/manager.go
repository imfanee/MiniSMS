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
	"github.com/minisms/minisms/internal/smpp/egresslog"
)

// Manager supervises per-carrier SMPP ESME session groups and executes
// submit_sm. Each carrier may run several parallel binds (smpp_bind_count);
// the group load-balances submits and aggregates delivery receipts.
type Manager struct {
	pool      *pgxpool.Pool
	cfg       *config.Config
	dlr       *dlr.Processor
	logHub    *egresslog.Hub
	mu        sync.RWMutex
	groups    map[string]*sessionGroup
	runCtx    context.Context
	runCancel context.CancelFunc
}

func NewManager(pool *pgxpool.Pool, cfg *config.Config, dlrProc *dlr.Processor) *Manager {
	return &Manager{
		pool:   pool,
		cfg:    cfg,
		dlr:    dlrProc,
		logHub: egresslog.NewHub(),
		groups: make(map[string]*sessionGroup),
	}
}

// LogHub returns the per-carrier SMPP session log hub for live admin tailing.
func (m *Manager) LogHub() *egresslog.Hub { return m.logHub }

// BindStatus reports how many of the carrier's parallel binds are currently up
// and the configured total. present is false when no session group exists (the
// carrier is not an active SMPP egress carrier).
func (m *Manager) BindStatus(carrierID string) (ready, total int, present bool) {
	m.mu.RLock()
	g := m.groups[carrierID]
	m.mu.RUnlock()
	if g == nil {
		return 0, 0, false
	}
	return g.readyCount(), len(g.sessions), true
}

// Restart tears down and immediately rebinds the carrier's SMPP session group.
// Used for troubleshooting (carriers sometimes ask for a bind restart). The log
// hub records the request and the fresh bind attempts stream to any live viewer.
func (m *Manager) Restart(carrierID string) {
	m.logHub.Event(carrierID, "WARN", "restart requested via admin")
	m.mu.Lock()
	if g := m.groups[carrierID]; g != nil {
		g.stop()
		delete(m.groups, carrierID)
	}
	ctx := m.runCtx
	m.mu.Unlock()
	// Rebind now rather than waiting for the periodic refresh tick.
	if ctx != nil {
		m.refresh(ctx)
	}
}

// Start launches supervisors for carriers with SMPP egress configured.
func (m *Manager) Start(parent context.Context) {
	ctx, cancel := context.WithCancel(parent)
	m.runCtx = ctx
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
		carrierID := row.CarrierID
		active[carrierID] = struct{}{}
		m.mu.Lock()
		existing := m.groups[carrierID]
		// Rebuild the group if the bind count changed so admins can scale
		// parallel binds without a full restart (next refresh tick).
		if existing != nil && existing.bindCount != cc.BindCount {
			existing.stop()
			delete(m.groups, carrierID)
			existing = nil
		}
		if existing == nil {
			g := newSessionGroup(cc, m.dlr, m.logHub)
			m.groups[carrierID] = g
			g.start(ctx, func(status string) {
				_ = db.UpdateCarrierSMPPStatus(context.Background(), m.pool, carrierID, status)
			})
		}
		m.mu.Unlock()
	}
	m.mu.Lock()
	for id, g := range m.groups {
		if _, ok := active[id]; !ok {
			g.stop()
			delete(m.groups, id)
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
	for _, g := range m.groups {
		g.stop()
	}
	m.groups = make(map[string]*sessionGroup)
	m.mu.Unlock()
}

// Submit sends via one of the carrier's SMPP sessions when bound.
func (m *Manager) Submit(ctx context.Context, carrierID string, req SubmitRequest) (*SubmitResult, error) {
	m.mu.RLock()
	g := m.groups[carrierID]
	m.mu.RUnlock()
	if g == nil {
		return nil, ErrSessionUnavailable
	}
	return g.submit(ctx, req)
}

// Ready reports whether the carrier has at least one active SMPP session.
func (m *Manager) Ready(carrierID string) bool {
	m.mu.RLock()
	g := m.groups[carrierID]
	m.mu.RUnlock()
	if g == nil {
		return false
	}
	return g.ready()
}
