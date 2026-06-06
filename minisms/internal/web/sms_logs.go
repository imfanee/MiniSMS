// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package web

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/csrf"
	"github.com/jackc/pgx/v5"
	"github.com/phpdave11/gofpdf"

	"github.com/minisms/minisms/internal/db"
	"github.com/minisms/minisms/internal/smslog"
)

type SMSLogFilter struct {
	ClientID         string
	CarrierID        string
	RoutingGroupID   string
	Status           string
	FailoverSequence string
	PrefixMatched    string
	DateFrom         string
	DateTo           string
	Page             int
	PageSize         int
}

type SMSLogRow struct {
	MessageID        string
	ClientName       *string
	ToNumber         string
	FromNumber       *string
	Segments         int
	TotalCharged     string
	Currency         string
	CarrierName      *string
	FailoverSequence int
	Status           string
	ReceivedAt       time.Time
}

type SMSLogListPage struct {
	AdminView
	Title       string
	CurrentPath string
	CSRFToken   string
	Flash       *Flash
	Filter      SMSLogFilter
	Clients     []db.ClientListRow
	Carriers    []db.CarrierRow
	Routing     []db.RoutingGroupListRow
	Rows        []SMSLogRow
	Total       int
	PageCount   int
	PrevPage    int
	NextPage    int
	Start       int
	End         int
	Pages       []int
	QueryBase   string
}

func (h *Handlers) ListSMSLogs() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		f := parseSMSLogFilter(r)
		rows, total, err := h.querySMSLogs(r, f)
		if err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		pageCount := (total + f.PageSize - 1) / f.PageSize
		if pageCount == 0 {
			pageCount = 1
		}
		start := 0
		if total > 0 {
			start = (f.Page-1)*f.PageSize + 1
		}
		end := (f.Page-1)*f.PageSize + len(rows)

		p := SMSLogListPage{
			Title:       "SMS Logs",
			CurrentPath: "/admin/sms-logs",
			CSRFToken:   csrf.Token(r),
			Flash:       GetFlash(w, r, "/", h.Config.SecretKey, h.Config.IsProduction()),
			Filter:      f,
			Rows:        rows,
			Total:       total,
			PageCount:   pageCount,
			PrevPage:    max(1, f.Page-1),
			NextPage:    min(pageCount, f.Page+1),
			Start:       start,
			End:         end,
			Pages:       pageWindow(f.Page, pageCount),
			QueryBase:   smsLogsQueryBase(f),
		}
		// HTMX boosted navbar/page navigation still sends HX-Request=true.
		// Only return the table fragment when the explicit target is the table container.
		if !isHTMX(r) || r.Header.Get("HX-Target") != "log-table-container" {
			p.Clients, _ = db.ListClients(r.Context(), h.Pool)
			p.Carriers, _ = db.ListCarriers(r.Context(), h.Pool)
			p.Routing, _ = db.ListRoutingGroups(r.Context(), h.Pool)
			if err := execT(w, h.SMSLogT, "base", p, r); err != nil {
				ServerError(w, r, err, h.Log, h.T500)
			}
			return
		}
		if err := execT(w, h.SMSLogFragT, "sms_logs_table", p); err != nil {
			ServerError(w, r, err, h.Log, h.T500)
		}
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

type SMSLogDetailPage struct {
	MessageID, ClientID, ToNumber, MessageBody, Encoding, RateApplied, TotalCharged, Currency, Status string
	ClientRef, FromNumber, PrefixMatched, CarrierMessageID, CarrierResponseBody, IngressTransport       *string
	RateGroupID, RoutingGroupID, RouteEntryID, CarrierID, DLRWebhookURL, DLRStatus, DLRForwardStatus    *string
	Segments, FailoverSequence, DLRForwardAttempts                                                      int
	DLRRequested                                                                                        bool
	DLRReceivedAt, DLRForwardedAt, DispatchedAt, DeliveredAt, FailedAt                                    *time.Time
	SourceAddrTON, SourceAddrNPI, DestAddrTON, DestAddrNPI                                              *int16
	CarrierSkipReason                                                                                     *string
	CarrierResponseCode                                                                                   *int
	ReceivedAt                                                                                            time.Time
	ClientName, CarrierName, RoutingGroupName                                                             *string
	Timeline                                                                                              []smslog.TimelineEventView
}

