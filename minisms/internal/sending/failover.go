// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package sending

import (
	"context"
	"strings"
	"sync"
)

type failoverCarrier struct {
	id string
	n  int
}

type weightedCarrier struct {
	id string
	n  int
	w  int
}

// routeRR holds per-route round-robin counters for weighted load balancing.
type routeRR struct {
	mu sync.Mutex
	c  map[string]uint64
}

func newRouteRR() *routeRR { return &routeRR{c: make(map[string]uint64)} }

func (r *routeRR) next(key string) uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.c[key]++
	return r.c[key]
}

// orderCarriers returns the ordered list of carriers to attempt for one message.
// The first entry is the preferred carrier for this message; the rest are its
// failover chain (failover is always available regardless of distribution mode).
//
//	failover    - fixed slot order (primary, failover1, failover2). Unchanged.
//	loadbalance - weighted round-robin picks the first carrier (parallel fan-out);
//	              the remaining carriers stay as this message's failover.
//	overflow    - prefer higher-priority carriers that are currently ready; spill
//	              to the next ready carrier when a higher one is unavailable.
func (s *Service) orderCarriers(ctx context.Context, route *RouteEntry, failoverEnabled bool) []failoverCarrier {
	if !failoverEnabled {
		return []failoverCarrier{{id: route.PrimaryCarrierID, n: 0}}
	}
	cands := []weightedCarrier{{id: route.PrimaryCarrierID, n: 0, w: route.PrimaryWeight}}
	if route.Failover1CarrierID != nil && *route.Failover1CarrierID != "" {
		cands = append(cands, weightedCarrier{id: *route.Failover1CarrierID, n: 1, w: route.Failover1Weight})
	}
	if route.Failover2CarrierID != nil && *route.Failover2CarrierID != "" {
		cands = append(cands, weightedCarrier{id: *route.Failover2CarrierID, n: 2, w: route.Failover2Weight})
	}
	if len(cands) == 1 {
		return slotOrder(cands)
	}
	switch strings.ToLower(strings.TrimSpace(route.DistributionMode)) {
	case "loadbalance":
		return s.orderLoadbalance(route.RouteEntryID, cands)
	case "overflow":
		return s.orderOverflow(ctx, cands)
	default: // failover
		return slotOrder(cands)
	}
}

func slotOrder(cands []weightedCarrier) []failoverCarrier {
	out := make([]failoverCarrier, len(cands))
	for i, c := range cands {
		out[i] = failoverCarrier{id: c.id, n: c.n}
	}
	return out
}

// orderLoadbalance selects the first carrier by weighted round-robin (share
// proportional to each carrier's weight) and appends the rest as failover.
func (s *Service) orderLoadbalance(routeID string, cands []weightedCarrier) []failoverCarrier {
	total := 0
	for _, c := range cands {
		if c.w > 0 {
			total += c.w
		}
	}
	if total == 0 || s.rr == nil {
		return slotOrder(cands)
	}
	pos := int(s.rr.next(routeID) % uint64(total))
	pick := -1
	for i, c := range cands {
		if c.w <= 0 {
			continue
		}
		if pos < c.w {
			pick = i
			break
		}
		pos -= c.w
	}
	if pick < 0 {
		return slotOrder(cands)
	}
	out := []failoverCarrier{{id: cands[pick].id, n: cands[pick].n}}
	for i, c := range cands {
		if i == pick {
			continue
		}
		out = append(out, failoverCarrier{id: c.id, n: c.n})
	}
	return out
}

// orderOverflow puts currently-ready carriers first (keeping slot priority among
// them) so traffic spills to the next ready carrier when a higher one is down.
func (s *Service) orderOverflow(ctx context.Context, cands []weightedCarrier) []failoverCarrier {
	var ready, down []failoverCarrier
	for _, c := range cands {
		if s.carrierReady(ctx, c.id) {
			ready = append(ready, failoverCarrier{id: c.id, n: c.n})
		} else {
			down = append(down, failoverCarrier{id: c.id, n: c.n})
		}
	}
	return append(ready, down...)
}

// carrierReady reports whether a carrier can currently accept traffic: an SMPP
// egress carrier needs a bound session; an HTTP carrier is always considered
// ready (readiness is checked per request by the dispatcher).
func (s *Service) carrierReady(ctx context.Context, carrierID string) bool {
	prof, ok := s.carrierProfile(ctx, carrierID)
	if !ok {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(prof.EgressTransport), "smpp") {
		return s.Egress != nil && s.Egress.Ready(carrierID)
	}
	return true
}
