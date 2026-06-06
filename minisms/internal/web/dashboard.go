// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package web

import (
	"encoding/json"
	"errors"
	"html/template"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/gorilla/csrf"
	"github.com/minisms/minisms"
	"github.com/minisms/minisms/internal/db"
	"golang.org/x/sync/errgroup"
)

type DashboardStats struct {
	ActiveClients      int64
	ActiveCarriers     int64
	SMSToday           int64
	TotalClientFunds   string
	LowBalanceCarriers int64
}

type CarrierHealthRow struct {
	CarrierName       string
	Status            string
	CurrentBalance    string
	Currency          string
	LastMessageAt     *time.Time
	SpendLast30d      string
	TotalMessagesSent int64
	SuccessRate       string
}

type FailoverCount struct {
	Primary int64
	F1      int64
	F2      int64
}

type RecentFailure struct {
	MessageID           string
	ReceivedAt          time.Time
	ToNumber            string
	ClientName          *string
	CarrierName         *string
	Status              string
	CarrierResponseCode *int
}

type ThroughputRow struct {
	Day         time.Time
	Total       int64
	ViaFailover int64
	ViaPrimary  int64
	FailoverPct float64
}

type DashboardPage struct {
	AdminView
	Title       string
	CurrentPath string
	CSRFToken   string
	Flash       *Flash
	Stats       DashboardStats
	Carrier     []CarrierHealthRow
	Failover    FailoverCount
	Failures    []RecentFailure
	Throughput  []ThroughputRow
}

func (h *Handlers) ShowDashboard() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		page, err := h.collectDashboard(r)
		if err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		page.Title = "Dashboard"
		page.CurrentPath = "/admin/dashboard"
		page.CSRFToken = csrf.Token(r)
		page.Flash = GetFlash(w, r, "/", h.Config.SecretKey, h.Config.IsProduction())
		if err := execT(w, h.DashT, "base", page, r); err != nil {
			ServerError(w, r, err, h.Log, h.T500)
		}
	}
}

func (h *Handlers) DashboardStatsFragment() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		page, err := h.collectDashboard(r)
		if err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		if err := execT(w, h.DashFragT, "dashboard_stats", page); err != nil {
			ServerError(w, r, err, h.Log, h.T500)
		}
	}
}

