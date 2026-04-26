package web

import (
	"html/template"
	"net/http"
	"net/mail"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/gorilla/csrf"
	"github.com/jackc/pgx/v5"

	"github.com/minisms/minisms"
	"github.com/minisms/minisms/internal/db"
)

var reClientCurrency = regexp.MustCompile(`^[A-Z]{3}$`)

type ClientListPage struct {
	Title, CurrentPath, CSRFToken string
	Flash                         *Flash
	Rows                          []db.ClientListRow
	HasRows                       bool
}

type ClientDetailPage struct {
	Title, CurrentPath, CSRFToken string
	Flash                         *Flash
	Client                        *db.Client
	RateGroups                    []db.RateGroupOption
	RoutingGroups                 []db.RoutingGroupListRow
}

func activeRoutingOptions(rows []db.RoutingGroupListRow) []db.RoutingGroupListRow {
	out := make([]db.RoutingGroupListRow, 0, len(rows))
	for _, r := range rows {
		if r.Status == "active" {
			out = append(out, r)
		}
	}
	return out
}

func validateClientForm(r *http.Request, clientID string, creating bool, pool interface {
}) (db.UpsertClientParams, map[string]string) {
	_ = pool
	p := db.UpsertClientParams{
		Name:             strings.TrimSpace(r.FormValue("name")),
		Email:            strings.TrimSpace(r.FormValue("email")),
		Status:           strings.TrimSpace(r.FormValue("status")),
		Currency:         strings.ToUpper(strings.TrimSpace(r.FormValue("currency"))),
		Notes:            strPtr(r.FormValue("notes")),
		DLRWebhookURL:    strPtr(r.FormValue("dlr_webhook_url")),
		DLRWebhookSecret: nil,
	}
	dlrSecretRaw := strings.TrimSpace(r.FormValue("dlr_webhook_secret"))
	if dlrSecretRaw != "" {
		p.DLRWebhookSecret = &dlrSecretRaw
	}
	if s := strings.TrimSpace(r.FormValue("rate_group_id")); s != "" {
		p.RateGroupID = &s
	}
	if s := strings.TrimSpace(r.FormValue("routing_group_id")); s != "" {
		p.RoutingGroupID = &s
	}
	errs := map[string]string{}
	if p.Name == "" {
		errs["name"] = "Name is required"
	}
	if p.Email == "" {
		errs["email"] = "Email is required"
	} else if _, e := mail.ParseAddress(p.Email); e != nil {
		errs["email"] = "Invalid email format"
	}
	if p.Status != "active" && p.Status != "suspended" && p.Status != "disabled" {
		errs["status"] = "Invalid status"
	}
	if !reClientCurrency.MatchString(p.Currency) {
		errs["currency"] = "Currency must be 3 uppercase letters"
	}
	if p.RateGroupID != nil {
		if _, e := uuid.Parse(*p.RateGroupID); e != nil {
			errs["rate_group_id"] = "Invalid rate group"
		}
	}
	if p.RoutingGroupID != nil {
		if _, e := uuid.Parse(*p.RoutingGroupID); e != nil {
			errs["routing_group_id"] = "Invalid routing group"
		}
	}
	if p.DLRWebhookURL != nil {
		u, e := url.Parse(strings.TrimSpace(*p.DLRWebhookURL))
		if e != nil || !strings.EqualFold(u.Scheme, "https") || strings.TrimSpace(u.Host) == "" {
			errs["dlr_webhook_url"] = "DLR webhook URL must be a valid https:// URL"
		}
	}
	if p.DLRWebhookSecret != nil && len(*p.DLRWebhookSecret) > 256 {
		errs["dlr_webhook_secret"] = "DLR webhook secret must be at most 256 characters"
	}
	_ = clientID
	_ = creating
	return p, errs
}

func maskedSecret(s *string) string {
	if s == nil || strings.TrimSpace(*s) == "" {
		return ""
	}
	raw := strings.TrimSpace(*s)
	if len(raw) <= 4 {
		return strings.Repeat("•", len(raw))
	}
	return strings.Repeat("•", len(raw)-4) + raw[len(raw)-4:]
}

