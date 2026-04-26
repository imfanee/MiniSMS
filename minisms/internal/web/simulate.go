package web

import (
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/gorilla/csrf"
	"github.com/jackc/pgx/v5"

	"github.com/minisms/minisms/internal/billing"
	"github.com/minisms/minisms/internal/carrier"
	"github.com/minisms/minisms/internal/db"
)

var simulateE164Re = regexp.MustCompile(`^\+[1-9]\d{6,14}$`)

type simulatePage struct {
	Title       string
	CurrentPath string
	CSRFToken   string
	Flash       *Flash
	Clients     []db.ClientListRow
	Form        simulateForm
	Result      *simulateResult
}

type simulateForm struct {
	ClientID    string
	Destination string
	SenderID    string
	Errors      map[string]string
}

type simulateResult struct {
	SenderCheck      string
	ResolvedSenderID string
	SelectedRate     string
	SelectedPrefix   string
	SelectedRoute    string
	Carrier          string
	CarrierRate      string
	FinalDecision    string
	CarrierChecks    []simulateCarrierCheck
}

type simulateCarrierCheck struct {
	Name     string
	Position string
	Eligible bool
	Reason   string
}

type simulateRouteEntry struct {
	RouteEntryID       string
	Prefix             string
	PrimaryCarrierID   string
	Failover1CarrierID *string
	Failover2CarrierID *string
}

func (h *Handlers) ShowSimulate() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		clients, err := db.ListClients(r.Context(), h.Pool)
		if err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		p := simulatePage{
			Title:       "Simulate",
			CurrentPath: "/admin/simulate",
			CSRFToken:   csrf.Token(r),
			Flash:       GetFlash(w, r, "/", h.Config.SecretKey, h.Config.IsProduction()),
			Clients:     clients,
			Form:        simulateForm{Errors: map[string]string{}},
		}
		if err := execT(w, h.SimulateT, "base", p); err != nil {
			ServerError(w, r, err, h.Log, h.T500)
		}
	}
}