func (h *Handlers) collectDashboard(r *http.Request) (*DashboardPage, error) {
	var p DashboardPage
	var threshold string
	if err := h.Pool.QueryRow(r.Context(), `SELECT value FROM system_settings WHERE key='carrier_low_balance_alert'`).Scan(&threshold); err != nil {
		threshold = "10"
	}
	var mu sync.Mutex
	g, ctx := errgroup.WithContext(r.Context())
	g.Go(func() error {
		var s DashboardStats
		err := h.Pool.QueryRow(ctx, `
			SELECT
				(SELECT COUNT(*)::bigint FROM clients WHERE status='active'),
				(SELECT COUNT(*)::bigint FROM carriers WHERE status='active'),
				(SELECT COUNT(*)::bigint FROM sms_logs WHERE received_at >= CURRENT_DATE AND status IN ('accepted','sent','delivered')),
				COALESCE((SELECT SUM(balance)::text FROM clients WHERE status='active'),'0')`).Scan(&s.ActiveClients, &s.ActiveCarriers, &s.SMSToday, &s.TotalClientFunds)
		if err != nil {
			return err
		}
		_ = h.Pool.QueryRow(ctx, `SELECT COUNT(*)::bigint FROM carriers WHERE balance < $1::numeric(18,6)`, threshold).Scan(&s.LowBalanceCarriers)
		mu.Lock()
		p.Stats = s
		mu.Unlock()
		return nil
	})
	g.Go(func() error {
		rows, err := h.Pool.Query(ctx, `SELECT carrier_name, status, current_balance::text, currency::text, last_message_at, spend_last_30d::text, total_messages_sent,
			CASE WHEN total_messages_sent > 0 THEN '100.00' ELSE '0.00' END AS success_rate
			FROM v_carrier_financial_position ORDER BY carrier_name ASC`)
		if err != nil {
			return err
		}
		defer rows.Close()
		var out []CarrierHealthRow
		for rows.Next() {
			var x CarrierHealthRow
			if err := rows.Scan(&x.CarrierName, &x.Status, &x.CurrentBalance, &x.Currency, &x.LastMessageAt, &x.SpendLast30d, &x.TotalMessagesSent, &x.SuccessRate); err != nil {
				return err
			}
			out = append(out, x)
		}
		mu.Lock()
		p.Carrier = out
		mu.Unlock()
		return rows.Err()
	})
	g.Go(func() error {
		rows, err := h.Pool.Query(ctx, `SELECT failover_sequence, COUNT(*)::bigint
			FROM sms_logs
			WHERE received_at >= now()-'24 hours'::interval AND status NOT IN ('rejected','pending')
			GROUP BY failover_sequence`)
		if err != nil {
			return err
		}
		defer rows.Close()
		var f FailoverCount
		for rows.Next() {
			var seq int
			var cnt int64
			if err := rows.Scan(&seq, &cnt); err != nil {
				return err
			}
			switch seq {
			case 0:
				f.Primary = cnt
			case 1:
				f.F1 = cnt
			case 2:
				f.F2 = cnt
			}
		}
		mu.Lock()
		p.Failover = f
		mu.Unlock()
		return rows.Err()
	})
	g.Go(func() error {
		rows, err := h.Pool.Query(ctx, `SELECT sl.message_id::text, sl.received_at, sl.to_number, c.name, ca.name, sl.status, sl.carrier_response_code
			FROM sms_logs sl
			LEFT JOIN clients c ON c.client_id=sl.client_id
			LEFT JOIN carriers ca ON ca.carrier_id=sl.carrier_id
			WHERE sl.status IN ('failed','rejected')
			ORDER BY sl.received_at DESC LIMIT 10`)
		if err != nil {
			return err
		}
		defer rows.Close()
		var out []RecentFailure
		for rows.Next() {
			var x RecentFailure
			if err := rows.Scan(&x.MessageID, &x.ReceivedAt, &x.ToNumber, &x.ClientName, &x.CarrierName, &x.Status, &x.CarrierResponseCode); err != nil {
				return err
			}
			out = append(out, x)
		}
		mu.Lock()
		p.Failures = out
		mu.Unlock()
		return rows.Err()
	})
	g.Go(func() error {
		rows, err := h.Pool.Query(ctx, `SELECT DATE(received_at), COUNT(*)::bigint, COUNT(*) FILTER (WHERE failover_sequence>0)::bigint
			FROM sms_logs WHERE received_at >= now()-'7 days'::interval
			GROUP BY DATE(received_at) ORDER BY DATE(received_at)`)
		if err != nil {
			return err
		}
		defer rows.Close()
		var out []ThroughputRow
		for rows.Next() {
			var x ThroughputRow
			if err := rows.Scan(&x.Day, &x.Total, &x.ViaFailover); err != nil {
				return err
			}
			x.ViaPrimary = x.Total - x.ViaFailover
			if x.Total > 0 {
				x.FailoverPct = (float64(x.ViaFailover) / float64(x.Total)) * 100
			}
			out = append(out, x)
		}
		mu.Lock()
		p.Throughput = out
		mu.Unlock()
		return rows.Err()
	})
	if err := g.Wait(); err != nil {
		return nil, err
	}
	return &p, nil
}

type DashboardReportsPage struct {
	DefaultFrom     string
	DefaultTo       string
	Today           string
	Yesterday       string
	Last7From       string
	Last30From      string
	MonthFrom       string
	FromDate        string
	ToDate          string
	SMSByClient     []db.SMSByClientRow
	SMSByCarrier    []db.SMSByCarrierRow
	SuccessClients  []db.SMSByClientRow
	SuccessCarriers []db.SMSByCarrierRow
	CarrierPrefix   []db.CarrierPrefixRow
	BillComparison  []db.BillVsCostRow
	// BillByClientChart is per-client totals (billed vs carrier cost) for the grouped bar chart only.
	BillByClientChart []billByClientChartRow
	CostComparison    []db.CarrierCostRow
}

// billByClientChartRow matches grouped bill comparison chart labels/datasets.
type billByClientChartRow struct {
	ClientName   string
	ClientBilled float64
	CarrierCost  float64
}

func aggregateBillByClientChart(rows []db.BillVsCostRow) []billByClientChartRow {
	type agg struct{ billed, cost float64 }
	m := make(map[string]agg, len(rows))
	for _, row := range rows {
		a := m[row.ClientName]
		a.billed += row.ClientBilled
		a.cost += row.CarrierCost
		m[row.ClientName] = a
	}
	names := make([]string, 0, len(m))
	for k := range m {
		names = append(names, k)
	}
	sort.Strings(names)
	out := make([]billByClientChartRow, 0, len(names))
	for _, name := range names {
		a := m[name]
		out = append(out, billByClientChartRow{ClientName: name, ClientBilled: a.billed, CarrierCost: a.cost})
	}
	return out
}

