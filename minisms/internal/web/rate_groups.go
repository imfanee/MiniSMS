// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package web

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/csrf"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/minisms/minisms/internal/db"
)

var (
	reRGCurrency = regexp.MustCompile(`^[A-Z]{3}$`)
	rePrefix     = regexp.MustCompile(`^(\*|[0-9]{1,15})$`)
	reRate       = regexp.MustCompile(`^[0-9]{1,18}(\.[0-9]{1,6})?$`)
)

type RGListPage struct {
	AdminView
	Title, CurrentPath, CSRFToken string
	Flash                         *Flash
	Rows                          []db.RateGroupListRow
	HasRows                       bool
}

type RGRowView struct {
	Row     db.RateGroupListRow
	CanDel  bool
	DelHint string
}

type RGEntryView struct {
	Entry          db.RateEntry
	Currency       string
	PrefixLabel    string
	RateFmt        string
	EffectiveToLbl string
	IsActive       bool
}

func mapRGEntry(e db.RateEntry, currency string) RGEntryView {
	lbl := e.Prefix
	if e.Prefix == "*" {
		lbl = "* (catch-all)"
	}
	rateFmt := e.RatePerSMS
	if f, err := strconv.ParseFloat(e.RatePerSMS, 64); err == nil {
		rateFmt = fmt.Sprintf("%.6f", f)
	}
	effTo := "Indefinite"
	if e.EffectiveTo != nil && strings.TrimSpace(*e.EffectiveTo) != "" {
		effTo = *e.EffectiveTo
	}
	now := time.Now().UTC().Format("2006-01-02")
	active := e.EffectiveFrom <= now && (e.EffectiveTo == nil || *e.EffectiveTo >= now)
	return RGEntryView{Entry: e, Currency: currency, PrefixLabel: lbl, RateFmt: rateFmt, EffectiveToLbl: effTo, IsActive: active}
}

func validateRateGroup(name, currency string) map[string]string {
	m := map[string]string{}
	if strings.TrimSpace(name) == "" {
		m["name"] = "Name is required"
	}
	if !reRGCurrency.MatchString(strings.ToUpper(strings.TrimSpace(currency))) {
		m["currency"] = "Currency must be 3 uppercase letters"
	}
	return m
}

func mustCurrencies(ctx context.Context, pool *pgxpool.Pool) []db.Currency {
	ccys, _ := db.ListActiveCurrencies(ctx, pool)
	return ccys
}

func parseRateEntryForm(r *http.Request) (prefix, desc, rate string, from time.Time, to *time.Time, errs map[string]string) {
	errs = map[string]string{}
	prefix = strings.TrimSpace(r.FormValue("prefix"))
	desc = strings.TrimSpace(r.FormValue("description"))
	rate = strings.TrimSpace(r.FormValue("rate_per_sms"))
	fromStr := strings.TrimSpace(r.FormValue("effective_from"))
	toStr := strings.TrimSpace(r.FormValue("effective_to"))

	if !rePrefix.MatchString(prefix) {
		errs["prefix"] = "Prefix must be numeric or *"
	}
	if !reRate.MatchString(rate) {
		errs["rate_per_sms"] = "Use up to 18 digits and 6 decimals"
	} else if f, e := strconv.ParseFloat(rate, 64); e != nil || f < 0 {
		errs["rate_per_sms"] = "Rate must be >= 0"
	}
	if fromStr == "" {
		errs["effective_from"] = "Effective from date is required"
	} else {
		d, e := time.Parse("2006-01-02", fromStr)
		if e != nil {
			errs["effective_from"] = "Invalid date"
		} else {
			minPast := time.Now().UTC().AddDate(-10, 0, 0)
			if d.Before(minPast) {
				errs["effective_from"] = "Date is too far in the past"
			}
			from = d
		}
	}
	if toStr != "" {
		d, e := time.Parse("2006-01-02", toStr)
		if e != nil {
			errs["effective_to"] = "Invalid date"
		} else {
			to = &d
			if !from.IsZero() && d.Before(from) {
				errs["effective_to"] = "Must be same or after effective from"
			}
		}
	}
	return
}

func (h *Handlers) ListRateGroups() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := db.ListRateGroups(r.Context(), h.Pool)
		if err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		f := GetFlash(w, r, "/", h.Config.SecretKey, h.Config.IsProduction())
		p := RGListPage{
			Title: "Rate Groups", CurrentPath: "/admin/rate-groups", CSRFToken: csrf.Token(r),
			Flash: f, Rows: rows, HasRows: len(rows) > 0,
		}
		_ = execT(w, h.RGListT, "base", p, r)
	}
}

