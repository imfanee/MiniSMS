// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package db

import (
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TestApplyPoolDefaults verifies the pool-exhaustion guardrails: a bare DSN gets the larger pool and a
// statement timeout (the pgx default of ~4 connections with no timeout was the root cause of the web
// hang), while any value the DSN sets explicitly is preserved.
func TestApplyPoolDefaults(t *testing.T) {
	parse := func(t *testing.T, dsn string) *pgxpool.Config {
		t.Helper()
		cfg, err := pgxpool.ParseConfig(dsn)
		if err != nil {
			t.Fatalf("parse %q: %v", dsn, err)
		}
		return cfg
	}

	t.Run("bare dsn gets larger pool and statement timeout", func(t *testing.T) {
		dsn := "postgres://u:p@localhost:5432/db"
		cfg := parse(t, dsn)
		applyPoolDefaults(cfg, dsn)
		if cfg.MaxConns != defaultPoolMaxConns {
			t.Errorf("MaxConns=%d want %d", cfg.MaxConns, defaultPoolMaxConns)
		}
		if cfg.MinConns != defaultPoolMinConns {
			t.Errorf("MinConns=%d want %d", cfg.MinConns, defaultPoolMinConns)
		}
		if got := cfg.ConnConfig.RuntimeParams["statement_timeout"]; got != defaultStatementTimeoutMS {
			t.Errorf("statement_timeout=%q want %q", got, defaultStatementTimeoutMS)
		}
	})

	t.Run("dsn pool_max_conns is preserved", func(t *testing.T) {
		dsn := "postgres://u:p@localhost:5432/db?pool_max_conns=50"
		cfg := parse(t, dsn)
		applyPoolDefaults(cfg, dsn)
		if cfg.MaxConns != 50 {
			t.Errorf("MaxConns=%d want 50 (DSN override must win)", cfg.MaxConns)
		}
	})

	t.Run("dsn statement_timeout is preserved", func(t *testing.T) {
		dsn := "postgres://u:p@localhost:5432/db?statement_timeout=5000"
		cfg := parse(t, dsn)
		applyPoolDefaults(cfg, dsn)
		if got := cfg.ConnConfig.RuntimeParams["statement_timeout"]; got != "5000" {
			t.Errorf("statement_timeout=%q want 5000 (DSN override must win)", got)
		}
	})

	t.Run("never leaves MaxConns below MinConns", func(t *testing.T) {
		dsn := "postgres://u:p@localhost:5432/db?pool_max_conns=1"
		cfg := parse(t, dsn)
		applyPoolDefaults(cfg, dsn)
		if cfg.MaxConns < cfg.MinConns {
			t.Errorf("MaxConns=%d < MinConns=%d", cfg.MaxConns, cfg.MinConns)
		}
	})
}
