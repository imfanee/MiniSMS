// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package routecache

import (
	"context"
	"strings"
	"sync"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/minisms/minisms/internal/carrier/numrules"
	"github.com/minisms/minisms/internal/db"
)

// RouteEntry is an active route row for longest-prefix matching.
type RouteEntry struct {
	RouteEntryID       string
	Prefix             string
	PrimaryCarrierID   string
	Failover1CarrierID *string
	Failover2CarrierID *string
	DistributionMode   string
	PrimaryWeight      int
	Failover1Weight    int
	Failover2Weight    int
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

// Cache holds routing tables, carrier profiles, and per-carrier HTTP dispatch config (request template
// and decrypted auth headers) for fast sends. Caching the template + headers removes two DB round-trips
// per HTTP send; the cache is reloaded on every carrier/route/template/header admin edit, so it never
// serves stale config. Balance-sensitive checks (carrier eligibility / in-loss protection) are NOT
// cached and always hit the DB, so a low-balance carrier is skipped in real time.
type Cache struct {
	mu        sync.RWMutex
	secretKey []byte
	routes    map[string][]RouteEntry
	carriers  map[string]CarrierProfile
	templates map[string]*db.RequestTemplate
	headers   map[string][]db.AuthHeaderRow
	numRules  map[string]*numrules.Compiled
}

func New() *Cache {
	// templates/headers stay nil until a successful Reload populates them, so their nil-ness is the
	// "not warmed" signal: accessors then report a miss and dispatch falls back to a DB read rather than
	// wrongly treating a carrier as having no template/headers.
	return &Cache{
		routes:   make(map[string][]RouteEntry),
		carriers: make(map[string]CarrierProfile),
	}
}

// SetSecretKey provides the AES key used to decrypt cached auth headers. Call once before the first
// Reload; without it, header caching is skipped and dispatch falls back to a per-message DB read.
func (c *Cache) SetSecretKey(key []byte) {
	c.mu.Lock()
	c.secretKey = key
	c.mu.Unlock()
}

// Reload refreshes all route entries, carrier profiles, request templates, and auth headers from
// PostgreSQL. Header/template loads are best-effort: a failure there does not abort the reload (dispatch
// falls back to a per-message DB read), so a template/header hiccup never blocks routing config.
func (c *Cache) Reload(ctx context.Context, pool *pgxpool.Pool) error {
	routes, err := loadRoutes(ctx, pool)
	if err != nil {
		return err
	}
	carriers, err := loadCarriers(ctx, pool)
	if err != nil {
		return err
	}
	templates, terr := db.ListAllRequestTemplates(ctx, pool)
	if terr != nil {
		templates = nil
	}
	c.mu.RLock()
	key := c.secretKey
	c.mu.RUnlock()
	var headers map[string][]db.AuthHeaderRow
	if len(key) > 0 {
		if h, herr := db.ListAllAuthHeaders(ctx, pool, key); herr == nil {
			headers = h
		}
	}
	// Compile each carrier's number-translation rules once here (regexes precompiled), so per-message
	// dispatch is a pure in-memory transform. A carrier with no/invalid rules maps to nil (pass-through).
	var numRules map[string]*numrules.Compiled
	if cfgs, nerr := db.ListAllNumberRules(ctx, pool); nerr == nil {
		numRules = make(map[string]*numrules.Compiled, len(cfgs))
		for id, cfg := range cfgs {
			if compiled, cerr := numrules.Compile(cfg); cerr == nil {
				numRules[id] = compiled
			}
		}
	}
	c.mu.Lock()
	c.routes = routes
	c.carriers = carriers
	c.templates = templates
	c.headers = headers
	c.numRules = numRules
	c.mu.Unlock()
	return nil
}

func loadRoutes(ctx context.Context, pool *pgxpool.Pool) (map[string][]RouteEntry, error) {
	rows, err := pool.Query(ctx, `
		SELECT routing_group_id::text, route_entry_id::text, prefix,
			primary_carrier_id::text, failover1_carrier_id::text, failover2_carrier_id::text,
			distribution_mode, primary_weight, failover1_weight, failover2_weight
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
		if err := rows.Scan(&gid, &e.RouteEntryID, &e.Prefix, &e.PrimaryCarrierID, &e.Failover1CarrierID, &e.Failover2CarrierID,
			&e.DistributionMode, &e.PrimaryWeight, &e.Failover1Weight, &e.Failover2Weight); err != nil {
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

// Template returns the cached request template for a carrier. ok is false when the cache has no entry
// (cache not warmed, or the carrier simply has no template row), so the caller falls back to a DB read
// that yields the same answer.
func (c *Cache) Template(carrierID string) (*db.RequestTemplate, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.templates == nil {
		return nil, false
	}
	t, ok := c.templates[carrierID]
	return t, ok
}

// AuthHeaders returns the cached, decrypted auth headers for a carrier. ok is false when header caching
// is not active (no secret key set or the bulk load failed), so the caller falls back to a per-message
// DB read. When caching is active a carrier with no headers correctly returns (nil, true) - no DB read.
func (c *Cache) AuthHeaders(carrierID string) ([]db.AuthHeaderRow, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.headers == nil {
		return nil, false
	}
	return c.headers[carrierID], true
}

// NumberRules returns the compiled A/B number-translation rules for a carrier. ok is false until the
// cache is warmed (caller falls back to a DB read); when warmed, a carrier with no rules returns
// (nil, true), and a nil *Compiled is a safe pass-through.
func (c *Cache) NumberRules(carrierID string) (*numrules.Compiled, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.numRules == nil {
		return nil, false
	}
	return c.numRules[carrierID], true
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
