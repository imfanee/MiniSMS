// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package sending

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// RouteEntry is the active route row used for carrier failover.
type RouteEntry struct {
	RouteEntryID       string
	Prefix             string
	PrimaryCarrierID   string
	Failover1CarrierID *string
	Failover2CarrierID *string
}

func (s *Service) lookupRouteEntry(ctx context.Context, routingGroupID *string, to string) (*RouteEntry, error) {
	if routingGroupID == nil || *routingGroupID == "" {
		return nil, pgx.ErrNoRows
	}
	if s.Routes != nil {
		if re, ok := s.Routes.LookupRoute(*routingGroupID, to); ok {
			return &RouteEntry{
				RouteEntryID:       re.RouteEntryID,
				Prefix:             re.Prefix,
				PrimaryCarrierID:   re.PrimaryCarrierID,
				Failover1CarrierID: re.Failover1CarrierID,
				Failover2CarrierID: re.Failover2CarrierID,
			}, nil
		}
		return nil, pgx.ErrNoRows
	}
	return lookupRouteEntryDB(ctx, s.Pool, routingGroupID, to)
}

func lookupRouteEntryDB(ctx context.Context, pool *pgxpool.Pool, routingGroupID *string, to string) (*RouteEntry, error) {
	if routingGroupID == nil || *routingGroupID == "" {
		return nil, pgx.ErrNoRows
	}
	rows, err := pool.Query(ctx, `
		SELECT route_entry_id::text, prefix, primary_carrier_id::text, failover1_carrier_id::text, failover2_carrier_id::text
		FROM route_entries
		WHERE routing_group_id = $1::uuid AND status='active'`, *routingGroupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var entries []RouteEntry
	for rows.Next() {
		var e RouteEntry
		if err := rows.Scan(&e.RouteEntryID, &e.Prefix, &e.PrimaryCarrierID, &e.Failover1CarrierID, &e.Failover2CarrierID); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	best := longestPrefixRoute(entries, to)
	if best == nil {
		return nil, pgx.ErrNoRows
	}
	return best, nil
}

func longestPrefixRoute(entries []RouteEntry, destination string) *RouteEntry {
	dst := billingSegmentNormalize(destination)
	var catchAll *RouteEntry
	var best *RouteEntry
	bestLen := -1
	for i := range entries {
		e := &entries[i]
		if e.Prefix == "*" {
			if catchAll == nil {
				catchAll = e
			}
			continue
		}
		if strings.HasPrefix(dst, e.Prefix) && len(e.Prefix) > bestLen {
			best = e
			bestLen = len(e.Prefix)
		}
	}
	if best != nil {
		return best
	}
	return catchAll
}

func billingSegmentNormalize(destination string) string {
	d := strings.TrimPrefix(strings.TrimSpace(destination), "+")
	var b strings.Builder
	for _, r := range d {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}