func (h *Handlers) RunSimulation() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		clients, err := db.ListClients(r.Context(), h.Pool)
		if err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		form := simulateForm{
			ClientID:    strings.TrimSpace(r.FormValue("client_id")),
			Destination: strings.TrimSpace(r.FormValue("destination")),
			SenderID:    strings.TrimSpace(r.FormValue("sender_id")),
			Errors:      map[string]string{},
		}
		if form.ClientID == "" {
			form.Errors["client_id"] = "Client is required"
		}
		if !simulateE164Re.MatchString(form.Destination) {
			form.Errors["destination"] = "Destination must be E.164 format (example: +447700900123)"
		}
		if len(form.Errors) > 0 {
			h.renderSimulatePage(w, r, clients, form, nil)
			return
		}

		cl, err := db.GetClient(r.Context(), h.Pool, form.ClientID)
		if err != nil {
			if err == pgx.ErrNoRows {
				form.Errors["client_id"] = "Client not found"
				h.renderSimulatePage(w, r, clients, form, nil)
				return
			}
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		systemDefaultSenderID := h.simulationSetting(r, "default_sender_id", "MiniSMS")
		sidResolution, err := carrier.ResolveSenderID(r.Context(), h.Pool, cl, form.SenderID, systemDefaultSenderID)
		if err != nil {
			h.renderSimulatePage(w, r, clients, form, &simulateResult{
				SenderCheck:   "Failed: sender ID is not allowed for this client",
				FinalDecision: "Message will not be routed",
			})
			return
		}
		if cl.RateGroupID == nil || *cl.RateGroupID == "" {
			h.renderSimulatePage(w, r, clients, form, &simulateResult{
				SenderCheck:      "Passed",
				ResolvedSenderID: sidResolution.Value,
				FinalDecision:    "Message will not be routed: client has no rate group",
			})
			return
		}
		rateEntry, err := billing.LookupRate(r.Context(), h.Pool, *cl.RateGroupID, form.Destination)
		if err != nil {
			h.renderSimulatePage(w, r, clients, form, &simulateResult{
				SenderCheck:      "Passed",
				ResolvedSenderID: sidResolution.Value,
				FinalDecision:    "Message will not be routed: no matching client rate",
			})
			return
		}
		routeEntry, err := h.simulationLookupRouteEntry(r, cl, form.Destination)
		if err != nil || routeEntry == nil {
			h.renderSimulatePage(w, r, clients, form, &simulateResult{
				SenderCheck:      "Passed",
				ResolvedSenderID: sidResolution.Value,
				SelectedRate:     rateEntry.RatePerSMS,
				SelectedPrefix:   rateEntry.Prefix,
				FinalDecision:    "Message will not be routed: no matching route",
			})
			return
		}
		res := &simulateResult{
			SenderCheck:      "Passed",
			ResolvedSenderID: sidResolution.Value,
			SelectedRate:     rateEntry.RatePerSMS,
			SelectedPrefix:   rateEntry.Prefix,
			SelectedRoute:    routeEntry.Prefix,
		}
		orderedCarriers := []struct {
			id       string
			position string
		}{
			{id: routeEntry.PrimaryCarrierID, position: "Primary"},
		}
		if routeEntry.Failover1CarrierID != nil && *routeEntry.Failover1CarrierID != "" {
			orderedCarriers = append(orderedCarriers, struct {
				id       string
				position string
			}{id: *routeEntry.Failover1CarrierID, position: "Failover 1"})
		}
		if routeEntry.Failover2CarrierID != nil && *routeEntry.Failover2CarrierID != "" {
			orderedCarriers = append(orderedCarriers, struct {
				id       string
				position string
			}{id: *routeEntry.Failover2CarrierID, position: "Failover 2"})
		}

		var selectedCarrierID, selectedCarrierName, selectedSender string
		for _, c := range orderedCarriers {
			var carrierName, status, senderIDPolicy string
			var defaultSenderIDValue, carrierRateGroupID *string
			if err := h.Pool.QueryRow(r.Context(), `
				SELECT name, status, sender_id_policy, default_sender_id_value, rate_group_id::text
				FROM carriers
				WHERE carrier_id = $1::uuid`, c.id).Scan(&carrierName, &status, &senderIDPolicy, &defaultSenderIDValue, &carrierRateGroupID); err != nil {
				res.CarrierChecks = append(res.CarrierChecks, simulateCarrierCheck{Name: c.id, Position: c.position, Eligible: false, Reason: "carrier not found"})
				continue
			}
			if status != "active" {
				res.CarrierChecks = append(res.CarrierChecks, simulateCarrierCheck{Name: carrierName, Position: c.position, Eligible: false, Reason: "inactive"})
				continue
			}
			effectiveSenderID, eligible, reason, err := carrier.CheckCarrierEligibility(
				r.Context(),
				h.Pool,
				c.id,
				carrierName,
				senderIDPolicy,
				defaultSenderIDValue,
				carrierRateGroupID,
				sidResolution,
				rateEntry.RatePerSMS,
				cl.ClientID,
				simulationNormalizeDestination(form.Destination),
			)
			if err != nil {
				res.CarrierChecks = append(res.CarrierChecks, simulateCarrierCheck{Name: carrierName, Position: c.position, Eligible: false, Reason: err.Error()})
				continue
			}
			if !eligible {
				res.CarrierChecks = append(res.CarrierChecks, simulateCarrierCheck{Name: carrierName, Position: c.position, Eligible: false, Reason: reason})
				continue
			}
			res.CarrierChecks = append(res.CarrierChecks, simulateCarrierCheck{Name: carrierName, Position: c.position, Eligible: true, Reason: "eligible"})
			if selectedCarrierID == "" {
				selectedCarrierID = c.id
				selectedCarrierName = carrierName
				selectedSender = effectiveSenderID
			}
		}

		if selectedCarrierID == "" {
			res.FinalDecision = "Message will not be routed: no eligible carrier in primary/failover chain"
			h.renderSimulatePage(w, r, clients, form, res)
			return
		}
		carrierRate, err := billing.LookupCarrierCost(r.Context(), h.Pool, selectedCarrierID, form.Destination, rateEntry.RatePerSMS)
		if err != nil {
			res.FinalDecision = "Message route selected but carrier rate lookup failed"
			h.renderSimulatePage(w, r, clients, form, res)
			return
		}
		res.Carrier = selectedCarrierName
		res.CarrierRate = carrierRate
		if selectedSender != "" {
			res.ResolvedSenderID = selectedSender
		}
		res.FinalDecision = "Message will be routed (simulation only). No SMS was sent and no SMS log was created."
		h.renderSimulatePage(w, r, clients, form, res)
	}
}

