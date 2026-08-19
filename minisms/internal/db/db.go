// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package db

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Pool sizing defaults. The pgx default MaxConns is max(4, NumCPU), which on a small host is only about
// 4 connections. Under concurrent admin polling plus API traffic that tiny pool saturates, and because
// the library applies no statement timeout, a single blocked query (or a burst that outnumbers the
// connections) wedges every further request in pool.Acquire indefinitely: the web stops responding
// (nginx 504) with no CPU load, and only a restart clears it. We therefore set a larger pool and a
// server-side statement timeout so a slow query aborts and frees its connection instead of hanging the
// whole service. Every value is overridable from the DSN (pool_max_conns=, pool_min_conns=,
// statement_timeout=, ...), so operations can still tune without a rebuild.
const (
	defaultPoolMaxConns       = 20
	defaultPoolMinConns       = 2
	defaultStatementTimeoutMS = "30000" // 30s: no legitimate query on this workload runs longer.
)

// NewPool opens a connection pool to PostgreSQL.
func NewPool(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	applyPoolDefaults(cfg, databaseURL)
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("connect database: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return pool, nil
}

// applyPoolDefaults sets safe pool sizing and a statement timeout, but never overrides a value the DSN
// already specified (pgxpool.ParseConfig has already applied those). Kept as a pure function so the
// sizing policy is unit-tested without a live database.
func applyPoolDefaults(cfg *pgxpool.Config, databaseURL string) {
	if !strings.Contains(databaseURL, "pool_max_conns") {
		cfg.MaxConns = defaultPoolMaxConns
	}
	if !strings.Contains(databaseURL, "pool_min_conns") && cfg.MinConns < defaultPoolMinConns {
		cfg.MinConns = defaultPoolMinConns
	}
	// Keep pool hygiene sane (pgx defaults are already 1h / 30m / 1m, but be explicit and never leave
	// MaxConns below MinConns if a DSN set an odd combination).
	if cfg.MaxConnLifetime == 0 {
		cfg.MaxConnLifetime = time.Hour
	}
	if cfg.MaxConnIdleTime == 0 {
		cfg.MaxConnIdleTime = 30 * time.Minute
	}
	if cfg.HealthCheckPeriod == 0 {
		cfg.HealthCheckPeriod = time.Minute
	}
	if cfg.MaxConns < cfg.MinConns {
		cfg.MaxConns = cfg.MinConns
	}
	if cfg.ConnConfig.RuntimeParams == nil {
		cfg.ConnConfig.RuntimeParams = map[string]string{}
	}
	if _, ok := cfg.ConnConfig.RuntimeParams["statement_timeout"]; !ok {
		cfg.ConnConfig.RuntimeParams["statement_timeout"] = defaultStatementTimeoutMS
	}
}
