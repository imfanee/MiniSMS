// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package web

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/minisms/minisms/internal/db"
	"golang.org/x/sync/errgroup"
)

// Health thresholds for the dashboard NOC panel. healthMinSample avoids flagging a carrier or client
// on a handful of messages; healthSuccessFloor is the 1h acceptance-success percentage below which we
// warn. "Acceptance success" is ok/(ok+bad) where ok = accepted|sent|delivered and bad = failed|rejected;
// it is available without waiting on a final DLR, so it reflects live interconnect health.
const (
	healthMinSample    = 20
	healthSuccessFloor = 90.0
)

// HealthState is the traffic-light state of one entity or the system.
type HealthState string

const (
	HealthOK   HealthState = "ok"
	HealthWarn HealthState = "warn"
	HealthDown HealthState = "down"
	HealthOff  HealthState = "off" // administratively inactive/disabled/suspended (not an alert)
)

// Class maps a state to a Bootstrap contextual suffix (text-bg-*, bg-*, border-*).
func (s HealthState) Class() string {
	switch s {
	case HealthDown:
		return "danger"
	case HealthWarn:
		return "warning"
	case HealthOff:
		return "secondary"
	default:
		return "success"
	}
}

// Label is the short uppercase state label shown on the dot/badge.
func (s HealthState) Label() string {
	switch s {
	case HealthDown:
		return "DOWN"
	case HealthWarn:
		return "WARN"
	case HealthOff:
		return "OFF"
	default:
		return "UP"
	}
}

// IsAlert reports whether a state should count toward the Alerts tile (operational trouble only, so an
// administratively OFF entity does not raise an alert).
func (s HealthState) IsAlert() bool { return s == HealthWarn || s == HealthDown }

// SystemHealth is the top status strip: live send rate, 1h success, SMPP interconnect totals, queue.
type SystemHealth struct {
	SMS1m, SMS5m, SMS1h int64
	Failed1h            int64
	SuccessPct1h        float64
	QueueEnabled        bool
	Queued, Sending     int64
	OldestQueuedAgeS    int64
	SMPPIngressEnabled  bool
	ClientBinds         int
	HasSMPPCarrier      bool
	EgressReady         int
	EgressTotal         int
	Alerts              int
	GeneratedAt         time.Time
}

// OldestQueuedStr is a humanized age of the oldest still-queued message ("-" when none).
func (s SystemHealth) OldestQueuedStr() string { return humanDurationS(s.OldestQueuedAgeS) }

// EgressState is the interconnect light for the aggregate SMPP egress binds.
func (s SystemHealth) EgressState() HealthState {
	if s.EgressTotal == 0 {
		return HealthOK
	}
	if s.EgressReady == 0 {
		return HealthDown
	}
	if s.EgressReady < s.EgressTotal {
		return HealthWarn
	}
	return HealthOK
}

// CarrierHealth is one carrier box on the health dashboard (live metrics + identifying config).
type CarrierHealth struct {
	CarrierID     string
	Name          string
	Status        string
	Transport     string // "http" | "smpp"
	IsSMPP        bool
	EndpointURL   string // HTTP endpoint (for HTTP carriers)
	HTTPMethod    string
	RateGroup     string
	Currency      string
	Balance       string
	BalanceLow    bool
	BindsKnown    bool
	BindsReady    int
	BindsTotal    int
	UnmatchedDLR  int64
	LastMessageAt *time.Time
	TotalMessages int64
	TotalAmount   string
	Sent1h        int64 // carrier attempts in last hour
	OK1h          int64
	Bad1h         int64
	SuccessPct1h  float64
	State         HealthState
	Reasons       []string
}

// EndpointHost is the host[:port] of a HTTP carrier endpoint, for compact display ("-" on parse fail).
func (c CarrierHealth) EndpointHost() string {
	s := strings.TrimSpace(c.EndpointURL)
	if s == "" {
		return "-"
	}
	if u, err := url.Parse(s); err == nil && u.Host != "" {
		return u.Host
	}
	return s
}

