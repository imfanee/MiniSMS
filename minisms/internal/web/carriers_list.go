// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package web

import (
	"errors"
	"html/template"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/gorilla/csrf"
	"github.com/jackc/pgx/v5"

	"github.com/minisms/minisms/internal/db"
)

func isHTMX(r *http.Request) bool { return r.Header.Get("HX-Request") == "true" }

// CarrierListPage is template data for carriers list.
type CarrierListPage struct {
	AdminView
	Title, CurrentPath, CSRFToken string
	Flash                         *Flash
	RowViews                      []CarrierRowView
	HasRows                       bool
}

// CarrierRowView is one list row.
type CarrierRowView struct {
	Row          db.CarrierRow
	BalFmt       string
	Active       bool
	Interconnect string // HTTP or SMPP
}

// CarrierFormRowData is inline add/edit carrier row form.
type CarrierFormRowData struct {
	CarrierID, CSRFToken, Name, Status, Currency, RateGroupID, Notes string
	SenderIDPolicy, DefaultSenderID                                   string
	RateGroups                                                        []db.RateGroupOption
	Currencies                                                        []db.Currency
	Errors                                                            map[string]string
}

func carrierFormFromValues(id, csrf string, v map[string][]string, rg []db.RateGroupOption, ccys []db.Currency, errs map[string]string) CarrierFormRowData {
	d := CarrierFormRowData{
		CarrierID: id, CSRFToken: csrf,
		Name: strings.TrimSpace(firstVal(v, "name")),
		Status: strings.TrimSpace(firstVal(v, "status")),
		Currency: strings.TrimSpace(firstVal(v, "currency")),
		RateGroupID: strings.TrimSpace(firstVal(v, "rate_group_id")),
		Notes: strings.TrimSpace(firstVal(v, "notes")),
		SenderIDPolicy: strings.TrimSpace(firstVal(v, "sender_id_policy")),
		DefaultSenderID: strings.TrimSpace(firstVal(v, "default_sender_id_value")),
		RateGroups: rg, Currencies: ccys, Errors: errs,
	}
	if d.SenderIDPolicy == "" {
		d.SenderIDPolicy = "any"
	}
	return d
}

func carrierFormFromCarrier(c *db.CarrierFull, csrf string, rg []db.RateGroupOption, ccys []db.Currency, errs map[string]string) CarrierFormRowData {
	rgid, notes, def := "", "", ""
	if c.RateGroupID != nil {
		rgid = *c.RateGroupID
	}
	if c.Notes != nil {
		notes = *c.Notes
	}
	if c.DefaultSenderIDValue != nil {
		def = *c.DefaultSenderIDValue
	}
	policy := c.SenderIDPolicy
	if policy == "" {
		policy = "any"
	}
	return CarrierFormRowData{
		CarrierID: c.CarrierID, CSRFToken: csrf,
		Name: c.Name, Status: c.Status, Currency: c.Currency,
		RateGroupID: rgid, Notes: notes,
		SenderIDPolicy: policy, DefaultSenderID: def,
		RateGroups: rg, Currencies: ccys, Errors: errs,
	}
}

func firstVal(v map[string][]string, key string) string {
	if vals, ok := v[key]; ok && len(vals) > 0 {
		return vals[0]
	}
	return ""
}

func mapRowView(r db.CarrierRow) CarrierRowView {
	ic := "HTTP"
	if strings.EqualFold(strings.TrimSpace(r.EgressTransport), "smpp") {
		ic = "SMPP"
	}
	return CarrierRowView{
		Row:          r,
		BalFmt:       FormatBalance2dp(r.Balance, r.Currency),
		Active:       r.Status == "active",
		Interconnect: ic,
	}
}

func execT(w http.ResponseWriter, t *template.Template, name string, v any, r ...*http.Request) error {
	if t == nil {
		return errNilTemplate
	}
	if name == "base" && len(r) > 0 && r[0] != nil {
		v = withAdminView(v, r[0])
	}
	return t.ExecuteTemplate(w, name, v)
}

var errNilTemplate = errors.New("template not loaded")