func (h *Handlers) SMSLogDetailModal() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		var d SMSLogDetailPage
		var timelineRaw []byte
		err := h.Pool.QueryRow(r.Context(), `
			SELECT sl.message_id::text, sl.client_id::text, sl.client_ref, sl.to_number, sl.from_number, sl.message_body,
				sl.segments, sl.encoding, sl.rate_group_id::text, sl.prefix_matched, sl.rate_applied::text, sl.total_charged::text, sl.currency::text,
				sl.routing_group_id::text, sl.route_entry_id::text, sl.failover_sequence, sl.carrier_id::text, sl.carrier_message_id,
				sl.carrier_response_code, sl.carrier_response_body, sl.status, sl.received_at, sl.dispatched_at, sl.delivered_at, sl.failed_at,
				sl.dlr_requested, sl.dlr_webhook_url, sl.dlr_status, sl.dlr_received_at, sl.dlr_forwarded_at, sl.dlr_forward_status, sl.dlr_forward_attempts,
				sl.source_addr_ton, sl.source_addr_npi, sl.dest_addr_ton, sl.dest_addr_npi, sl.carrier_skip_reason::text,
				sl.ingress_transport, sl.event_timeline,
				c.name, ca.name, rg.name
			FROM sms_logs sl
			LEFT JOIN clients c ON c.client_id = sl.client_id
			LEFT JOIN carriers ca ON ca.carrier_id = sl.carrier_id
			LEFT JOIN routing_groups rg ON rg.routing_group_id = sl.routing_group_id
			WHERE sl.message_id = $1::uuid`, id).
			Scan(
				&d.MessageID, &d.ClientID, &d.ClientRef, &d.ToNumber, &d.FromNumber, &d.MessageBody,
				&d.Segments, &d.Encoding, &d.RateGroupID, &d.PrefixMatched, &d.RateApplied, &d.TotalCharged, &d.Currency,
				&d.RoutingGroupID, &d.RouteEntryID, &d.FailoverSequence, &d.CarrierID, &d.CarrierMessageID,
				&d.CarrierResponseCode, &d.CarrierResponseBody, &d.Status, &d.ReceivedAt, &d.DispatchedAt, &d.DeliveredAt, &d.FailedAt,
				&d.DLRRequested, &d.DLRWebhookURL, &d.DLRStatus, &d.DLRReceivedAt, &d.DLRForwardedAt, &d.DLRForwardStatus, &d.DLRForwardAttempts,
				&d.SourceAddrTON, &d.SourceAddrNPI, &d.DestAddrTON, &d.DestAddrNPI, &d.CarrierSkipReason,
				&d.IngressTransport, &timelineRaw,
				&d.ClientName, &d.CarrierName, &d.RoutingGroupName,
			)
		if err != nil {
			if err == pgx.ErrNoRows {
				http.NotFound(w, r)
				return
			}
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		events := smslog.ParseTimeline(timelineRaw)
		if len(events) == 0 {
			ingress := "http"
			if d.IngressTransport != nil {
				ingress = *d.IngressTransport
			}
			events = smslog.SynthesizeTimeline(smslog.LegacyDetail{
				ReceivedAt: d.ReceivedAt, DispatchedAt: d.DispatchedAt, DeliveredAt: d.DeliveredAt, FailedAt: d.FailedAt,
				IngressTransport: ingress, CarrierName: d.CarrierName, FailoverSequence: d.FailoverSequence,
				CarrierResponseCode: d.CarrierResponseCode, CarrierResponseBody: d.CarrierResponseBody,
				CarrierMessageID: d.CarrierMessageID, CarrierSkipReason: d.CarrierSkipReason, Status: d.Status,
				DLRRequested: d.DLRRequested, DLRWebhookURL: d.DLRWebhookURL, DLRStatus: d.DLRStatus,
				DLRReceivedAt: d.DLRReceivedAt, DLRForwardedAt: d.DLRForwardedAt, DLRForwardStatus: d.DLRForwardStatus,
				DLRForwardAttempts: d.DLRForwardAttempts,
			})
		}
		d.Timeline = smslog.FormatViews(events)
		if err := execT(w, h.SMSLogFragT, "sms_log_detail_modal", d); err != nil {
			ServerError(w, r, err, h.Log, h.T500)
		}
	}
}