func parseReportDateRange(r *http.Request) (time.Time, time.Time, string, string, error) {
	today := time.Now().UTC().Truncate(24 * time.Hour)
	fromStr := r.URL.Query().Get("from")
	toStr := r.URL.Query().Get("to")
	if fromStr == "" {
		fromStr = today.Format("2006-01-02")
	}
	if toStr == "" {
		toStr = today.Format("2006-01-02")
	}
	from, err := time.Parse("2006-01-02", fromStr)
	if err != nil {
		return time.Time{}, time.Time{}, "", "", err
	}
	to, err := time.Parse("2006-01-02", toStr)
	if err != nil {
		return time.Time{}, time.Time{}, "", "", err
	}
	if from.After(to) {
		return time.Time{}, time.Time{}, "", "", errors.New("from date must be before or equal to to date")
	}
	return from, to, fromStr, toStr, nil
}

func (h *Handlers) GetDashboardReports() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		from, to, fromStr, toStr, err := parseReportDateRange(r)
		if err != nil {
			http.Error(w, "invalid date range", http.StatusBadRequest)
			return
		}
		var page DashboardReportsPage
		var mu sync.Mutex
		g, ctx := errgroup.WithContext(r.Context())
		g.Go(func() error {
			rows, err := db.GetSMSByClientReport(ctx, h.Pool, from, to)
			if err != nil {
				return err
			}
			mu.Lock()
			page.SMSByClient = rows
			page.SuccessClients = rows
			mu.Unlock()
			return nil
		})
		g.Go(func() error {
			rows, err := db.GetSMSByCarrierReport(ctx, h.Pool, from, to)
			if err != nil {
				return err
			}
			mu.Lock()
			page.SMSByCarrier = rows
			page.SuccessCarriers = rows
			mu.Unlock()
			return nil
		})
		g.Go(func() error {
			rows, err := db.GetCarrierPrefixSuccessReport(ctx, h.Pool, from, to)
			if err != nil {
				return err
			}
			mu.Lock()
			page.CarrierPrefix = rows
			mu.Unlock()
			return nil
		})
		g.Go(func() error {
			rows, err := db.GetBillVsCostReport(ctx, h.Pool, from, to)
			if err != nil {
				return err
			}
			mu.Lock()
			page.BillComparison = rows
			mu.Unlock()
			return nil
		})
		g.Go(func() error {
			rows, err := db.GetCarrierCostComparisonReport(ctx, h.Pool, from, to)
			if err != nil {
				return err
			}
			mu.Lock()
			page.CostComparison = rows
			mu.Unlock()
			return nil
		})
		if err := g.Wait(); err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		now := time.Now().UTC()
		page.DefaultFrom = fromStr
		page.DefaultTo = toStr
		page.FromDate = fromStr
		page.ToDate = toStr
		page.Today = now.Format("2006-01-02")
		page.Yesterday = now.AddDate(0, 0, -1).Format("2006-01-02")
		page.Last7From = now.AddDate(0, 0, -6).Format("2006-01-02")
		page.Last30From = now.AddDate(0, 0, -29).Format("2006-01-02")
		page.MonthFrom = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC).Format("2006-01-02")
		page.BillByClientChart = aggregateBillByClientChart(page.BillComparison)

		tmpl, err := template.ParseFS(minisms.TemplateFS, "templates/admin/dashboard_reports.html")
		if err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		if err := tmpl.ExecuteTemplate(w, "dashboard_reports", page); err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
	}
}

func (h *Handlers) GetReportSMSByClient() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		from, to, _, _, err := parseReportDateRange(r)
		if err != nil {
			http.Error(w, "invalid date range", http.StatusBadRequest)
			return
		}
		rows, err := db.GetSMSByClientReport(r.Context(), h.Pool, from, to)
		if err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		labels := make([]string, 0, len(rows))
		data := make([]int64, 0, len(rows))
		for _, row := range rows {
			labels = append(labels, row.ClientName)
			data = append(data, row.TotalMessages)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"labels": labels,
			"datasets": []map[string]any{
				{"label": "SMS Sent", "data": data},
			},
		})
	}
}