func (c CarrierHealth) LastAge() string    { return humanAge(c.LastMessageAt) }
func (c CarrierHealth) ReasonsStr() string { return strings.Join(c.Reasons, "; ") }

// ClientHealth is one client box on the health dashboard (live metrics + identifying config).
type ClientHealth struct {
	ClientID      string
	Name          string
	Status        string
	Email         string
	RateGroup     string
	RoutingGroup  string
	APIKeyPrefix  string
	DLRWebhookSet bool
	Currency      string
	Balance       string
	BalanceLow    bool
	Binds         int // live SMPP ingress binds
	Sent1h        int64
	OK1h          int64
	Bad1h         int64
	ViaSMPP1h     int64
	ViaHTTP1h     int64
	SuccessPct1h  float64
	LastMessageAt *time.Time
	State         HealthState
	Reasons       []string
}

func (c ClientHealth) LastAge() string    { return humanAge(c.LastMessageAt) }
func (c ClientHealth) ReasonsStr() string { return strings.Join(c.Reasons, "; ") }

// DashboardHealthData backs the dashboard_health fragment.
type DashboardHealthData struct {
	System   SystemHealth
	Carriers []CarrierHealth
	Clients  []ClientHealth
}

// healthPct is acceptance success: ok/(ok+bad) as a percentage; 100 when there is no traffic (nothing
// is failing).
func healthPct(ok, bad int64) float64 {
	d := ok + bad
	if d == 0 {
		return 100
	}
	return float64(ok) / float64(d) * 100
}

// balanceBelow reports whether a numeric-text balance is under threshold (false on parse error).
func balanceBelow(balance string, threshold float64) bool {
	v, err := strconv.ParseFloat(strings.TrimSpace(balance), 64)
	if err != nil {
		return false
	}
	return v < threshold
}

// carrierState derives the traffic-light state and human reasons for one carrier. Down conditions are
// hard (disabled, or an SMPP carrier with no bind up); the rest are warnings.
func carrierState(c CarrierHealth) (HealthState, []string) {
	var reasons []string
	if !strings.EqualFold(c.Status, "active") {
		return HealthOff, []string{"carrier " + strings.ToLower(c.Status)}
	}
	if c.IsSMPP && c.BindsKnown && c.BindsReady == 0 {
		return HealthDown, []string{"no SMPP binds up"}
	}
	state := HealthOK
	if c.IsSMPP && c.BindsKnown && c.BindsReady < c.BindsTotal {
		reasons = append(reasons, "partial binds "+strconv.Itoa(c.BindsReady)+"/"+strconv.Itoa(c.BindsTotal))
		state = HealthWarn
	}
	if c.BalanceLow {
		reasons = append(reasons, "low balance")
		state = HealthWarn
	}
	if c.UnmatchedDLR > 0 {
		reasons = append(reasons, strconv.FormatInt(c.UnmatchedDLR, 10)+" unmatched DLRs")
		state = HealthWarn
	}
	if c.Sent1h >= healthMinSample && c.SuccessPct1h < healthSuccessFloor {
		reasons = append(reasons, "1h success "+strconv.FormatFloat(c.SuccessPct1h, 'f', 0, 64)+"%")
		state = HealthWarn
	}
	return state, reasons
}

// clientState derives the traffic-light state and reasons for one client.
func clientState(c ClientHealth) (HealthState, []string) {
	if !strings.EqualFold(c.Status, "active") {
		return HealthOff, []string{"client " + strings.ToLower(c.Status)}
	}
	var reasons []string
	state := HealthOK
	if c.BalanceLow {
		reasons = append(reasons, "low balance")
		state = HealthWarn
	}
	if c.Sent1h >= healthMinSample && c.SuccessPct1h < healthSuccessFloor {
		reasons = append(reasons, "1h success "+strconv.FormatFloat(c.SuccessPct1h, 'f', 0, 64)+"%")
		state = HealthWarn
	}
	return state, reasons
}