func (h *Handlers) ExportSMSLogsCSV() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		f := parseSMSLogFilter(r)
		rows, err := h.querySMSLogsForExport(r, f)
		if err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		filename := "sms_logs_" + time.Now().UTC().Format("20060102_150405") + ".csv"
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", "attachment; filename=\""+filename+"\"")
		cw := csv.NewWriter(w)
		if err := cw.Write([]string{"received_at", "message_id", "client", "to", "from", "segments", "total_charged", "currency", "carrier", "failover_sequence", "status"}); err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		for _, row := range rows {
			client := "-"
			if row.ClientName != nil {
				client = *row.ClientName
			}
			from := "-"
			if row.FromNumber != nil {
				from = *row.FromNumber
			}
			carrierName := "-"
			if row.CarrierName != nil {
				carrierName = *row.CarrierName
			}
			if err := cw.Write([]string{
				row.ReceivedAt.UTC().Format("2006-01-02 15:04:05"),
				row.MessageID,
				client,
				row.ToNumber,
				from,
				strconv.Itoa(row.Segments),
				row.TotalCharged,
				row.Currency,
				carrierName,
				strconv.Itoa(row.FailoverSequence),
				row.Status,
			}); err != nil {
				ServerError(w, r, err, h.Log, h.T500)
				return
			}
		}
		cw.Flush()
		if err := cw.Error(); err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
	}
}

func (h *Handlers) ExportSMSLogsPDF() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		f := parseSMSLogFilter(r)
		rows, err := h.querySMSLogsForExport(r, f)
		if err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		pdf := gofpdf.New("L", "mm", "A4", "")
		pdf.SetMargins(8, 8, 8)
		pdf.SetAutoPageBreak(true, 8)
		pdf.AddPage()
		pdf.SetFont("Arial", "B", 12)
		pdf.CellFormat(0, 8, "SMS Logs Export", "", 1, "L", false, 0, "")
		pdf.SetFont("Arial", "", 9)
		pdf.CellFormat(0, 6, "Generated: "+time.Now().UTC().Format(time.RFC3339), "", 1, "L", false, 0, "")
		pdf.CellFormat(0, 6, "Total rows: "+strconv.Itoa(len(rows)), "", 1, "L", false, 0, "")
		pdf.Ln(1)
		pdf.SetFont("Arial", "B", 10)
		pdf.CellFormat(0, 6, "Selected Criteria", "", 1, "L", false, 0, "")
		pdf.SetFont("Arial", "", 8)
		criteria := [][]string{
			{"Client ID", exportValueOrAll(f.ClientID)},
			{"Carrier ID", exportValueOrAll(f.CarrierID)},
			{"Routing Group ID", exportValueOrAll(f.RoutingGroupID)},
			{"Status", exportValueOrAll(f.Status)},
			{"Failover Sequence", exportValueOrAll(f.FailoverSequence)},
			{"Prefix Matched", exportValueOrAll(f.PrefixMatched)},
			{"Date From", exportValueOrAll(f.DateFrom)},
			{"Date To", exportValueOrAll(f.DateTo)},
		}
		for _, c := range criteria {
			pdf.SetFont("Arial", "B", 8)
			pdf.CellFormat(38, 5.5, c[0]+":", "0", 0, "L", false, 0, "")
			pdf.SetFont("Arial", "", 8)
			pdf.CellFormat(0, 5.5, c[1], "0", 1, "L", false, 0, "")
		}
		pdf.Ln(1)
		headers := []string{"Received", "Message ID", "Client", "To", "From", "Seg", "Charged", "Currency", "Carrier", "Fail", "Status"}
		widths := []float64{28, 28, 30, 28, 24, 10, 18, 14, 28, 10, 16}
		pdf.SetFont("Arial", "B", 8)
		for i := range headers {
			pdf.CellFormat(widths[i], 6, headers[i], "1", 0, "L", false, 0, "")
		}
		pdf.Ln(-1)
		pdf.SetFont("Arial", "", 8)
		for _, row := range rows {
			client := "-"
			if row.ClientName != nil {
				client = *row.ClientName
			}
			from := "-"
			if row.FromNumber != nil {
				from = *row.FromNumber
			}
			carrierName := "-"
			if row.CarrierName != nil {
				carrierName = *row.CarrierName
			}
			values := []string{
				row.ReceivedAt.UTC().Format("2006-01-02 15:04:05"),
				truncateText(row.MessageID, 18),
				truncateText(client, 20),
				row.ToNumber,
				truncateText(from, 16),
				strconv.Itoa(row.Segments),
				row.TotalCharged,
				row.Currency,
				truncateText(carrierName, 20),
				strconv.Itoa(row.FailoverSequence),
				row.Status,
			}
			for i := range values {
				pdf.CellFormat(widths[i], 5.5, values[i], "1", 0, "L", false, 0, "")
			}
			pdf.Ln(-1)
		}
		var buf bytes.Buffer
		if err := pdf.Output(&buf); err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		filename := "sms_logs_" + time.Now().UTC().Format("20060102_150405") + ".pdf"
		w.Header().Set("Content-Type", "application/pdf")
		w.Header().Set("Content-Disposition", "attachment; filename=\""+filename+"\"")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(buf.Bytes())
	}
}

