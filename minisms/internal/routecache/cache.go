// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package routecache

import (
	"context"
	"strings"
	"sync"

	"github.com/jackc/pgx/v5/pgxpool"
)

// RouteEntry is an active route row for longest-prefix matching.
type RouteEntry struct {
	RouteEntryID       string
	Prefix             string
	PrimaryCarrierID   string
	Failover1CarrierID *string
	Failover2CarrierID *string
}

// CarrierProfile is dispatch-relevant carrier configuration kept in RAM.
type CarrierProfile struct {
	CarrierID              string
	Name                   string
	Status                 string
	EgressTransport        string
	EndpointURL            string
	HTTPMethod             string
	SenderIDPolicy         string
	DefaultSenderIDValue   *string
	RateGroupID            *string
	DLRCallbackURLTemplate *string
	SMPPSourceAddrTON      string
	SMPPSourceAddrNPI      string
	SMPPDestAddrTON        string
	SMPPDestAddrNPI        string
}

// Cache holds routing tables and carrier profiles for fast sends.
type Cache struct {
	mu       sync.RWMutex
	routes   map[string][]RouteEntry
	carriers map[string]CarrierProfile
}

func New() *Cache {
	return &Cache{
		routes:   make(map[string][]RouteEntry),
		carriers: make(map[string]CarrierProfile),
	}
}

// Reload refreshes all route entries and carrier profiles from PostgreSQL.
func (c *Cache) Reload(ctx context.Context, pool *pgxpool.Pool) error {
	routes, err := loadRoutes(ctx, pool)
	if err != nil {
		return err
	}
	carriers, err := loadCarriers(ctx, pool)
	if err != nil {
		return err
	}
	c.mu.Lock()
	c.routes = routes
	c.carriers = carriers
	c.mu.Unlock()
	return nil
}

func loadRoutes(ctx context.Context, pool *pgxpool.Pool) (map[string][]RouteEntry, error) {
	rows, err := pool.Query(ctx, `
		SELECT routing_group_id::text, route_entry_id::text, prefix,
			primary_carrier_id::text, failover1_carrier_id::text, failover2_carrier_id::text
		FROM route_entries
		WHERE status = 'active'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string][]RouteEntry)
	for rows.Next() {
		var gid string
		var e RouteEntry
		if err := rows.Scan(&gid, &e.RouteEntryID, &e.Prefix, &e.PrimaryCarrierID, &e.Failover1CarrierID, &e.Failover2CarrierID); err != nil {
			return nil, err
		}
		out[gid] = append(out[gid], e)
	}
	return out, rows.Err()
}

func loadCarriers(ctx context.Context, pool *pgxpool.Pool) (map[string]CarrierProfile, error) {
	rows, err := pool.Query(ctx, `
		SELECT carrier_id::text, name, status, egress_transport, endpoint_url, http_method,
			sender_id_policy, default_sender_id_value, rate_group_id::text,
			dlr_callback_url_template,
			smpp_source_addr_ton, smpp_source_addr_npi, smpp_dest_addr_ton, smpp_dest_addr_npi
		FROM carriers`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]CarrierProfile)
	for rows.Next() {
		var p CarrierProfile
		if err := rows.Scan(
			&p.CarrierID, &p.Name, &p.Status, &p.EgressTransport, &p.EndpointURL, &p.HTTPMethod,
			&p.SenderIDPolicy, &p.DefaultSenderIDValue, &p.RateGroupID,
			&p.DLRCallbackURLTemplate,
			&p.SMPPSourceAddrTON, &p.SMPPSourceAddrNPI, &p.SMPPDestAddrTON, &p.SMPPDestAddrNPI,
		); err != nil {
			return nil, err
		}
		p.EgressTransport = strings.ToLower(strings.TrimSpace(p.EgressTransport))
		if p.EgressTransport != "smpp" {
			p.EgressTransport = "http"
		}
		out[p.CarrierID] = p
	}
	return out, rows.Err()
}

// LookupRoute returns the longest-prefix route for a routing group and destination.
func (c *Cache) LookupRoute(routingGroupID string, destination string) (*RouteEntry, bool) {
	if routingGroupID == "" {
		return nil, false
	}
	c.mu.RLock()
	entries := c.routes[routingGroupID]
	c.mu.RUnlock()
	if len(entries) == 0 {
		return nil, false
	}
	best := longestPrefix(entries, destination)
	if best == nil {
		return nil, false
	}
	return best, true
}

// Carrier returns a cached carrier profile.
func (c *Cache) Carrier(carrierID string) (CarrierProfile, bool) {
	c.mu.RLock()
	p, ok := c.carriers[carrierID]
	c.mu.RUnlock()
	return p, ok
}

func longestPrefix(entries []RouteEntry, destination string) *RouteEntry {
	dst := normalizeDigits(destination)
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

func normalizeDigits(destination string) string {
	d := strings.TrimPrefix(strings.TrimSpace(destination), "+")
	var b strings.Builder
	for _, r := range d {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}