func (h *Handlers) ShowAddRateGroupForm() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !isHTMX(r) {
			http.Redirect(w, r, "/admin/rate-groups", http.StatusFound)
			return
		}
		_ = execT(w, h.RGFragT, "rg_add_form_row", struct {
			CSRFToken, Name, Currency, Description string
			Currencies                             []db.Currency
			Errors                                 map[string]string
		}{csrf.Token(r), "", "GBP", "", mustCurrencies(r.Context(), h.Pool), nil})
	}
}

func (h *Handlers) CreateRateGroup() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		name := strings.TrimSpace(r.FormValue("name"))
		currency := strings.ToUpper(strings.TrimSpace(r.FormValue("currency")))
		desc := strings.TrimSpace(r.FormValue("description"))
		errs := validateRateGroup(name, currency)
		if len(errs) > 0 {
			w.WriteHeader(http.StatusUnprocessableEntity)
			_ = execT(w, h.RGFragT, "rg_add_form_row", struct {
				CSRFToken, Name, Currency, Description string
				Currencies                             []db.Currency
				Errors                                 map[string]string
			}{csrf.Token(r), name, currency, desc, mustCurrencies(r.Context(), h.Pool), errs})
			return
		}
		id, err := db.CreateRateGroup(r.Context(), h.Pool, db.CreateRateGroupParams{
			Name: name, Currency: currency, Description: strPtr(desc),
		})
		if err != nil {
			if errors.Is(err, db.ErrDuplicateRateGroupName) {
				w.WriteHeader(http.StatusUnprocessableEntity)
				_ = execT(w, h.RGFragT, "rg_add_form_row", struct {
					CSRFToken, Name, Currency, Description string
					Currencies                             []db.Currency
					Errors                                 map[string]string
				}{csrf.Token(r), name, currency, desc, mustCurrencies(r.Context(), h.Pool), map[string]string{"name": "A rate group with this name already exists"}})
				return
			}
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		all, err := db.ListRateGroups(r.Context(), h.Pool)
		if err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		for _, x := range all {
			if x.RateGroupID == id {
				_ = execT(w, h.RGFragT, "rg_row", x)
				return
			}
		}
		http.Error(w, "created row missing", http.StatusInternalServerError)
	}
}

func (h *Handlers) ShowEditRateGroupForm() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		if !isHTMX(r) {
			http.Redirect(w, r, "/admin/rate-groups", http.StatusFound)
			return
		}
		g, err := db.GetRateGroup(r.Context(), h.Pool, id)
		if err == pgx.ErrNoRows {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		desc := ""
		if g.Description != nil {
			desc = *g.Description
		}
		_ = execT(w, h.RGFragT, "rg_edit_form_row", struct {
			RateGroupID, CSRFToken, Name, Currency, Description string
			Currencies                                          []db.Currency
			Errors                                              map[string]string
		}{g.RateGroupID, csrf.Token(r), g.Name, g.Currency, desc, mustCurrencies(r.Context(), h.Pool), nil})
	}
}

func (h *Handlers) GetRateGroupRowFragment() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		all, err := db.ListRateGroups(r.Context(), h.Pool)
		if err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		for _, x := range all {
			if x.RateGroupID == id {
				_ = execT(w, h.RGFragT, "rg_row", x)
				return
			}
		}
		http.NotFound(w, r)
	}
}

func (h *Handlers) UpdateRateGroup() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		_ = r.ParseForm()
		name := strings.TrimSpace(r.FormValue("name"))
		currency := strings.ToUpper(strings.TrimSpace(r.FormValue("currency")))
		desc := strings.TrimSpace(r.FormValue("description"))
		errs := validateRateGroup(name, currency)
		if len(errs) > 0 {
			w.WriteHeader(http.StatusUnprocessableEntity)
			_ = execT(w, h.RGFragT, "rg_edit_form_row", struct {
				RateGroupID, CSRFToken, Name, Currency, Description string
				Currencies                                          []db.Currency
				Errors                                              map[string]string
			}{id, csrf.Token(r), name, currency, desc, mustCurrencies(r.Context(), h.Pool), errs})
			return
		}
		err := db.UpdateRateGroup(r.Context(), h.Pool, id, db.UpdateRateGroupParams{
			Name: name, Currency: currency, Description: strPtr(desc),
		})
		if err == pgx.ErrNoRows {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			if errors.Is(err, db.ErrDuplicateRateGroupName) {
				w.WriteHeader(http.StatusUnprocessableEntity)
				_ = execT(w, h.RGFragT, "rg_edit_form_row", struct {
					RateGroupID, CSRFToken, Name, Currency, Description string
					Currencies                                          []db.Currency
					Errors                                              map[string]string
				}{id, csrf.Token(r), name, currency, desc, mustCurrencies(r.Context(), h.Pool), map[string]string{"name": "A rate group with this name already exists"}})
				return
			}
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		all, e2 := db.ListRateGroups(r.Context(), h.Pool)
		if e2 != nil {
			ServerError(w, r, e2, h.Log, h.T500)
			return
		}
		for _, x := range all {
			if x.RateGroupID == id {
				_ = execT(w, h.RGFragT, "rg_row", x)
				return
			}
		}
		http.NotFound(w, r)
	}
}

