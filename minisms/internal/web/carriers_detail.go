// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package web

import (
	"context"
	"encoding/base64"
	"html/template"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/csrf"
	"github.com/jackc/pgx/v5"

	"github.com/minisms/minisms/internal/db"
)

// CarrierDetailPage is full page data for carrier detail.
type CarrierDetailPage struct {
	AdminView
	Title, CurrentPath, CSRFToken string
	Flash                         *Flash
	Carrier                       *db.CarrierFull
	BalanceFmt                    string
	InterconnectLabel             string
	RateGroups                    []db.RateGroupOption
	ActiveTab                     string
}

// GetCarrierDetail GET /admin/carriers/{id}
func (h *Handlers) GetCarrierDetail() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		c, err := db.GetCarrier(r.Context(), h.Pool, id)
		if err == pgx.ErrNoRows {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		rg, _ := db.ListRateGroupsIDName(r.Context(), h.Pool)
		f := GetFlash(w, r, "/", h.Config.SecretKey, h.Config.IsProduction())
		icLabel := "HTTP"
		if interconnectType(c) == "smpp" {
			icLabel = "SMPP"
		}
		p := CarrierDetailPage{
			Title: c.Name + " — Carrier", CurrentPath: r.URL.Path, CSRFToken: csrf.Token(r), Flash: f,
			Carrier: c, BalanceFmt: FormatBalance2dp(c.Balance, c.Currency), InterconnectLabel: icLabel,
			RateGroups: rg, ActiveTab: "interconnect",
		}
		_ = execT(w, h.CarrDetT, "base", p, r)
	}
}

// ——— Auth headers ———

// HeaderDisplay for templates.
type HeaderDisplay struct {
	CarrierID, HeaderID, Name, Value string
}

// templateTextareaHTML keeps JSON/XML quotes literal inside <textarea> (html/template escapes " otherwise).
func templateTextareaHTML(s string) template.HTML {
	if s == "" {
		return ""
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '&':
			b.WriteString("&amp;")
		case '<':
			b.WriteString("&lt;")
		default:
			b.WriteByte(s[i])
		}
	}
	return template.HTML(b.String())
}

func templateBodyB64(s string) string {
	if s == "" {
		return ""
	}
	return base64.StdEncoding.EncodeToString([]byte(s))
}

// TemplatePanelData is the request template form.
type TemplatePanelData struct {
	CarrierID, CSRFToken, ContentType, Updated, Success, HTTPMethod string
	Body, Query                                                      template.HTML
	BodyB64, QueryB64                                                string
	Errors                                                           map[string]string
}

// ListAuthHeaders GET
func (h *Handlers) ListAuthHeaders() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cid := chi.URLParam(r, "id")
		rows, err := db.ListAuthHeaders(r.Context(), h.Pool, cid, h.Config.SecretKey)
		if err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		hs := make([]HeaderDisplay, 0, len(rows))
		for _, x := range rows {
			hs = append(hs, HeaderDisplay{
				CarrierID: cid, HeaderID: x.HeaderID, Name: x.HeaderName, Value: x.Value,
			})
		}
		d := struct {
			CarrierID, CSRFToken string
			Headers              []HeaderDisplay
		}{cid, csrf.Token(r), hs}
		_ = execT(w, h.CarrFragT, "headers_table", d)
	}
}

// ShowAddAuthHeaderForm
func (h *Handlers) ShowAddAuthHeaderForm() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !isHTMX(r) {
			http.Redirect(w, r, "/admin/carriers/"+chi.URLParam(r, "id"), http.StatusFound)
			return
		}
		cid := chi.URLParam(r, "id")
		_ = execT(w, h.CarrFragT, "add_auth_header_row", struct {
			CarrierID, CSRFToken, HName, HValue string
			Errors                              map[string]string
		}{cid, csrf.Token(r), "", "", nil})
	}
}

// CreateAuthHeader POST
func (h *Handlers) CreateAuthHeader() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cid := chi.URLParam(r, "id")
		_ = r.ParseForm()
		hn := r.FormValue("header_name")
		hv := r.FormValue("header_value")
		em := validateHeaderForm(hn, hv)
		if len(em) > 0 {
			w.WriteHeader(422)
			_ = execT(w, h.CarrFragT, "add_auth_header_row", struct {
				CarrierID, CSRFToken, HName, HValue string
				Errors                              map[string]string
			}{
				cid, csrf.Token(r), hn, hv, em})
			return
		}
		hid, err := db.CreateAuthHeader(r.Context(), h.Pool, cid, hn, hv, h.Config.SecretKey)
		if err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		row, err := db.GetAuthHeaderRow(r.Context(), h.Pool, cid, hid, h.Config.SecretKey)
		if err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		_ = execT(w, h.CarrFragT, "header_row", HeaderDisplay{
			CarrierID: cid, HeaderID: row.HeaderID, Name: row.HeaderName, Value: row.Value,
		})
	}
}