// ListCarriers GET /admin/carriers
func (h *Handlers) ListCarriers() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := db.ListCarriers(r.Context(), h.Pool)
		if err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		views := make([]CarrierRowView, 0, len(rows))
		for _, x := range rows {
			views = append(views, mapRowView(x))
		}
		f := GetFlash(w, r, "/", h.Config.SecretKey, h.Config.IsProduction())
		p := CarrierListPage{
			Title: "Carriers", CurrentPath: r.URL.Path, CSRFToken: csrf.Token(r), Flash: f,
			RowViews: views, HasRows: len(views) > 0,
		}
		if err := execT(w, h.CarrListT, "base", p, r); err != nil {
			ServerError(w, r, err, h.Log, h.T500)
		}
	}
}

// ShowAddForm GET /admin/carriers/new
func (h *Handlers) ShowAddForm() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !isHTMX(r) {
			http.Redirect(w, r, "/admin/carriers", http.StatusFound)
			return
		}
		rg, err := db.ListRateGroupsIDName(r.Context(), h.Pool)
		if err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		ccys, _ := db.ListActiveCurrencies(r.Context(), h.Pool)
		d := CarrierFormRowData{CSRFToken: csrf.Token(r), Status: "active", Currency: "GBP", SenderIDPolicy: "any", RateGroups: rg, Currencies: ccys, Errors: nil}
		if err := execT(w, h.CarrFragT, "add_form_row", d); err != nil {
			ServerError(w, r, err, h.Log, h.T500)
		}
	}
}

// CreateCarrier POST /admin/carriers
func (h *Handlers) CreateCarrier() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		n, st, ccy, rg, notes, em := validateCarrierForm(r.Form)
		if em == nil {
			em = map[string]string{}
		}
		senderPolicy, defaultSID := applyCarrierSenderFields(r.Form, em)
		if len(em) == 0 && strings.TrimSpace(rg) != "" {
			if _, perr := uuid.Parse(strings.TrimSpace(rg)); perr != nil {
				em["rate_group_id"] = "Invalid rate group"
			} else if ok, _ := db.RateGroupExists(r.Context(), h.Pool, strings.TrimSpace(rg)); !ok {
				em["rate_group_id"] = "Unknown rate group"
			}
		}
		if len(em) > 0 {
			rglist, _ := db.ListRateGroupsIDName(r.Context(), h.Pool)
			ccys, _ := db.ListActiveCurrencies(r.Context(), h.Pool)
			d := carrierFormFromValues("", csrf.Token(r), r.Form, rglist, ccys, em)
			w.WriteHeader(http.StatusUnprocessableEntity)
			_ = execT(w, h.CarrFragT, "add_form_row", d)
			return
		}
		var rgp *string
		if strings.TrimSpace(rg) != "" {
			s := strings.TrimSpace(rg)
			rgp = &s
		}
		id, err := db.CreateCarrier(r.Context(), h.Pool, db.CreateCarrierParams{
			Name: n, Status: st, Currency: ccy, RateGroupID: rgp, Notes: strPtr(notes),
			SenderIDPolicy: senderPolicy, DefaultSenderIDValue: defaultSID,
		})
		if err != nil {
			if errors.Is(err, db.ErrDuplicateCarrierName) {
				rglist, _ := db.ListRateGroupsIDName(r.Context(), h.Pool)
				ccys, _ := db.ListActiveCurrencies(r.Context(), h.Pool)
				d := carrierFormFromValues("", csrf.Token(r), r.Form, rglist, ccys, map[string]string{"name": "A carrier with this name already exists"})
				w.WriteHeader(http.StatusUnprocessableEntity)
				_ = execT(w, h.CarrFragT, "add_form_row", d)
				return
			}
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		row, err := db.GetCarrierRow(r.Context(), h.Pool, id)
		if err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		h.reloadRouteCache(r.Context())
		cid, cname := id, row.Name
		h.recordAudit(r, "carrier.create", "carrier", &cid, &cname, map[string]string{"status": row.Status})
		w.Header().Set("HX-Trigger", "carrierCreated")
		_ = execT(w, h.CarrFragT, "carrier_row", mapRowView(*row))
	}
}

func strPtr(s string) *string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	return &s
}