func (h *Handlers) ListClients() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := db.ListClients(r.Context(), h.Pool)
		if err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		_ = execT(w, h.CLIListT, "base", ClientListPage{
			Title: "Clients", CurrentPath: "/admin/clients", CSRFToken: csrf.Token(r),
			Flash: GetFlash(w, r, "/", h.Config.SecretKey, h.Config.IsProduction()),
			Rows:  rows, HasRows: len(rows) > 0,
		})
	}
}

func (h *Handlers) ShowAddClientForm() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !isHTMX(r) {
			http.Redirect(w, r, "/admin/clients", http.StatusFound)
			return
		}
		rg, _ := db.ListRateGroupsIDName(r.Context(), h.Pool)
		rog, _ := db.ListRoutingGroups(r.Context(), h.Pool)
		_ = execT(w, h.CLIFragT, "client_add_form_row", struct {
			CSRFToken, Name, Email, Status, Currency, RateGroupID, RoutingGroupID, Notes string
			RateGroups                                                                   []db.RateGroupOption
			Currencies                                                                   []db.Currency
			RoutingGroups                                                                []db.RoutingGroupListRow
			Errors                                                                       map[string]string
		}{csrf.Token(r), "", "", "active", "GBP", "", "", "", rg, mustCurrencies(r.Context(), h.Pool), activeRoutingOptions(rog), nil})
	}
}

func (h *Handlers) CreateClient() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		p, errs := validateClientForm(r, "", true, h.Pool)
		if p.DLRWebhookSecret != nil && strings.TrimSpace(*p.DLRWebhookSecret) != "" {
			enc, err := db.EncryptValue(h.Config.SecretKey, *p.DLRWebhookSecret)
			if err != nil {
				errs["dlr_webhook_secret"] = "Failed to encrypt DLR webhook secret"
			} else {
				p.DLRWebhookSecret = &enc
			}
		}
		if len(errs) == 0 && p.RateGroupID != nil {
			ccy, e := db.RateGroupCurrency(r.Context(), h.Pool, *p.RateGroupID)
			if e != nil || ccy == nil {
				errs["rate_group_id"] = "Rate group not found"
			}
		}
		if len(errs) == 0 && p.RoutingGroupID != nil {
			ok, _ := db.RoutingGroupActiveExists(r.Context(), h.Pool, *p.RoutingGroupID)
			if !ok {
				errs["routing_group_id"] = "Routing group must exist and be active"
			}
		}
		if len(errs) > 0 {
			rg, _ := db.ListRateGroupsIDName(r.Context(), h.Pool)
			rog, _ := db.ListRoutingGroups(r.Context(), h.Pool)
			w.WriteHeader(http.StatusUnprocessableEntity)
			_ = execT(w, h.CLIFragT, "client_add_form_row", struct {
				CSRFToken, Name, Email, Status, Currency, RateGroupID, RoutingGroupID, Notes string
				RateGroups                                                                   []db.RateGroupOption
				Currencies                                                                   []db.Currency
				RoutingGroups                                                                []db.RoutingGroupListRow
				Errors                                                                       map[string]string
			}{csrf.Token(r), p.Name, p.Email, p.Status, p.Currency, derefS(p.RateGroupID), derefS(p.RoutingGroupID), derefS(p.Notes), rg, mustCurrencies(r.Context(), h.Pool), activeRoutingOptions(rog), errs})
			return
		}
		id, err := db.CreateClient(r.Context(), h.Pool, p)
		if err != nil {
			if errorsIs(err, db.ErrDuplicateClientEmail) {
				errs["email"] = "Email already in use"
				rg, _ := db.ListRateGroupsIDName(r.Context(), h.Pool)
				rog, _ := db.ListRoutingGroups(r.Context(), h.Pool)
				w.WriteHeader(http.StatusUnprocessableEntity)
				_ = execT(w, h.CLIFragT, "client_add_form_row", struct {
					CSRFToken, Name, Email, Status, Currency, RateGroupID, RoutingGroupID, Notes string
					RateGroups                                                                   []db.RateGroupOption
					Currencies                                                                   []db.Currency
					RoutingGroups                                                                []db.RoutingGroupListRow
					Errors                                                                       map[string]string
				}{csrf.Token(r), p.Name, p.Email, p.Status, p.Currency, derefS(p.RateGroupID), derefS(p.RoutingGroupID), derefS(p.Notes), rg, mustCurrencies(r.Context(), h.Pool), activeRoutingOptions(rog), errs})
				return
			}
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		rows, _ := db.ListClients(r.Context(), h.Pool)
		for _, x := range rows {
			if x.ClientID == id {
				_ = execT(w, h.CLIFragT, "client_row", x)
				return
			}
		}
		http.NotFound(w, r)
	}
}