func (h *Handlers) DeleteRateGroup() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		cRefs, err := db.CountRateGroupCarrierRefs(r.Context(), h.Pool, id)
		if err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		clRefs, err := db.CountRateGroupClientRefs(r.Context(), h.Pool, id)
		if err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		if cRefs > 0 || clRefs > 0 {
			w.WriteHeader(http.StatusConflict)
			msg := fmt.Sprintf("Cannot delete: referenced by %d carrier(s) and %d client(s)", cRefs, clRefs)
			_, _ = w.Write([]byte(`<div class="alert alert-danger py-1 px-2 small mt-2 mb-0">` + msg + `</div>`))
			return
		}
		_, err = db.DeleteRateGroup(r.Context(), h.Pool, id)
		if err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		w.WriteHeader(http.StatusOK)
	}
}

func (h *Handlers) GetRateGroupDetail() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		g, err := db.GetRateGroup(r.Context(), h.Pool, id)
		if err == pgx.ErrNoRows {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		entries, err := db.ListEntries(r.Context(), h.Pool, id)
		if err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		ev := make([]RGEntryView, 0, len(entries))
		for _, e := range entries {
			ev = append(ev, mapRGEntry(e, g.Currency))
		}
		_ = execT(w, h.RGDetT, "base", struct {
			AdminView
			Title, CurrentPath, CSRFToken string
			Flash                         *Flash
			Group                         *db.RateGroup
			Entries                       []RGEntryView
		}{
			Title: "Rate Group", CurrentPath: "/admin/rate-groups", CSRFToken: csrf.Token(r),
			Flash: GetFlash(w, r, "/", h.Config.SecretKey, h.Config.IsProduction()), Group: g, Entries: ev,
		}, r)
	}
}

func (h *Handlers) ShowAddEntryForm() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !isHTMX(r) {
			http.Redirect(w, r, "/admin/rate-groups/"+chi.URLParam(r, "id"), http.StatusFound)
			return
		}
		id := chi.URLParam(r, "id")
		_ = execT(w, h.RGFragT, "entry_add_row", struct {
			RateGroupID, CSRFToken, Prefix, Description, RatePerSMS, EffectiveFrom, EffectiveTo string
			Errors                                                                              map[string]string
		}{id, csrf.Token(r), "", "", "0.000000", time.Now().UTC().Format("2006-01-02"), "", nil})
	}
}

func (h *Handlers) CreateRateEntry() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		_ = r.ParseForm()
		prefix, desc, rate, from, to, errs := parseRateEntryForm(r)
		if len(errs) == 0 {
			if dup, _ := db.ExistsEntryKey(r.Context(), h.Pool, id, prefix, from, nil); dup {
				errs["prefix"] = "A rate entry for this prefix with the same effective date already exists"
			}
		}
		if len(errs) > 0 {
			w.WriteHeader(http.StatusUnprocessableEntity)
			_ = execT(w, h.RGFragT, "entry_add_row", struct {
				RateGroupID, CSRFToken, Prefix, Description, RatePerSMS, EffectiveFrom, EffectiveTo string
				Errors                                                                              map[string]string
			}{id, csrf.Token(r), prefix, desc, rate, r.FormValue("effective_from"), r.FormValue("effective_to"), errs})
			return
		}
		entryID, err := db.CreateEntry(r.Context(), h.Pool, id, db.UpsertRateEntryParams{
			Prefix: prefix, Description: strPtr(desc), RatePerSMS: rate, EffectiveFrom: from, EffectiveTo: to,
		})
		if err != nil {
			if errors.Is(err, db.ErrDuplicateRateEntry) {
				w.WriteHeader(http.StatusUnprocessableEntity)
				_ = execT(w, h.RGFragT, "entry_add_row", struct {
					RateGroupID, CSRFToken, Prefix, Description, RatePerSMS, EffectiveFrom, EffectiveTo string
					Errors                                                                              map[string]string
				}{id, csrf.Token(r), prefix, desc, rate, r.FormValue("effective_from"), r.FormValue("effective_to"), map[string]string{
					"prefix": "A rate entry for this prefix with the same effective date already exists",
				}})
				return
			}
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		e, err := db.GetEntry(r.Context(), h.Pool, id, entryID)
		if err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		g, _ := db.GetRateGroup(r.Context(), h.Pool, id)
		ccy := ""
		if g != nil {
			ccy = g.Currency
		}
		_ = execT(w, h.RGFragT, "entry_row", mapRGEntry(*e, ccy))
	}
}

