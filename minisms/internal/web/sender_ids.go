package web

import (
	"html/template"
	"net/http"
	"regexp"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/csrf"
	"github.com/minisms/minisms"
	"github.com/minisms/minisms/internal/db"
)

type senderIDsPage struct {
	Title       string
	CurrentPath string
	CSRFToken   string
	Flash       *Flash
	Rows        []senderIDRowVM
	Form        senderIDForm
}

// senderIDRowVM is one table row for the Sender ID library list (view or edit templates).
// Row is named (not embedded) so templates can use .Row.SenderID for the UUID; a bare
// .SenderID would resolve to the whole embedded struct in text/template.
type senderIDRowVM struct {
	Row       db.SenderID
	CSRFToken string
}

type senderIDForm struct {
	Value        string
	SenderIDType string
	Description  string
	Errors       map[string]string
}

var (
	alphaSenderRe   = regexp.MustCompile(`^[A-Za-z0-9]{1,11}$`)
	numericSenderRe = regexp.MustCompile(`^[0-9]{1,15}$`)
	e164SenderRe    = regexp.MustCompile(`^\+[1-9][0-9]{6,14}$`)
)

func (h *Handlers) ListSenderIDs() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := db.ListAllSenderIDs(r.Context(), h.Pool)
		if err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		tok := csrf.Token(r)
		vms := make([]senderIDRowVM, len(rows))
		for i := range rows {
			vms[i] = senderIDRowVM{Row: rows[i], CSRFToken: tok}
		}
		p := senderIDsPage{
			Title:       "Sender IDs",
			CurrentPath: "/admin/sender-ids",
			CSRFToken:   tok,
			Flash:       GetFlash(w, r, "/", h.Config.SecretKey, h.Config.IsProduction()),
			Rows:        vms,
			Form: senderIDForm{
				SenderIDType: "alpha",
				Errors:       map[string]string{},
			},
		}
		t, err := template.ParseFS(minisms.TemplateFS,
			"templates/layout/base.html",
			"templates/layout/partials/navbar.html",
			"templates/layout/partials/flash.html",
			"templates/admin/sender_ids/list.html",
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

func (h *Handlers) CreateSenderID() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		f := senderIDForm{
			Value:        strings.TrimSpace(r.FormValue("value")),
			SenderIDType: strings.TrimSpace(r.FormValue("sender_id_type")),
			Description:  strings.TrimSpace(r.FormValue("description")),
			Errors:       map[string]string{},
		}
		if !validateSenderIDForm(&f) {
			h.renderSenderIDsWithForm(w, r, f, http.StatusBadRequest)
			return
		}
		desc := f.Description
		row, err := db.CreateSenderID(r.Context(), h.Pool, db.SenderID{
			Value:        f.Value,
			SenderIDType: f.SenderIDType,
			Description:  &desc,
			IsActive:     true,
		})
		if err != nil {
			h.renderSenderIDsWithForm(w, r, f, http.StatusBadRequest)
			return
		}
		h.renderSenderIDRowView(w, r, row)
	}
}

func (h *Handlers) UpdateSenderID() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		if err := r.ParseForm(); err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		f := senderIDForm{
			Value:        strings.TrimSpace(r.FormValue("value")),
			SenderIDType: strings.TrimSpace(r.FormValue("sender_id_type")),
			Description:  strings.TrimSpace(r.FormValue("description")),
			Errors:       map[string]string{},
		}
		if !validateSenderIDForm(&f) {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		desc := f.Description
		if err := db.UpdateSenderID(r.Context(), h.Pool, db.SenderID{
			SenderID:     id,
			Value:        f.Value,
			SenderIDType: f.SenderIDType,
			Description:  &desc,
		}); err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		row, err := db.GetSenderID(r.Context(), h.Pool, id)
		if err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		h.renderSenderIDRowView(w, r, row)
	}
}