func errorsIs(err, target error) bool {
	return err != nil && target != nil && strings.Contains(err.Error(), target.Error())
}

func derefS(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func (h *Handlers) ShowEditClientForm() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		if !isHTMX(r) {
			http.Redirect(w, r, "/admin/clients/"+id, http.StatusFound)
			return
		}
		c, err := db.GetClient(r.Context(), h.Pool, id)
		if err == pgx.ErrNoRows {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		rg, _ := db.ListRateGroupsIDName(r.Context(), h.Pool)
		rog, _ := db.ListRoutingGroups(r.Context(), h.Pool)
		_ = execT(w, h.CLIFragT, "client_edit_form_row", struct {
			ClientID, CSRFToken, Name, Email, Status, Currency, RateGroupID, RoutingGroupID, Notes string
			RateGroups                                                                             []db.RateGroupOption
			Currencies                                                                             []db.Currency
			RoutingGroups                                                                          []db.RoutingGroupListRow
			ConversionFactor                                                                       string
			Errors                                                                                 map[string]string
		}{
			c.ClientID, csrf.Token(r), c.Name, c.Email, c.Status, c.Currency, derefS(c.RateGroupID), derefS(c.RoutingGroupID), derefS(c.Notes),
			rg, mustCurrencies(r.Context(), h.Pool), activeRoutingOptions(rog), "", nil,
		})
	}
}

func (h *Handlers) GetClientRowFragment() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		rows, err := db.ListClients(r.Context(), h.Pool)
		if err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		for _, x := range rows {
			if x.ClientID == id {
				_ = execT(w, h.CLIFragT, "client_row", x)
				return
			}
		}
		http.NotFound(w, r)
	}
}

func (h *Handlers) ShowClient() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		c, err := db.GetClient(r.Context(), h.Pool, id)
		if err == pgx.ErrNoRows {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		rg, _ := db.ListRateGroupsIDName(r.Context(), h.Pool)
		rog, _ := db.ListRoutingGroups(r.Context(), h.Pool)
		_ = execT(w, h.CLIDetT, "base", ClientDetailPage{
			Title: "Client", CurrentPath: "/admin/clients", CSRFToken: csrf.Token(r),
			Flash:  GetFlash(w, r, "/", h.Config.SecretKey, h.Config.IsProduction()),
			Client: c, RateGroups: rg, RoutingGroups: activeRoutingOptions(rog),
		})
	}
}

func (h *Handlers) GetClientInfoPanel() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		c, err := db.GetClient(r.Context(), h.Pool, id)
		if err == pgx.ErrNoRows {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		rg, _ := db.ListRateGroupsIDName(r.Context(), h.Pool)
		rog, _ := db.ListRoutingGroups(r.Context(), h.Pool)
		var shownSecret string
		if c != nil && c.DLRWebhookSecret != nil {
			if dec, err := db.DecryptValue(h.Config.SecretKey, *c.DLRWebhookSecret); err == nil {
				shownSecret = maskedSecret(&dec)
			}
		}
		_ = execT(w, h.CLIFragT, "client_info_form", struct {
			Client           *db.Client
			CSRFToken        string
			RateGroups       []db.RateGroupOption
			Currencies       []db.Currency
			RoutingGroups    []db.RoutingGroupListRow
			RateGroupSel     string
			RoutingGroupSel  string
			ConversionFactor string
			Success          string
			Errors           map[string]string
			DLRSecretMasked  string
		}{c, csrf.Token(r), rg, mustCurrencies(r.Context(), h.Pool), activeRoutingOptions(rog), derefS(c.RateGroupID), derefS(c.RoutingGroupID), "", "", nil, shownSecret})
	}
}