func (h *Handlers) renderSimulatePage(w http.ResponseWriter, r *http.Request, clients []db.ClientListRow, form simulateForm, result *simulateResult) {
	p := simulatePage{
		Title:       "Simulate",
		CurrentPath: "/admin/simulate",
		CSRFToken:   csrf.Token(r),
		Flash:       GetFlash(w, r, "/", h.Config.SecretKey, h.Config.IsProduction()),
		Clients:     clients,
		Form:        form,
		Result:      result,
	}
	if isHTMX(r) && r.Header.Get("HX-Target") == "simulate-result" {
		if err := execT(w, h.SimulateT, "simulate_result", p); err != nil {
			ServerError(w, r, err, h.Log, h.T500)
		}
		return
	}
	if err := execT(w, h.SimulateT, "base", p); err != nil {
		ServerError(w, r, err, h.Log, h.T500)
	}
}

func (h *Handlers) simulationLookupRouteEntry(r *http.Request, client *db.Client, to string) (*simulateRouteEntry, error) {
	if client.RoutingGroupID == nil || *client.RoutingGroupID == "" {
		return nil, pgx.ErrNoRows
	}
	rows, err := h.Pool.Query(r.Context(), `
		SELECT route_entry_id::text, prefix, primary_carrier_id::text, failover1_carrier_id::text, failover2_carrier_id::text
		FROM route_entries
		WHERE routing_group_id = $1::uuid AND status='active'`, *client.RoutingGroupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var entries []simulateRouteEntry
	for rows.Next() {
		var e simulateRouteEntry
		if err := rows.Scan(&e.RouteEntryID, &e.Prefix, &e.PrimaryCarrierID, &e.Failover1CarrierID, &e.Failover2CarrierID); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	best := simulationLongestPrefixRoute(entries, to)
	if best == nil {
		return nil, pgx.ErrNoRows
	}
	return best, nil
}

func simulationLongestPrefixRoute(entries []simulateRouteEntry, destination string) *simulateRouteEntry {
	dst := simulationNormalizeDestination(destination)
	var catchAll *simulateRouteEntry
	var best *simulateRouteEntry
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

func simulationNormalizeDestination(destination string) string {
	d := strings.TrimPrefix(strings.TrimSpace(destination), "+")
	var b strings.Builder
	for _, r := range d {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func (h *Handlers) simulationSetting(r *http.Request, key, def string) string {
	var v string
	if err := h.Pool.QueryRow(r.Context(), `SELECT value FROM system_settings WHERE key = $1`, key).Scan(&v); err != nil {
		return def
	}
	return v
}

func simulationFailoverNumber(position string) int {
	switch position {
	case "Primary":
		return 0
	case "Failover 1":
		return 1
	case "Failover 2":
		return 2
	default:
		return 0
	}
}

func simulationFailoverLabel(position string) string {
	return "sequence " + strconv.Itoa(simulationFailoverNumber(position))
}