func humanAge(t *time.Time) string {
	if t == nil {
		return "-"
	}
	return humanDurationS(int64(time.Since(*t).Seconds()))
}

func humanDurationS(secs int64) string {
	if secs <= 0 {
		return "-"
	}
	switch {
	case secs < 60:
		return strconv.FormatInt(secs, 10) + "s"
	case secs < 3600:
		return strconv.FormatInt(secs/60, 10) + "m"
	case secs < 86400:
		return strconv.FormatInt(secs/3600, 10) + "h"
	default:
		return strconv.FormatInt(secs/86400, 10) + "d"
	}
}

type carrierMetric struct{ attempts, ok, bad int64 }
type clientMetric struct{ sent, ok, bad, viaSMPP, viaHTTP int64 }
type carrierTotal struct {
	last          *time.Time
	totalMessages int64
	totalAmount   string
}

// DashboardHealthFragment renders the auto-refreshing NOC health panel (system strip + per-carrier and
// per-client interconnect health). Behind the same RBAC as the dashboard stats fragment.
func (h *Handlers) DashboardHealthFragment() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data, err := h.collectHealth(r.Context())
		if err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		if err := execT(w, h.DashFragT, "dashboard_health", data); err != nil {
			ServerError(w, r, err, h.Log, h.T500)
		}
	}
}