func (h *Handlers) UpdateClient() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		hxTarget := r.Header.Get("HX-Target")
		isListEdit := strings.HasPrefix(hxTarget, "client-row-")
		_ = r.ParseForm()
		p, errs := validateClientForm(r, id, false, h.Pool)
		existing, _ := db.GetClient(r.Context(), h.Pool, id)
		if p.DLRWebhookSecret != nil && strings.TrimSpace(*p.DLRWebhookSecret) != "" {
			enc, err := db.EncryptValue(h.Config.SecretKey, *p.DLRWebhookSecret)
			if err != nil {
				errs["dlr_webhook_secret"] = "Failed to encrypt DLR webhook secret"
			} else {
				p.DLRWebhookSecret = &enc
			}
		} else if existing != nil {
			p.DLRWebhookSecret = existing.DLRWebhookSecret
		}
		conversionFactor := strings.TrimSpace(r.FormValue("conversion_factor"))
		needsConversion := false
		if existing != nil && existing.Currency != p.Currency {
			var hasPositiveBalance bool
			if err := h.Pool.QueryRow(r.Context(), `SELECT ($1::numeric(18,6) > 0)`, existing.Balance).Scan(&hasPositiveBalance); err != nil {
				ServerError(w, r, err, h.Log, h.T500)
				return
			}
			if hasPositiveBalance {
				needsConversion = true
				var validFactor bool
				if conversionFactor != "" {
					_ = h.Pool.QueryRow(r.Context(), `SELECT ($1::numeric(18,6) > 0)`, conversionFactor).Scan(&validFactor)
				}
				if !validFactor {
					errs["conversion_factor"] = "Required (> 0) when changing currency for accounts with balance"
				}
			}
		}
		if len(errs) == 0 && p.RateGroupID != nil {
			ccy, e := db.RateGroupCurrency(r.Context(), h.Pool, *p.RateGroupID)
			if e != nil || ccy == nil {
				errs["rate_group_id"] = "Rate group not found"
			}
		}
		if len(errs) == 0 && p.RoutingGroupID != nil {
			ok, _ := db.RoutingGroupActiveExists(r.Context(), h.Pool, *p.RoutingGroupID)
			if !ok {
				errs["routing_group_id"] = "Routing group must exist and be active"
			}
		}
		if len(errs) == 0 {
			if err := db.UpdateClient(r.Context(), h.Pool, id, p); err != nil {
				if errorsIs(err, db.ErrDuplicateClientEmail) {
					errs["email"] = "Email already in use"
				} else if err == pgx.ErrNoRows {
					http.NotFound(w, r)
					return
				} else {
					ServerError(w, r, err, h.Log, h.T500)
					return
				}
			}
		}
		if len(errs) == 0 && needsConversion {
			var newBalance string
			if err := h.Pool.QueryRow(r.Context(), `SELECT ($1::numeric(18,6) * $2::numeric(18,6))::numeric(18,6)::text`, existing.Balance, conversionFactor).Scan(&newBalance); err != nil {
				ServerError(w, r, err, h.Log, h.T500)
				return
			}
			if _, err := h.Pool.Exec(r.Context(), `UPDATE clients SET balance = $1::numeric(18,6), updated_at = now() WHERE client_id = $2::uuid`, newBalance, id); err != nil {
				ServerError(w, r, err, h.Log, h.T500)
				return
			}
		}

		if isListEdit {
			if len(errs) > 0 {
				rg, _ := db.ListRateGroupsIDName(r.Context(), h.Pool)
				rog, _ := db.ListRoutingGroups(r.Context(), h.Pool)
				w.WriteHeader(http.StatusUnprocessableEntity)
				_ = execT(w, h.CLIFragT, "client_edit_form_row", struct {
					ClientID, CSRFToken, Name, Email, Status, Currency, RateGroupID, RoutingGroupID, Notes string
					RateGroups                                                                             []db.RateGroupOption
					Currencies                                                                             []db.Currency
					RoutingGroups                                                                          []db.RoutingGroupListRow
					ConversionFactor                                                                       string
					Errors                                                                                 map[string]string
				}{
					id, csrf.Token(r), p.Name, p.Email, p.Status, p.Currency, derefS(p.RateGroupID), derefS(p.RoutingGroupID), derefS(p.Notes),
					rg, mustCurrencies(r.Context(), h.Pool), activeRoutingOptions(rog), conversionFactor, errs,
				})
				return
			}
			rows, _ := db.ListClients(r.Context(), h.Pool)
			for _, x := range rows {
				if x.ClientID == id {
					_ = execT(w, h.CLIFragT, "client_row", x)
					return
				}
			}
			http.NotFound(w, r)
			return
		}

		c, _ := db.GetClient(r.Context(), h.Pool, id)
		rg, _ := db.ListRateGroupsIDName(r.Context(), h.Pool)
		rog, _ := db.ListRoutingGroups(r.Context(), h.Pool)
		if len(errs) > 0 {
			w.WriteHeader(http.StatusUnprocessableEntity)
		}
		success := ""
		if len(errs) == 0 {
			success = "Client updated"
		}
		var shownSecret string
		if c != nil && c.DLRWebhookSecret != nil {
			if dec, err := db.DecryptValue(h.Config.SecretKey, *c.DLRWebhookSecret); err == nil {
				shownSecret = maskedSecret(&dec)
			}
		}
		_ = execT(w, h.CLIFragT, "client_info_form", struct {
			Client           *db.Client
			CSRFToken        string
			RateGroups       []db.RateGroupOption
			Currencies       []db.Currency
			RoutingGroups    []db.RoutingGroupListRow
			RateGroupSel     string
			RoutingGroupSel  string
			ConversionFactor string
			Success          string
			Errors           map[string]string
			DLRSecretMasked  string
		}{c, csrf.Token(r), rg, mustCurrencies(r.Context(), h.Pool), activeRoutingOptions(rog), derefS(c.RateGroupID), derefS(c.RoutingGroupID), conversionFactor, success, errs, shownSecret})
	}
}