func (h *Handlers) GetReportSMSByCarrier() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		from, to, _, _, err := parseReportDateRange(r)
		if err != nil {
			http.Error(w, "invalid date range", http.StatusBadRequest)
			return
		}
		rows, err := db.GetSMSByCarrierReport(r.Context(), h.Pool, from, to)
		if err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		labels := make([]string, 0, len(rows))
		p := make([]int64, 0, len(rows))
		f1 := make([]int64, 0, len(rows))
		f2 := make([]int64, 0, len(rows))
		for _, row := range rows {
			labels = append(labels, row.CarrierName)
			p = append(p, row.AsPrimary)
			f1 = append(f1, row.AsFailover1)
			f2 = append(f2, row.AsFailover2)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"labels": labels, "datasets": []map[string]any{
			{"label": "Primary", "data": p},
			{"label": "Failover 1", "data": f1},
			{"label": "Failover 2", "data": f2},
		}})
	}
}

func (h *Handlers) GetReportSuccessRatioClients() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		from, to, _, _, err := parseReportDateRange(r)
		if err != nil {
			http.Error(w, "invalid date range", http.StatusBadRequest)
			return
		}
		rows, err := db.GetSMSByClientReport(r.Context(), h.Pool, from, to)
		if err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		labels := make([]string, 0, len(rows))
		data := make([]float64, 0, len(rows))
		for _, row := range rows {
			labels = append(labels, row.ClientName)
			data = append(data, row.SuccessRatePct)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"labels": labels, "datasets": []map[string]any{{"label": "Success %", "data": data}}})
	}
}

func (h *Handlers) GetReportSuccessRatioCarriers() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		from, to, _, _, err := parseReportDateRange(r)
		if err != nil {
			http.Error(w, "invalid date range", http.StatusBadRequest)
			return
		}
		rows, err := db.GetSMSByCarrierReport(r.Context(), h.Pool, from, to)
		if err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		labels := make([]string, 0, len(rows))
		data := make([]float64, 0, len(rows))
		for _, row := range rows {
			labels = append(labels, row.CarrierName)
			data = append(data, row.SuccessRatePct)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"labels": labels, "datasets": []map[string]any{{"label": "Success %", "data": data}}})
	}
}

func (h *Handlers) GetReportCarrierPrefixSuccess() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		from, to, _, _, err := parseReportDateRange(r)
		if err != nil {
			http.Error(w, "invalid date range", http.StatusBadRequest)
			return
		}
		rows, err := db.GetCarrierPrefixSuccessReport(r.Context(), h.Pool, from, to)
		if err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(rows)
	}
}

func (h *Handlers) GetReportBillComparison() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		from, to, _, _, err := parseReportDateRange(r)
		if err != nil {
			http.Error(w, "invalid date range", http.StatusBadRequest)
			return
		}
		rows, err := db.GetBillVsCostReport(r.Context(), h.Pool, from, to)
		if err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		type agg struct{ billed, cost float64 }
		byClient := map[string]agg{}
		for _, row := range rows {
			a := byClient[row.ClientName]
			a.billed += row.ClientBilled
			a.cost += row.CarrierCost
			byClient[row.ClientName] = a
		}
		labels := make([]string, 0, len(byClient))
		billed := make([]float64, 0, len(byClient))
		cost := make([]float64, 0, len(byClient))
		for k, v := range byClient {
			labels = append(labels, k)
			billed = append(billed, v.billed)
			cost = append(cost, v.cost)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"labels": labels, "datasets": []map[string]any{
			{"label": "Client Billed", "data": billed},
			{"label": "Carrier Cost", "data": cost},
		}})
	}
}

func (h *Handlers) GetReportCostComparison() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		from, to, _, _, err := parseReportDateRange(r)
		if err != nil {
			http.Error(w, "invalid date range", http.StatusBadRequest)
			return
		}
		rows, err := db.GetCarrierCostComparisonReport(r.Context(), h.Pool, from, to)
		if err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		labels := make([]string, 0, len(rows))
		revenue := make([]float64, 0, len(rows))
		cost := make([]float64, 0, len(rows))
		for _, row := range rows {
			labels = append(labels, row.CarrierName)
			revenue = append(revenue, row.ClientRevenue)
			cost = append(cost, row.CarrierCost)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"labels": labels, "datasets": []map[string]any{
			{"label": "Client Revenue", "data": revenue},
			{"label": "Carrier Cost", "data": cost},
		}})
	}
}