// ShowEditForm GET /admin/carriers/{id}/edit
func (h *Handlers) ShowEditForm() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		if !isHTMX(r) {
			http.Redirect(w, r, "/admin/carriers/"+id, http.StatusFound)
			return
		}
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
		ccys, _ := db.ListActiveCurrencies(r.Context(), h.Pool)
		d := carrierFormFromCarrier(c, csrf.Token(r), rg, ccys, nil)
		if err := execT(w, h.CarrFragT, "edit_form_row", d); err != nil {
			ServerError(w, r, err, h.Log, h.T500)
		}
	}
}

// UpdateCarrier PUT /admin/carriers/{id}
func (h *Handlers) UpdateCarrier() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		_ = r.ParseForm()
		n, st, ccy, rg, notes, em := validateCarrierForm(r.Form)
		if em == nil {
			em = map[string]string{}
		}
		senderPolicy, defaultSID := applyCarrierSenderFields(r.Form, em)
		if len(em) == 0 && strings.TrimSpace(rg) != "" {
			if _, perr := uuid.Parse(strings.TrimSpace(rg)); perr != nil {
				em["rate_group_id"] = "Invalid"
			} else if ok, _ := db.RateGroupExists(r.Context(), h.Pool, strings.TrimSpace(rg)); !ok {
				em["rate_group_id"] = "Unknown"
			}
		}
		if len(em) > 0 {
			rglist, _ := db.ListRateGroupsIDName(r.Context(), h.Pool)
			ccys, _ := db.ListActiveCurrencies(r.Context(), h.Pool)
			d := carrierFormFromValues(id, csrf.Token(r), r.Form, rglist, ccys, em)
			w.WriteHeader(http.StatusUnprocessableEntity)
			_ = execT(w, h.CarrFragT, "edit_form_row", d)
			return
		}
		var rgp *string
		if strings.TrimSpace(rg) != "" {
			s := strings.TrimSpace(rg)
			rgp = &s
		}
		err := db.UpdateCarrier(r.Context(), h.Pool, id, db.UpdateCarrierParams{
			Name: n, Status: st, Currency: ccy, RateGroupID: rgp, Notes: strPtr(notes),
			SenderIDPolicy: senderPolicy, DefaultSenderIDValue: defaultSID,
		})
		if err == pgx.ErrNoRows {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			if errors.Is(err, db.ErrDuplicateCarrierName) {
				rglist, _ := db.ListRateGroupsIDName(r.Context(), h.Pool)
				ccys, _ := db.ListActiveCurrencies(r.Context(), h.Pool)
				d := carrierFormFromValues(id, csrf.Token(r), r.Form, rglist, ccys, map[string]string{"name": "A carrier with this name already exists"})
				w.WriteHeader(http.StatusUnprocessableEntity)
				_ = execT(w, h.CarrFragT, "edit_form_row", d)
				return
			}
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		row, e2 := db.GetCarrierRow(r.Context(), h.Pool, id)
		if e2 != nil {
			ServerError(w, r, e2, h.Log, h.T500)
			return
		}
		h.reloadRouteCache(r.Context())
		cid, cname := id, row.Name
		h.recordAudit(r, "carrier.update", "carrier", &cid, &cname, map[string]string{"status": row.Status})
		_ = execT(w, h.CarrFragT, "carrier_row", mapRowView(*row))
	}
}

// GetCarrierRowFragment GET /admin/carriers/{id}/row — cancel inline edit; HTMX
func (h *Handlers) GetCarrierRowFragment() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		row, err := db.GetCarrierRow(r.Context(), h.Pool, id)
		if err == pgx.ErrNoRows {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		_ = execT(w, h.CarrFragT, "carrier_row", mapRowView(*row))
	}
}

// ToggleCarrierStatus POST /admin/carriers/{id}/toggle-status
func (h *Handlers) ToggleCarrierStatus() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		_, err := db.ToggleCarrierStatus(r.Context(), h.Pool, id)
		if err == pgx.ErrNoRows {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		row, e2 := db.GetCarrierRow(r.Context(), h.Pool, id)
		if e2 != nil {
			ServerError(w, r, e2, h.Log, h.T500)
			return
		}
		h.reloadRouteCache(r.Context())
		_ = execT(w, h.CarrFragT, "carrier_row", mapRowView(*row))
	}
}