// collectHealth gathers all health signals concurrently (mirrors collectDashboard). It is read-only:
// carrier/client static rows + last activity + 1h sms_logs metrics from the DB, and live bind counts
// from the in-memory SMPP controllers.
func (h *Handlers) collectHealth(ctx context.Context) (*DashboardHealthData, error) {
	var carrierThreshS, clientThreshS string
	if err := h.Pool.QueryRow(ctx, `
		SELECT
			COALESCE((SELECT value FROM system_settings WHERE key='carrier_low_balance_alert'), '10'),
			COALESCE((SELECT value FROM system_settings WHERE key='low_balance_alert_threshold'), '1')`).
		Scan(&carrierThreshS, &clientThreshS); err != nil {
		carrierThreshS, clientThreshS = "10", "1"
	}
	carrierThresh, _ := strconv.ParseFloat(strings.TrimSpace(carrierThreshS), 64)
	clientThresh, _ := strconv.ParseFloat(strings.TrimSpace(clientThreshS), 64)

	var (
		mu             sync.Mutex
		sys            SystemHealth
		carriers       []db.CarrierRow
		clients        []db.ClientListRow
		carrierTotals  = map[string]carrierTotal{}
		clientLast     = map[string]*time.Time{}
		carrierMetrics = map[string]carrierMetric{}
		clientMetrics  = map[string]clientMetric{}
	)

	g, ctx := errgroup.WithContext(ctx)

	g.Go(func() error {
		var m1, m5, h1, ok1h, bad1h int64
		if err := h.Pool.QueryRow(ctx, `
			SELECT
				COUNT(*) FILTER (WHERE received_at >= now()-'1 minute'::interval),
				COUNT(*) FILTER (WHERE received_at >= now()-'5 minutes'::interval),
				COUNT(*),
				COUNT(*) FILTER (WHERE status IN ('accepted','sent','delivered')),
				COUNT(*) FILTER (WHERE status IN ('failed','rejected'))
			FROM sms_logs WHERE received_at >= now()-'1 hour'::interval`).Scan(&m1, &m5, &h1, &ok1h, &bad1h); err != nil {
			return err
		}
		mu.Lock()
		sys.SMS1m, sys.SMS5m, sys.SMS1h = m1, m5, h1
		sys.Failed1h = bad1h
		sys.SuccessPct1h = healthPct(ok1h, bad1h)
		mu.Unlock()
		return nil
	})

	if h.Config.SendQueueEnabled {
		g.Go(func() error {
			var queued, sending int64
			var oldest *time.Time
			if err := h.Pool.QueryRow(ctx, `
				SELECT
					COUNT(*) FILTER (WHERE status='queued'),
					COUNT(*) FILTER (WHERE status='sending'),
					MIN(received_at) FILTER (WHERE status='queued')
				FROM sms_logs WHERE status IN ('queued','sending')`).Scan(&queued, &sending, &oldest); err != nil {
				return err
			}
			mu.Lock()
			sys.Queued, sys.Sending = queued, sending
			if oldest != nil {
				sys.OldestQueuedAgeS = int64(time.Since(*oldest).Seconds())
			}
			mu.Unlock()
			return nil
		})
	}

	g.Go(func() error {
		cs, err := db.ListCarriers(ctx, h.Pool)
		if err != nil {
			return err
		}
		mu.Lock()
		carriers = cs
		mu.Unlock()
		return nil
	})

	g.Go(func() error {
		rows, err := h.Pool.Query(ctx, `SELECT carrier_id::text, last_message_at, total_messages, total_amount::text FROM carrier_usage_totals`)
		if err != nil {
			return err
		}
		defer rows.Close()
		m := map[string]carrierTotal{}
		for rows.Next() {
			var id string
			var v carrierTotal
			if err := rows.Scan(&id, &v.last, &v.totalMessages, &v.totalAmount); err != nil {
				return err
			}
			m[id] = v
		}
		mu.Lock()
		carrierTotals = m
		mu.Unlock()
		return rows.Err()
	})

	g.Go(func() error {
		rows, err := h.Pool.Query(ctx, `
			SELECT carrier_id::text,
				COUNT(*) FILTER (WHERE status NOT IN ('pending','queued','sending')),
				COUNT(*) FILTER (WHERE status IN ('accepted','sent','delivered')),
				COUNT(*) FILTER (WHERE status IN ('failed','rejected'))
			FROM sms_logs
			WHERE received_at >= now()-'1 hour'::interval AND carrier_id IS NOT NULL
			GROUP BY carrier_id`)
		if err != nil {
			return err
		}
		defer rows.Close()
		m := map[string]carrierMetric{}
		for rows.Next() {
			var id string
			var v carrierMetric
			if err := rows.Scan(&id, &v.attempts, &v.ok, &v.bad); err != nil {
				return err
			}
			m[id] = v
		}
		mu.Lock()
		carrierMetrics = m
		mu.Unlock()
		return rows.Err()
	})

	g.Go(func() error {
		cs, err := db.ListClients(ctx, h.Pool)
		if err != nil {
			return err
		}
		mu.Lock()
		clients = cs
		mu.Unlock()
		return nil
	})

	g.Go(func() error {
		rows, err := h.Pool.Query(ctx, `
			SELECT client_id::text,
				COUNT(*),
				COUNT(*) FILTER (WHERE status IN ('accepted','sent','delivered')),
				COUNT(*) FILTER (WHERE status IN ('failed','rejected')),
				COUNT(*) FILTER (WHERE ingress_transport='smpp'),
				COUNT(*) FILTER (WHERE ingress_transport='http')
			FROM sms_logs
			WHERE received_at >= now()-'1 hour'::interval
			GROUP BY client_id`)
		if err != nil {
			return err
		}
		defer rows.Close()
		m := map[string]clientMetric{}
		for rows.Next() {
			var id string
			var v clientMetric
			if err := rows.Scan(&id, &v.sent, &v.ok, &v.bad, &v.viaSMPP, &v.viaHTTP); err != nil {
				return err
			}
			m[id] = v
		}
		mu.Lock()
		clientMetrics = m
		mu.Unlock()
		return rows.Err()
	})

	g.Go(func() error {
		rows, err := h.Pool.Query(ctx, `SELECT client_id::text, last_message_at FROM v_client_sms_summary`)
		if err != nil {
			return err
		}
		defer rows.Close()
		m := map[string]*time.Time{}
		for rows.Next() {
			var id string
			var t *time.Time
			if err := rows.Scan(&id, &t); err != nil {
				return err
			}
			m[id] = t
		}
		mu.Lock()
		clientLast = m
		mu.Unlock()
		return rows.Err()
	})

	if err := g.Wait(); err != nil {
		return nil, err
	}

	alerts := 0
	carrierRows := make([]CarrierHealth, 0, len(carriers))
	for _, c := range carriers {
		ch := CarrierHealth{
			CarrierID: c.CarrierID, Name: c.Name, Status: c.Status,
			Transport: c.EgressTransport, Currency: c.Currency, Balance: c.Balance,
			EndpointURL: c.EndpointURL, HTTPMethod: c.HTTPMethod, RateGroup: derefOrDash(c.RateGroupName),
		}
		ch.IsSMPP = strings.EqualFold(strings.TrimSpace(c.EgressTransport), "smpp")
		ch.BalanceLow = balanceBelow(c.Balance, carrierThresh)
		if t, ok := carrierTotals[c.CarrierID]; ok {
			ch.LastMessageAt = t.last
			ch.TotalMessages = t.totalMessages
			ch.TotalAmount = t.totalAmount
		}
		if m, ok := carrierMetrics[c.CarrierID]; ok {
			ch.Sent1h, ch.OK1h, ch.Bad1h = m.attempts, m.ok, m.bad
		}
		ch.SuccessPct1h = healthPct(ch.OK1h, ch.Bad1h)
		if ch.IsSMPP {
			sys.HasSMPPCarrier = true
			if h.SMPPCtl != nil {
				if ready, total, present := h.SMPPCtl.BindStatus(c.CarrierID); present {
					ch.BindsKnown = true
					ch.BindsReady, ch.BindsTotal = ready, total
					sys.EgressReady += ready
					sys.EgressTotal += total
				}
				ch.UnmatchedDLR = h.SMPPCtl.UnmatchedDLRs(c.CarrierID)
			}
		}
		ch.State, ch.Reasons = carrierState(ch)
		if ch.State.IsAlert() {
			alerts++
		}
		carrierRows = append(carrierRows, ch)
	}

	clientRows := make([]ClientHealth, 0, len(clients))
	for _, c := range clients {
		cl := ClientHealth{
			ClientID: c.ClientID, Name: c.Name, Status: c.Status,
			Currency: c.Currency, Balance: c.Balance,
			Email: c.Email, RateGroup: derefOrDash(c.RateGroupName), RoutingGroup: derefOrDash(c.RoutingGroupName),
			APIKeyPrefix: derefOrDash(c.APIKeyPrefix), DLRWebhookSet: c.DLRWebhookURL != nil && strings.TrimSpace(*c.DLRWebhookURL) != "",
		}
		cl.BalanceLow = balanceBelow(c.Balance, clientThresh)
		cl.LastMessageAt = clientLast[c.ClientID]
		if m, ok := clientMetrics[c.ClientID]; ok {
			cl.Sent1h, cl.OK1h, cl.Bad1h = m.sent, m.ok, m.bad
			cl.ViaSMPP1h, cl.ViaHTTP1h = m.viaSMPP, m.viaHTTP
		}
		cl.SuccessPct1h = healthPct(cl.OK1h, cl.Bad1h)
		if h.SMPPIngress != nil {
			cl.Binds = h.SMPPIngress.BindCount(c.ClientID)
			sys.ClientBinds += cl.Binds
		}
		cl.State, cl.Reasons = clientState(cl)
		if cl.State.IsAlert() {
			alerts++
		}
		clientRows = append(clientRows, cl)
	}

	sys.QueueEnabled = h.Config.SendQueueEnabled
	sys.SMPPIngressEnabled = h.Config.SMPPServerEnabled
	sys.Alerts = alerts
	sys.GeneratedAt = time.Now().UTC()

	return &DashboardHealthData{System: sys, Carriers: carrierRows, Clients: clientRows}, nil
}