func (h *Handlers) ToggleClientStatus() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		_, err := db.ToggleClientStatus(r.Context(), h.Pool, id)
		if err == pgx.ErrNoRows {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		rows, _ := db.ListClients(r.Context(), h.Pool)
		for _, x := range rows {
			if x.ClientID == id {
				_ = execT(w, h.CLIFragT, "client_row", x)
				return
			}
		}
		http.NotFound(w, r)
	}
}

func (h *Handlers) ListClientLedger() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		c, err := db.GetClient(r.Context(), h.Pool, id)
		if err != nil {
			if err == pgx.ErrNoRows {
				http.NotFound(w, r)
			} else {
				ServerError(w, r, err, h.Log, h.T500)
			}
			return
		}
		entries, err := db.ListClientLedger(r.Context(), h.Pool, id)
		if err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		_ = execT(w, h.CLIFragT, "client_ledger_panel", struct {
			Client     *db.Client
			Entries    []db.ClientLedgerEntry
			BalanceFmt string
			CSRFToken  string
			Errors     map[string]string
		}{c, entries, FormatBalance2dp(c.Balance, c.Currency), csrf.Token(r), nil})
	}
}

func (h *Handlers) CreditClientBalance() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		_ = r.ParseForm()
		c, err := db.GetClient(r.Context(), h.Pool, id)
		if err != nil {
			if err == pgx.ErrNoRows {
				http.NotFound(w, r)
			} else {
				ServerError(w, r, err, h.Log, h.T500)
			}
			return
		}
		amount := strings.TrimSpace(r.FormValue("amount"))
		ref := strings.TrimSpace(r.FormValue("reference"))
		notes := strPtr(r.FormValue("notes"))
		errs := map[string]string{}
		f, e := strconv.ParseFloat(amount, 64)
		if e != nil || f <= 0 || f > 999999.999999 {
			errs["amount"] = "Amount must be > 0 and <= 999999.999999"
		}
		if strings.ToUpper(strings.TrimSpace(r.FormValue("currency"))) != c.Currency {
			errs["currency"] = "Currency mismatch"
		}
		if len(errs) == 0 {
			_, err = db.CreditClientBalance(r.Context(), h.Pool, id, amount, c.Currency, ref, notes)
			if err != nil {
				ServerError(w, r, err, h.Log, h.T500)
				return
			}
		}
		c2, _ := db.GetClient(r.Context(), h.Pool, id)
		entries, _ := db.ListClientLedger(r.Context(), h.Pool, id)
		if len(errs) > 0 {
			w.WriteHeader(http.StatusUnprocessableEntity)
		} else {
			w.Header().Set("HX-Trigger", "creditAdded")
		}
		_ = execT(w, h.CLIFragT, "client_ledger_panel", struct {
			Client     *db.Client
			Entries    []db.ClientLedgerEntry
			BalanceFmt string
			CSRFToken  string
			Errors     map[string]string
		}{c2, entries, FormatBalance2dp(c2.Balance, c2.Currency), csrf.Token(r), errs})
	}
}

