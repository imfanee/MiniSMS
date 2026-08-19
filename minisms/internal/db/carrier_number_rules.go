// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package db

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/minisms/minisms/internal/carrier/numrules"
)

// GetCarrierNumberRules returns a carrier's A/B number-translation rules (empty config when the carrier
// has none). The value is stored as JSONB in carriers.number_rules.
func GetCarrierNumberRules(ctx context.Context, pool *pgxpool.Pool, carrierID string) (numrules.Config, error) {
	var raw []byte
	err := pool.QueryRow(ctx, `SELECT COALESCE(number_rules, '{}'::jsonb) FROM carriers WHERE carrier_id = $1::uuid`, carrierID).Scan(&raw)
	if err != nil {
		return numrules.Config{}, err
	}
	return parseNumberRules(raw), nil
}

// SaveCarrierNumberRules persists a carrier's rule configuration. The caller should have validated it
// with numrules.Compile first (invalid regex / missing prefix rejected).
func SaveCarrierNumberRules(ctx context.Context, pool *pgxpool.Pool, carrierID string, cfg numrules.Config) error {
	b, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	_, err = pool.Exec(ctx, `UPDATE carriers SET number_rules = $2::jsonb, updated_at = now() WHERE carrier_id = $1::uuid`, carrierID, string(b))
	return err
}

// ListAllNumberRules loads every carrier's rule configuration in one query, keyed by carrier id, to warm
// the dispatch cache. Carriers with no rules ('{}') are still returned as an empty config.
func ListAllNumberRules(ctx context.Context, pool *pgxpool.Pool) (map[string]numrules.Config, error) {
	rows, err := pool.Query(ctx, `SELECT carrier_id::text, COALESCE(number_rules, '{}'::jsonb) FROM carriers`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]numrules.Config)
	for rows.Next() {
		var id string
		var raw []byte
		if err := rows.Scan(&id, &raw); err != nil {
			return nil, err
		}
		out[id] = parseNumberRules(raw)
	}
	return out, rows.Err()
}

func parseNumberRules(raw []byte) numrules.Config {
	var cfg numrules.Config
	if len(raw) == 0 {
		return cfg
	}
	_ = json.Unmarshal(raw, &cfg) // a malformed blob yields an empty config (pass-through), never a crash
	return cfg
}