// DeleteAuthHeader
func (h *Handlers) DeleteAuthHeader() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cid := chi.URLParam(r, "id")
		hid := chi.URLParam(r, "header_id")
		_, _ = db.DeleteAuthHeader(r.Context(), h.Pool, cid, hid)
		w.WriteHeader(http.StatusOK)
	}
}

func (h *Handlers) templatePanelData(ctx context.Context, cid, csrfToken string, success string, errs map[string]string) (TemplatePanelData, error) {
	c, err := db.GetCarrier(ctx, h.Pool, cid)
	if err != nil {
		return TemplatePanelData{}, err
	}
	t, err := db.GetRequestTemplate(ctx, h.Pool, cid)
	if err != nil {
		return TemplatePanelData{}, err
	}
	ct, body, query, upd := "application/json", "{}", "", ""
	if t != nil {
		ct, body, query = t.ContentType, t.BodyTemplate, t.QueryTemplate
		if t.UpdatedAt != nil {
			upd = *t.UpdatedAt
		}
	}
	method := strings.ToUpper(strings.TrimSpace(c.HTTPMethod))
	if method == "" {
		method = "POST"
	}
	return TemplatePanelData{
		CarrierID: cid, CSRFToken: csrfToken, ContentType: ct,
		Body: templateTextareaHTML(body), Query: templateTextareaHTML(query),
		BodyB64: templateBodyB64(body), QueryB64: templateBodyB64(query),
		Updated: upd, Success: success, HTTPMethod: method, Errors: errs,
	}, nil
}

// GetTemplatePanel
func (h *Handlers) GetTemplatePanel() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cid := chi.URLParam(r, "id")
		d, err := h.templatePanelData(r.Context(), cid, csrf.Token(r), "", nil)
		if err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		_ = execT(w, h.CarrFragT, "template_form", d)
	}
}

func derefP(t *db.RequestTemplate) string {
	if t != nil && t.UpdatedAt != nil {
		return *t.UpdatedAt
	}
	return ""
}

// SaveTemplate POST
func (h *Handlers) SaveTemplate() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cid := chi.URLParam(r, "id")
		_ = r.ParseForm()
		ct, body, query := r.FormValue("content_type"), r.FormValue("body_template"), r.FormValue("query_template")
		c, err := db.GetCarrier(r.Context(), h.Pool, cid)
		if err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		method := strings.ToUpper(strings.TrimSpace(c.HTTPMethod))
		storedBody, storedQuery := "", ""
		if existing, _ := db.GetRequestTemplate(r.Context(), h.Pool, cid); existing != nil {
			storedBody = existing.BodyTemplate
			storedQuery = existing.QueryTemplate
		}
		body = resolveGETBodyTemplateForSave(method, body, storedBody)
		query = resolvePOSTQueryTemplateForSave(method, query, storedQuery)
		em := validateRequestTemplate(method, ct, body, query)
		if len(em) > 0 {
			w.WriteHeader(422)
			d, derr := h.templatePanelData(r.Context(), cid, csrf.Token(r), "", em)
			if derr != nil {
				d = TemplatePanelData{
					CarrierID: cid, CSRFToken: csrf.Token(r), ContentType: ct,
					Body: templateTextareaHTML(body), Query: templateTextareaHTML(query),
					BodyB64: templateBodyB64(body), QueryB64: templateBodyB64(query),
					HTTPMethod: method, Errors: em,
				}
			} else {
				d.Body = templateTextareaHTML(body)
				d.Query = templateTextareaHTML(query)
				d.BodyB64 = templateBodyB64(body)
				d.QueryB64 = templateBodyB64(query)
				d.ContentType = ct
			}
			_ = execT(w, h.CarrFragT, "template_form", d)
			return
		}
		if err := db.UpsertRequestTemplate(r.Context(), h.Pool, cid, ct, body, query); err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		h.reloadRouteCache(r.Context())
		d, err := h.templatePanelData(r.Context(), cid, csrf.Token(r), "Template saved", nil)
		if err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		// Ensure the re-rendered form shows what we just persisted (editors read these fields).
		d.Body = templateTextareaHTML(body)
		d.Query = templateTextareaHTML(query)
		d.BodyB64 = templateBodyB64(body)
		d.QueryB64 = templateBodyB64(query)
		d.ContentType = ct
		_ = execT(w, h.CarrFragT, "template_form", d)
	}
}

// ledgerPanel is shared struct for template
type ledgerPanelData struct {
	Carrier           *db.CarrierFull
	Entries           []db.LedgerEntryRow
	FormatBal         string
	CSRFToken         string
	PayErrors         map[string]string
	DefaultPayDate    string
	OpenInvoices      []db.OpenInvoiceOption
	RefType           string
	SelectedInvoiceID string
	OtherReference    string
}

// ListLedger GET
func (h *Handlers) ListLedger() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cid := chi.URLParam(r, "id")
		c, err := db.GetCarrier(r.Context(), h.Pool, cid)
		if err == pgx.ErrNoRows {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		rows, err := db.ListLedgerEntries(r.Context(), h.Pool, cid)
		if err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		openInv, _ := db.ListOpenInvoices(r.Context(), h.Pool, db.InvoiceEntityCarrier, cid)
		_ = execT(w, h.CarrFragT, "ledger_panel", ledgerPanelData{
			Carrier: c, Entries: rows, FormatBal: FormatBalance2dp(c.Balance, c.Currency), CSRFToken: csrf.Token(r),
			DefaultPayDate: time.Now().UTC().Format("2006-01-02"), OpenInvoices: openInv, RefType: "other",
		})
	}
}