func parseSMSLogFilter(r *http.Request) SMSLogFilter {
	q := r.URL.Query()
	f := SMSLogFilter{
		ClientID:         strings.TrimSpace(q.Get("client_id")),
		CarrierID:        strings.TrimSpace(q.Get("carrier_id")),
		RoutingGroupID:   strings.TrimSpace(q.Get("routing_group_id")),
		Status:           strings.TrimSpace(q.Get("status")),
		FailoverSequence: strings.TrimSpace(q.Get("failover_sequence")),
		PrefixMatched:    strings.TrimSpace(q.Get("prefix_matched")),
		DateFrom:         strings.TrimSpace(q.Get("date_from")),
		DateTo:           strings.TrimSpace(q.Get("date_to")),
		Page:             parseIntDefault(q.Get("page"), 1),
		PageSize:         parseIntDefault(q.Get("page_size"), 50),
	}
	if f.Page < 1 {
		f.Page = 1
	}
	if f.PageSize < 1 {
		f.PageSize = 50
	}
	if f.PageSize > 200 {
		f.PageSize = 200
	}
	return f
}

func parseIntDefault(s string, d int) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return d
	}
	return n
}

func truncateText(s string, max int) string {
	if len([]rune(s)) <= max {
		return s
	}
	runes := []rune(s)
	if max <= 3 {
		return string(runes[:max])
	}
	return string(runes[:max-3]) + "..."
}

func exportValueOrAll(v string) string {
	x := strings.TrimSpace(v)
	if x == "" {
		return "All"
	}
	return x
}

