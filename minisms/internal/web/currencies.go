package web

import (
	"errors"
	"html/template"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/gorilla/csrf"
	"github.com/jackc/pgx/v5"
	"github.com/minisms/minisms"
	"github.com/minisms/minisms/internal/db"
)

type currenciesPage struct {
	Title       string
	CurrentPath string
	CSRFToken   string
	Flash       *Flash
	Rows        []db.Currency
	Form        currencyForm
}

type currencyForm struct {
	Code          string
	Name          string
	Symbol        string
	DecimalPlaces string
	Errors        map[string]string
}

var currencyCodeRe = regexp.MustCompile(`^[A-Z]{3}$`)

func (h *Handlers) ListCurrencies() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := db.ListAllCurrencies(r.Context(), h.Pool)
		if err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		p := currenciesPage{
			Title:       "Currencies",
			CurrentPath: "/admin/currencies",
			CSRFToken:   csrf.Token(r),
			Flash:       GetFlash(w, r, "/", h.Config.SecretKey, h.Config.IsProduction()),
			Rows:        rows,
			Form: currencyForm{
				DecimalPlaces: "2",
				Errors:        map[string]string{},
			},
		}
		t, err := template.ParseFS(minisms.TemplateFS,
			"templates/layout/base.html",
			"templates/layout/partials/navbar.html",
			"templates/layout/partials/flash.html",
			"templates/admin/currencies/list.html",
		)
		if err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		if err := t.ExecuteTemplate(w, "base", p); err != nil {
			ServerError(w, r, err, h.Log, h.T500)
		}
	}
}

func (h *Handlers) CreateCurrency() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		f := currencyForm{
			Code:          strings.ToUpper(strings.TrimSpace(r.FormValue("code"))),
			Name:          strings.TrimSpace(r.FormValue("name")),
			Symbol:        strings.TrimSpace(r.FormValue("symbol")),
			DecimalPlaces: strings.TrimSpace(r.FormValue("decimal_places")),
			Errors:        map[string]string{},
		}
		dp, ok := validateCurrencyForm(&f, true)
		if !ok {
			h.renderCurrenciesWithForm(w, r, f, http.StatusBadRequest)
			return
		}
		err := db.CreateCurrency(r.Context(), h.Pool, db.Currency{
			Code:          f.Code,
			Name:          f.Name,
			Symbol:        f.Symbol,
			DecimalPlaces: int16(dp),
			IsActive:      true,
		})
		if err != nil {
			h.renderCurrenciesWithForm(w, r, f, http.StatusBadRequest)
			return
		}
		row, err := db.GetCurrency(r.Context(), h.Pool, f.Code)
		if err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		h.renderCurrencyRow(w, r, row)
	}
}

func (h *Handlers) UpdateCurrency() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		code := strings.ToUpper(strings.TrimSpace(r.PathValue("code")))
		if err := r.ParseForm(); err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		f := currencyForm{
			Code:          code,
			Name:          strings.TrimSpace(r.FormValue("name")),
			Symbol:        strings.TrimSpace(r.FormValue("symbol")),
			DecimalPlaces: strings.TrimSpace(r.FormValue("decimal_places")),
			Errors:        map[string]string{},
		}
		dp, ok := validateCurrencyForm(&f, false)
		if !ok {
			w.WriteHeader(http.StatusBadRequest)
			h.renderCurrencyRow(w, r, &db.Currency{
				Code:          f.Code,
				Name:          f.Name,
				Symbol:        f.Symbol,
				DecimalPlaces: int16(dp),
				IsActive:      true,
			})
			return
		}
		if err := db.UpdateCurrency(r.Context(), h.Pool, db.Currency{
			Code:          code,
			Name:          f.Name,
			Symbol:        f.Symbol,
			DecimalPlaces: int16(dp),
		}); err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		row, err := db.GetCurrency(r.Context(), h.Pool, code)
		if err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		h.renderCurrencyRow(w, r, row)
	}
}

func (h *Handlers) ToggleCurrencyActive() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		code := strings.ToUpper(strings.TrimSpace(r.PathValue("code")))
		if err := db.ToggleCurrencyActive(r.Context(), h.Pool, code); err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		row, err := db.GetCurrency(r.Context(), h.Pool, code)
		if err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		h.renderCurrencyRow(w, r, row)
	}
}

func (h *Handlers) renderCurrenciesWithForm(w http.ResponseWriter, r *http.Request, f currencyForm, status int) {
	rows, err := db.ListAllCurrencies(r.Context(), h.Pool)
	if err != nil {
		ServerError(w, r, err, h.Log, h.T500)
		return
	}
	p := currenciesPage{
		Title:       "Currencies",
		CurrentPath: "/admin/currencies",
		CSRFToken:   csrf.Token(r),
		Rows:        rows,
		Form:        f,
	}
	t, err := template.ParseFS(minisms.TemplateFS,
		"templates/layout/base.html",
		"templates/layout/partials/navbar.html",
		"templates/layout/partials/flash.html",
		"templates/admin/currencies/list.html",
	)
	if err != nil {
		ServerError(w, r, err, h.Log, h.T500)
		return
	}
	w.WriteHeader(status)
	_ = t.ExecuteTemplate(w, "base", p)
}

func (h *Handlers) renderCurrencyRow(w http.ResponseWriter, r *http.Request, row *db.Currency) {
	t, err := template.ParseFS(minisms.TemplateFS, "templates/admin/currencies/list.html")
	if err != nil {
		ServerError(w, r, err, h.Log, h.T500)
		return
	}
	if err := t.ExecuteTemplate(w, "currency_row", row); err != nil {
		ServerError(w, r, err, h.Log, h.T500)
	}
}

func validateCurrencyForm(f *currencyForm, includeCode bool) (int, bool) {
	ok := true
	if includeCode && !currencyCodeRe.MatchString(f.Code) {
		f.Errors["code"] = "Code must be exactly 3 uppercase letters"
		ok = false
	}
	if n := len([]rune(f.Name)); n < 1 || n > 100 {
		f.Errors["name"] = "Name must be 1-100 chars"
		ok = false
	}
	if n := len([]rune(f.Symbol)); n < 1 || n > 8 {
		f.Errors["symbol"] = "Symbol must be 1-8 chars"
		ok = false
	}
	dp, err := strconv.Atoi(f.DecimalPlaces)
	if err != nil || dp < 0 || dp > 6 {
		f.Errors["decimal_places"] = "Decimal places must be integer 0-6"
		ok = false
	}
	if !ok {
		return 0, false
	}
	return dp, true
}

func mapDuplicateAsBadRequest(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	return err
}