func (h *Handlers) ShowEditEntryForm() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		eid := chi.URLParam(r, "entry_id")
		if !isHTMX(r) {
			http.Redirect(w, r, "/admin/rate-groups/"+id, http.StatusFound)
			return
		}
		e, err := db.GetEntry(r.Context(), h.Pool, id, eid)
		if err == pgx.ErrNoRows {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		desc := ""
		if e.Description != nil {
			desc = *e.Description
		}
		to := ""
		if e.EffectiveTo != nil {
			to = *e.EffectiveTo
		}
		_ = execT(w, h.RGFragT, "entry_edit_row", struct {
			RateGroupID, EntryID, CSRFToken, Prefix, Description, RatePerSMS, EffectiveFrom, EffectiveTo string
			Errors                                                                                       map[string]string
		}{id, eid, csrf.Token(r), e.Prefix, desc, e.RatePerSMS, e.EffectiveFrom, to, nil})
	}
}

func (h *Handlers) UpdateRateEntry() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		eid := chi.URLParam(r, "entry_id")
		_ = r.ParseForm()
		prefix, desc, rate, from, to, errs := parseRateEntryForm(r)
		if len(errs) == 0 {
			if dup, _ := db.ExistsEntryKey(r.Context(), h.Pool, id, prefix, from, &eid); dup {
				errs["prefix"] = "A rate entry for this prefix with the same effective date already exists"
			}
		}
		if len(errs) > 0 {
			w.WriteHeader(http.StatusUnprocessableEntity)
			_ = execT(w, h.RGFragT, "entry_edit_row", struct {
				RateGroupID, EntryID, CSRFToken, Prefix, Description, RatePerSMS, EffectiveFrom, EffectiveTo string
				Errors                                                                                       map[string]string
			}{id, eid, csrf.Token(r), prefix, desc, rate, r.FormValue("effective_from"), r.FormValue("effective_to"), errs})
			return
		}
		err := db.UpdateEntry(r.Context(), h.Pool, id, eid, db.UpsertRateEntryParams{
			Prefix: prefix, Description: strPtr(desc), RatePerSMS: rate, EffectiveFrom: from, EffectiveTo: to,
		})
		if err == pgx.ErrNoRows {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			if errors.Is(err, db.ErrDuplicateRateEntry) {
				w.WriteHeader(http.StatusUnprocessableEntity)
				_ = execT(w, h.RGFragT, "entry_edit_row", struct {
					RateGroupID, EntryID, CSRFToken, Prefix, Description, RatePerSMS, EffectiveFrom, EffectiveTo string
					Errors                                                                                       map[string]string
				}{id, eid, csrf.Token(r), prefix, desc, rate, r.FormValue("effective_from"), r.FormValue("effective_to"), map[string]string{
					"prefix": "A rate entry for this prefix with the same effective date already exists",
				}})
				return
			}
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		e, e2 := db.GetEntry(r.Context(), h.Pool, id, eid)
		if e2 != nil {
			ServerError(w, r, e2, h.Log, h.T500)
			return
		}
		g, _ := db.GetRateGroup(r.Context(), h.Pool, id)
		ccy := ""
		if g != nil {
			ccy = g.Currency
		}
		_ = execT(w, h.RGFragT, "entry_row", mapRGEntry(*e, ccy))
	}
}

func (h *Handlers) GetRateEntryRowFragment() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		eid := chi.URLParam(r, "entry_id")
		e, err := db.GetEntry(r.Context(), h.Pool, id, eid)
		if err == pgx.ErrNoRows {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		g, _ := db.GetRateGroup(r.Context(), h.Pool, id)
		ccy := ""
		if g != nil {
			ccy = g.Currency
		}
		_ = execT(w, h.RGFragT, "entry_row", mapRGEntry(*e, ccy))
	}
}

func (h *Handlers) DeleteRateEntry() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		eid := chi.URLParam(r, "entry_id")
		entry, _ := db.GetEntry(r.Context(), h.Pool, id, eid)
		if entry != nil && entry.Prefix == "*" {
			n, _ := db.CountCatchAllEntries(r.Context(), h.Pool, id)
			if n <= 1 {
				w.Header().Set("HX-Trigger", "minismsWarnCatchAllLast")
			}
		}
		_, err := db.DeleteEntry(r.Context(), h.Pool, id, eid)
		if err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		w.WriteHeader(http.StatusOK)
	}
}
