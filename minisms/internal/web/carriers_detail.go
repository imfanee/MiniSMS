package web

import (
	"encoding/base64"
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
	Title, CurrentPath, CSRFToken string
	Flash                         *Flash
	Carrier                       *db.CarrierFull
	BalanceFmt                    string
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
		p := CarrierDetailPage{Title: c.Name + " — Carrier", CurrentPath: r.URL.Path, CSRFToken: csrf.Token(r), Flash: f, Carrier: c, BalanceFmt: FormatBalance2dp(c.Balance, c.Currency), RateGroups: rg, ActiveTab: "headers"}
		_ = execT(w, h.CarrDetT, "base", p)
	}
}

// ——— Auth headers ———

// HeaderDisplay for templates.
type HeaderDisplay struct {
	CarrierID, HeaderID, Name, Masked, B64 string
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
			mask, _ := db.MaskedHeaderValue(x.Value)
			hs = append(hs, HeaderDisplay{
				CarrierID: cid, HeaderID: x.HeaderID, Name: x.HeaderName, Masked: mask,
				B64: base64.StdEncoding.EncodeToString([]byte(x.Value)),
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
		mask, _ := db.MaskedHeaderValue(row.Value)
		_ = execT(w, h.CarrFragT, "header_row", HeaderDisplay{
			CarrierID: cid, HeaderID: row.HeaderID, Name: row.HeaderName, Masked: mask,
			B64: base64.StdEncoding.EncodeToString([]byte(row.Value)),
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

// GetTemplatePanel
func (h *Handlers) GetTemplatePanel() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cid := chi.URLParam(r, "id")
		t, err := db.GetRequestTemplate(r.Context(), h.Pool, cid)
		if err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		ct, body, query, upd := "application/json", "{}", "", ""
		if t != nil {
			ct, body, query = t.ContentType, t.BodyTemplate, t.QueryTemplate
			if t.UpdatedAt != nil {
				upd = *t.UpdatedAt
			}
		}
		_ = execT(w, h.CarrFragT, "template_form", struct {
			CarrierID, CSRFToken, ContentType, Body, Query, Updated, Success string
			Errors                                                           map[string]string
		}{
			cid, csrf.Token(r), ct, body, query, upd, "", nil})
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
		em := validateRequestTemplate(ct, body, query)
		if len(em) > 0 {
			w.WriteHeader(422)
			_ = execT(w, h.CarrFragT, "template_form", struct {
				CarrierID, CSRFToken, ContentType, Body, Query, Updated, Success string
				Errors                                                           map[string]string
			}{
				cid, csrf.Token(r), ct, body, query, "", "", em})
			return
		}
		if err := db.UpsertRequestTemplate(r.Context(), h.Pool, cid, ct, body, query); err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		nt, _ := db.GetRequestTemplate(r.Context(), h.Pool, cid)
		upd := derefP(nt)
		_ = execT(w, h.CarrFragT, "template_form", struct {
			CarrierID, CSRFToken, ContentType, Body, Query, Updated, Success string
			Errors                                                           map[string]string
		}{
			cid, csrf.Token(r), ct, body, query, upd, "Template saved", nil})
	}
}

// ledgerPanel is shared struct for template
type ledgerPanelData struct {
	Carrier        *db.CarrierFull
	Entries        []db.LedgerEntryRow
	FormatBal      string
	CSRFToken      string
	PayErrors      map[string]string
	DefaultPayDate string
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
		_ = execT(w, h.CarrFragT, "ledger_panel", ledgerPanelData{
			Carrier: c, Entries: rows, FormatBal: FormatBalance2dp(c.Balance, c.Currency), CSRFToken: csrf.Token(r),
			DefaultPayDate: time.Now().UTC().Format("2006-01-02"),
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
		amt, d, v := validatePayment(r.FormValue("amount"), r.FormValue("payment_date"))
		if v != nil {
			rows, _ := db.ListLedgerEntries(r.Context(), h.Pool, cid)
			w.WriteHeader(422)
			_ = execT(w, h.CarrFragT, "ledger_panel", ledgerPanelData{
				Carrier: c, Entries: rows, FormatBal: FormatBalance2dp(c.Balance, c.Currency),
				CSRFToken: csrf.Token(r), PayErrors: v,
				DefaultPayDate: time.Now().UTC().Format("2006-01-02"),
			})
			return
		}
		_, err = db.RecordPayment(r.Context(), h.Pool, cid, amt, c.Currency, formPtr(r, "payment_reference"), formPtr(r, "invoice_number"), d, formPtr(r, "notes"))
		if err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		c2, _ := db.GetCarrier(r.Context(), h.Pool, cid)
		rows, _ := db.ListLedgerEntries(r.Context(), h.Pool, cid)
		_ = execT(w, h.CarrFragT, "ledger_panel", ledgerPanelData{
			Carrier: c2, Entries: rows, FormatBal: FormatBalance2dp(c2.Balance, c2.Currency), CSRFToken: csrf.Token(r),
			DefaultPayDate: time.Now().UTC().Format("2006-01-02"),
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