// RecordPayment POST
func (h *Handlers) RecordPayment() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cid := chi.URLParam(r, "id")
		_ = r.ParseForm()
		c, err := db.GetCarrier(r.Context(), h.Pool, cid)
		if err == pgx.ErrNoRows {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		errs := map[string]string{}
		amt, d, payErrs := validatePayment(r.FormValue("amount"), r.FormValue("payment_date"))
		for k, msg := range payErrs {
			errs[k] = msg
		}
		parsed := parsePaymentReference(r, errs)
		openInv, _ := db.ListOpenInvoices(r.Context(), h.Pool, db.InvoiceEntityCarrier, cid)
		payDateFld := strings.TrimSpace(r.FormValue("payment_date"))
		if payDateFld == "" {
			payDateFld = time.Now().UTC().Format("2006-01-02")
		}
		if len(errs) > 0 {
			rows, _ := db.ListLedgerEntries(r.Context(), h.Pool, cid)
			w.WriteHeader(422)
			_ = execT(w, h.CarrFragT, "ledger_panel", ledgerPanelData{
				Carrier: c, Entries: rows, FormatBal: FormatBalance2dp(c.Balance, c.Currency),
				CSRFToken: csrf.Token(r), PayErrors: errs, DefaultPayDate: payDateFld,
				OpenInvoices: openInv, RefType: parsed.RefType, SelectedInvoiceID: parsed.InvoiceID,
				OtherReference: strings.TrimSpace(r.FormValue("payment_reference")),
			})
			return
		}
		tx, err := h.Pool.Begin(r.Context())
		if err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		defer tx.Rollback(r.Context())
		paymentRef, invoiceNum, _, err := resolvePaymentFields(r.Context(), tx, db.InvoiceEntityCarrier, cid, amt, parsed, errs)
		if len(errs) > 0 {
			rows, _ := db.ListLedgerEntries(r.Context(), h.Pool, cid)
			w.WriteHeader(422)
			_ = execT(w, h.CarrFragT, "ledger_panel", ledgerPanelData{
				Carrier: c, Entries: rows, FormatBal: FormatBalance2dp(c.Balance, c.Currency),
				CSRFToken: csrf.Token(r), PayErrors: errs, DefaultPayDate: payDateFld,
				OpenInvoices: openInv, RefType: parsed.RefType, SelectedInvoiceID: parsed.InvoiceID,
				OtherReference: strings.TrimSpace(r.FormValue("payment_reference")),
			})
			return
		}
		if err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		_, err = db.RecordPayment(r.Context(), tx, cid, amt, c.Currency, paymentRef, invoiceNum, d, formPtr(r, "notes"))
		if err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		if err := tx.Commit(r.Context()); err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		cname := c.Name
		auditExtra := map[string]string{"amount": amt, "currency": c.Currency}
		if invoiceNum != nil {
			auditExtra["invoice_number"] = *invoiceNum
		}
		h.recordAudit(r, "carrier.payment", "carrier", &cid, &cname, auditExtra)
		c2, _ := db.GetCarrier(r.Context(), h.Pool, cid)
		rows, _ := db.ListLedgerEntries(r.Context(), h.Pool, cid)
		openInv, _ = db.ListOpenInvoices(r.Context(), h.Pool, db.InvoiceEntityCarrier, cid)
		_ = execT(w, h.CarrFragT, "ledger_panel", ledgerPanelData{
			Carrier: c2, Entries: rows, FormatBal: FormatBalance2dp(c2.Balance, c2.Currency), CSRFToken: csrf.Token(r),
			DefaultPayDate: time.Now().UTC().Format("2006-01-02"), OpenInvoices: openInv, RefType: "other",
		})
	}
}

func formPtr(r *http.Request, k string) *string {
	s := strings.TrimSpace(r.FormValue(k))
	if s == "" {
		return nil
	}
	return &s
}

// GetUsagePanel GET
func (h *Handlers) GetUsagePanel() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cid := chi.URLParam(r, "id")
		c, err := db.GetCarrier(r.Context(), h.Pool, cid)
		if err == pgx.ErrNoRows {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		u, err := db.GetUsageTotals(r.Context(), h.Pool, cid)
		if err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		sp, _ := db.GetCarrier30DayChargeSum(r.Context(), h.Pool, cid)
		_ = execT(w, h.CarrFragT, "usage_panel", struct {
			TotalMessages, TotalSegments int64
			TotalChargedFmt, Spend30dFmt string
			Currency                     string
			LastAt                       *string
		}{
			u.TotalMessages, u.TotalSegments, FormatBalance2dp(u.TotalAmount, c.Currency), FormatBalance2dp(sp, c.Currency), c.Currency, u.LastMessageAt,
		})
	}
}