func (h *Handlers) GetAPIKeyPanel() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		meta, err := db.GetActiveKey(r.Context(), h.Pool, id)
		if err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		_ = execT(w, h.CLIFragT, "client_apikey_panel", struct {
			ClientID  string
			CSRFToken string
			Meta      *db.APIKeyMeta
		}{id, csrf.Token(r), meta})
	}
}

func (h *Handlers) GenerateClientAPIKey() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		raw, err := db.GenerateAPIKey(r.Context(), h.Pool, id)
		if err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		w.Header().Set("HX-Trigger", "keyGenerated")
		_ = execT(w, h.CLIFragT, "client_apikey_display", struct {
			ClientID string
			RawKey   string
		}{id, raw})
	}
}

func (h *Handlers) RevokeClientAPIKey() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		if err := db.RevokeAPIKey(r.Context(), h.Pool, id, "admin_revoked"); err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		meta, _ := db.GetActiveKey(r.Context(), h.Pool, id)
		_ = execT(w, h.CLIFragT, "client_apikey_panel", struct {
			ClientID  string
			CSRFToken string
			Meta      *db.APIKeyMeta
		}{id, csrf.Token(r), meta})
	}
}

func (h *Handlers) GetClientSenderIDsPanel() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		c, err := db.GetClient(r.Context(), h.Pool, id)
		if err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		rows, _ := db.ListClientSenderIDs(r.Context(), h.Pool, id)
		avail, _ := db.GetAvailableSenderIDsForClient(r.Context(), h.Pool, id)
		var allowAny bool
		_ = h.Pool.QueryRow(r.Context(), `SELECT allow_any_sender_id FROM clients WHERE client_id=$1::uuid`, id).Scan(&allowAny)
		t, err := template.ParseFS(minisms.TemplateFS, "templates/admin/clients/sender_ids_panel.html")
		if err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		_ = t.ExecuteTemplate(w, "client_sender_ids_panel", struct {
			ClientID  string
			CSRFToken string
			Client    *db.Client
			AllowAny  bool
			Rows      []db.ClientSenderIDRow
			Available []db.SenderID
		}{id, csrf.Token(r), c, allowAny, rows, avail})
	}
}

func (h *Handlers) AddClientSenderID() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		_ = r.ParseForm()
		sid := r.FormValue("sender_id")
		if sid != "" {
			_ = db.AddClientSenderID(r.Context(), h.Pool, id, sid)
		}
		h.GetClientSenderIDsPanel().ServeHTTP(w, r)
	}
}

func (h *Handlers) RemoveClientSenderID() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		csid := chi.URLParam(r, "cid")
		_ = db.RemoveClientSenderID(r.Context(), h.Pool, csid, id)
		h.GetClientSenderIDsPanel().ServeHTTP(w, r)
	}
}

func (h *Handlers) SetClientSenderIDDefault() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		csid := chi.URLParam(r, "cid")
		_ = db.SetClientSenderIDDefault(r.Context(), h.Pool, csid, id)
		h.GetClientSenderIDsPanel().ServeHTTP(w, r)
	}
}