func (h *Handlers) ToggleSenderIDActive() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		if err := db.ToggleSenderIDActive(r.Context(), h.Pool, id); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`<tr><td colspan="7" class="text-danger">Cannot deactivate: sender ID is referenced by clients/carriers.</td></tr>`))
			return
		}
		row, err := db.GetSenderID(r.Context(), h.Pool, id)
		if err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		h.renderSenderIDRowView(w, r, row)
	}
}

func (h *Handlers) renderSenderIDsWithForm(w http.ResponseWriter, r *http.Request, f senderIDForm, status int) {
	rows, err := db.ListAllSenderIDs(r.Context(), h.Pool)
	if err != nil {
		ServerError(w, r, err, h.Log, h.T500)
		return
	}
	tok := csrf.Token(r)
	vms := make([]senderIDRowVM, len(rows))
	for i := range rows {
		vms[i] = senderIDRowVM{Row: rows[i], CSRFToken: tok}
	}
	p := senderIDsPage{
		Title:       "Sender IDs",
		CurrentPath: "/admin/sender-ids",
		CSRFToken:   tok,
		Rows:        vms,
		Form:        f,
	}
	t, err := template.ParseFS(minisms.TemplateFS,
		"templates/layout/base.html",
		"templates/layout/partials/navbar.html",
		"templates/layout/partials/flash.html",
		"templates/admin/sender_ids/list.html",
	)
	if err != nil {
		ServerError(w, r, err, h.Log, h.T500)
		return
	}
	w.WriteHeader(status)
	_ = t.ExecuteTemplate(w, "base", p)
}

func (h *Handlers) renderSenderIDRowView(w http.ResponseWriter, r *http.Request, row *db.SenderID) {
	t, err := template.ParseFS(minisms.TemplateFS, "templates/admin/sender_ids/list.html")
	if err != nil {
		ServerError(w, r, err, h.Log, h.T500)
		return
	}
	vm := senderIDRowVM{Row: *row, CSRFToken: csrf.Token(r)}
	if err := t.ExecuteTemplate(w, "sender_id_row_view", vm); err != nil {
		ServerError(w, r, err, h.Log, h.T500)
	}
}

// GetSenderIDRowView returns a single library row in read-only mode (HTMX fragment).
func (h *Handlers) GetSenderIDRowView() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		row, err := db.GetSenderID(r.Context(), h.Pool, id)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		h.renderSenderIDRowView(w, r, row)
	}
}

// GetSenderIDRowEdit returns a single library row in edit mode (HTMX fragment).
func (h *Handlers) GetSenderIDRowEdit() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		row, err := db.GetSenderID(r.Context(), h.Pool, id)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		t, err := template.ParseFS(minisms.TemplateFS, "templates/admin/sender_ids/list.html")
		if err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		vm := senderIDRowVM{Row: *row, CSRFToken: csrf.Token(r)}
		if err := t.ExecuteTemplate(w, "sender_id_row_edit", vm); err != nil {
			ServerError(w, r, err, h.Log, h.T500)
		}
	}
}

func validateSenderIDForm(f *senderIDForm) bool {
	ok := true
	if f.SenderIDType != "alpha" && f.SenderIDType != "numeric" && f.SenderIDType != "e164" {
		f.Errors["sender_id_type"] = "Type must be alpha, numeric, or e164"
		ok = false
	}
	if strings.TrimSpace(f.Value) == "" {
		f.Errors["value"] = "Value is required"
		return false
	}
	switch f.SenderIDType {
	case "alpha":
		if !alphaSenderRe.MatchString(f.Value) {
			f.Errors["value"] = "Alpha must match ^[A-Za-z0-9]{1,11}$"
			ok = false
		}
	case "numeric":
		if !numericSenderRe.MatchString(f.Value) {
			f.Errors["value"] = "Numeric must match ^[0-9]{1,15}$"
			ok = false
		}
	case "e164":
		if !e164SenderRe.MatchString(f.Value) {
			f.Errors["value"] = "E164 must match ^\\+[1-9][0-9]{6,14}$"
			ok = false
		}
	}
	return ok
}