func (h *Handlers) querySMSLogs(r *http.Request, f SMSLogFilter) ([]SMSLogRow, int, error) {
	whereSQL, args := buildSMSLogWhere(f)
	baseFrom := ` FROM sms_logs sl
		LEFT JOIN clients c ON c.client_id = sl.client_id
		LEFT JOIN carriers ca ON ca.carrier_id = sl.carrier_id
		LEFT JOIN routing_groups rg ON rg.routing_group_id = sl.routing_group_id`
	var total int
	if err := h.Pool.QueryRow(r.Context(), "SELECT COUNT(*)"+baseFrom+whereSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	limitArg := len(args) + 1
	offsetArg := len(args) + 2
	args = append(args, f.PageSize, (f.Page-1)*f.PageSize)
	q := fmt.Sprintf(`SELECT sl.message_id::text, c.name, sl.to_number, sl.from_number, sl.segments, sl.total_charged::text, sl.currency::text,
		ca.name, sl.failover_sequence, sl.status, sl.received_at %s %s
		ORDER BY sl.received_at DESC LIMIT $%d OFFSET $%d`, baseFrom, whereSQL, limitArg, offsetArg)
	rows, err := h.Pool.Query(r.Context(), q, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []SMSLogRow
	for rows.Next() {
		var x SMSLogRow
		if err := rows.Scan(&x.MessageID, &x.ClientName, &x.ToNumber, &x.FromNumber, &x.Segments, &x.TotalCharged, &x.Currency, &x.CarrierName, &x.FailoverSequence, &x.Status, &x.ReceivedAt); err != nil {
			return nil, 0, err
		}
		out = append(out, x)
	}
	return out, total, rows.Err()
}

func (h *Handlers) querySMSLogsForExport(r *http.Request, f SMSLogFilter) ([]SMSLogRow, error) {
	whereSQL, args := buildSMSLogWhere(f)
	baseFrom := ` FROM sms_logs sl
		LEFT JOIN clients c ON c.client_id = sl.client_id
		LEFT JOIN carriers ca ON ca.carrier_id = sl.carrier_id
		LEFT JOIN routing_groups rg ON rg.routing_group_id = sl.routing_group_id`
	q := fmt.Sprintf(`SELECT sl.message_id::text, c.name, sl.to_number, sl.from_number, sl.segments, sl.total_charged::text, sl.currency::text,
		ca.name, sl.failover_sequence, sl.status, sl.received_at %s %s
		ORDER BY sl.received_at DESC`, baseFrom, whereSQL)
	rows, err := h.Pool.Query(r.Context(), q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SMSLogRow
	for rows.Next() {
		var x SMSLogRow
		if err := rows.Scan(&x.MessageID, &x.ClientName, &x.ToNumber, &x.FromNumber, &x.Segments, &x.TotalCharged, &x.Currency, &x.CarrierName, &x.FailoverSequence, &x.Status, &x.ReceivedAt); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

func buildSMSLogWhere(f SMSLogFilter) (string, []any) {
	var args []any
	var where []string
	add := func(sql string, v any) {
		args = append(args, v)
		where = append(where, fmt.Sprintf(sql, len(args)))
	}
	if f.ClientID != "" {
		add("sl.client_id = $%d::uuid", f.ClientID)
	}
	if f.CarrierID != "" {
		add("sl.carrier_id = $%d::uuid", f.CarrierID)
	}
	if f.RoutingGroupID != "" {
		add("sl.routing_group_id = $%d::uuid", f.RoutingGroupID)
	}
	if f.Status != "" {
		add("sl.status = $%d", f.Status)
	}
	if f.FailoverSequence != "" {
		add("sl.failover_sequence = $%d::smallint", f.FailoverSequence)
	}
	if f.PrefixMatched != "" {
		add("sl.prefix_matched ILIKE ('%%' || $%d || '%%')", f.PrefixMatched)
	}
	if f.DateFrom != "" {
		add("sl.received_at >= $%d::date", f.DateFrom)
	}
	if f.DateTo != "" {
		add("sl.received_at <= ($%d::date + interval '1 day' - interval '1 second')", f.DateTo)
	}
	whereSQL := ""
	if len(where) > 0 {
		whereSQL = " WHERE " + strings.Join(where, " AND ")
	}
	return whereSQL, args
}

func smsLogsQueryBase(f SMSLogFilter) string {
	v := url.Values{}
	if f.ClientID != "" {
		v.Set("client_id", f.ClientID)
	}
	if f.CarrierID != "" {
		v.Set("carrier_id", f.CarrierID)
	}
	if f.RoutingGroupID != "" {
		v.Set("routing_group_id", f.RoutingGroupID)
	}
	if f.Status != "" {
		v.Set("status", f.Status)
	}
	if f.FailoverSequence != "" {
		v.Set("failover_sequence", f.FailoverSequence)
	}
	if f.PrefixMatched != "" {
		v.Set("prefix_matched", f.PrefixMatched)
	}
	if f.DateFrom != "" {
		v.Set("date_from", f.DateFrom)
	}
	if f.DateTo != "" {
		v.Set("date_to", f.DateTo)
	}
	v.Set("page_size", strconv.Itoa(f.PageSize))
	enc := v.Encode()
	if enc == "" {
		return ""
	}
	return "&" + enc
}

func pageWindow(current, total int) []int {
	start := current - 2
	if start < 1 {
		start = 1
	}
	end := start + 4
	if end > total {
		end = total
		start = end - 4
		if start < 1 {
			start = 1
		}
	}
	var out []int
	for i := start; i <= end; i++ {
		out = append(out, i)
	}
	return out
}
