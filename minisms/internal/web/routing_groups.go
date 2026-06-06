// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package web

import (
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/csrf"
	"github.com/jackc/pgx/v5"

	"github.com/minisms/minisms/internal/db"
)

var reRoutePrefix = regexp.MustCompile(`^(\*|[0-9]{1,15})$`)

type RoutingListPage struct {
	AdminView
	Title, CurrentPath, CSRFToken string
	Flash                         *Flash
	Rows                          []db.RoutingGroupListRow
	HasRows                       bool
}

type RouteRowView struct {
	Row            db.RouteEntryDetail
	PrefixLabel    string
	PrimaryBadge   string
	Failover1Badge string
	Failover2Badge string
}

func mapRouteRow(x db.RouteEntryDetail) RouteRowView {
	lbl := x.Prefix
	if x.Prefix == "*" {
		lbl = "* (catch-all)"
	}
	return RouteRowView{
		Row:            x,
		PrefixLabel:    lbl,
		PrimaryBadge:   carrierBadgeClass(x.PrimaryCarrierStatus, &x.PrimaryBalance),
		Failover1Badge: carrierBadgeClass(ptrOr(x.Failover1CarrierStatus, "inactive"), x.Failover1Balance),
		Failover2Badge: carrierBadgeClass(ptrOr(x.Failover2CarrierStatus, "inactive"), x.Failover2Balance),
	}
}

func ptrOr(s *string, def string) string {
	if s == nil {
		return def
	}
	return *s
}

func carrierBadgeClass(status string, balance *string) string {
	if balance != nil {
		if f, err := strconv.ParseFloat(strings.TrimSpace(*balance), 64); err == nil && f < 0 {
			return "bg-danger"
		}
	}
	if status == "active" {
		return "bg-success"
	}
	return "bg-secondary"
}

func validateRoutingGroup(name, status string) map[string]string {
	m := map[string]string{}
	if strings.TrimSpace(name) == "" {
		m["name"] = "Name is required"
	}
	if status != "active" && status != "inactive" {
		m["status"] = "Invalid status"
	}
	return m
}

func (h *Handlers) ListRoutingGroups() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := db.ListRoutingGroups(r.Context(), h.Pool)
		if err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		_ = execT(w, h.ROGListT, "base", RoutingListPage{
			Title: "Routing Groups", CurrentPath: "/admin/routing-groups", CSRFToken: csrf.Token(r),
			Flash: GetFlash(w, r, "/", h.Config.SecretKey, h.Config.IsProduction()),
			Rows:  rows, HasRows: len(rows) > 0,
		}, r)
	}
}

func (h *Handlers) ShowAddRoutingGroupForm() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !isHTMX(r) {
			http.Redirect(w, r, "/admin/routing-groups", http.StatusFound)
			return
		}
		_ = execT(w, h.ROGFragT, "rog_add_form_row", struct {
			CSRFToken, Name, Description, Status string
			Errors                               map[string]string
		}{csrf.Token(r), "", "", "active", nil})
	}
}

func (h *Handlers) CreateRoutingGroup() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		name := strings.TrimSpace(r.FormValue("name"))
		desc := strings.TrimSpace(r.FormValue("description"))
		status := strings.TrimSpace(r.FormValue("status"))
		errs := validateRoutingGroup(name, status)
		if len(errs) > 0 {
			w.WriteHeader(http.StatusUnprocessableEntity)
			_ = execT(w, h.ROGFragT, "rog_add_form_row", struct {
				CSRFToken, Name, Description, Status string
				Errors                               map[string]string
			}{csrf.Token(r), name, desc, status, errs})
			return
		}
		id, err := db.CreateRoutingGroup(r.Context(), h.Pool, db.UpsertRoutingGroupParams{
			Name: name, Description: strPtr(desc), Status: status,
		})
		if err != nil {
			if errors.Is(err, db.ErrDuplicateRoutingGroupName) {
				w.WriteHeader(http.StatusUnprocessableEntity)
				_ = execT(w, h.ROGFragT, "rog_add_form_row", struct {
					CSRFToken, Name, Description, Status string
					Errors                               map[string]string
				}{csrf.Token(r), name, desc, status, map[string]string{"name": "A routing group with this name already exists"}})
				return
			}
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		rows, e2 := db.ListRoutingGroups(r.Context(), h.Pool)
		if e2 != nil {
			ServerError(w, r, e2, h.Log, h.T500)
			return
		}
		for _, x := range rows {
			if x.RoutingGroupID == id {
				_ = execT(w, h.ROGFragT, "rog_row", x)
				return
			}
		}
		http.NotFound(w, r)
	}
}

func (h *Handlers) ShowEditRoutingGroupForm() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		if !isHTMX(r) {
			http.Redirect(w, r, "/admin/routing-groups", http.StatusFound)
			return
		}
		g, err := db.GetRoutingGroup(r.Context(), h.Pool, id)
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
		_ = execT(w, h.ROGFragT, "rog_edit_form_row", struct {
			RoutingGroupID, CSRFToken, Name, Description, Status string
			Errors                                               map[string]string
		}{g.RoutingGroupID, csrf.Token(r), g.Name, desc, g.Status, nil})
	}
}

func (h *Handlers) GetRoutingGroupRowFragment() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		rows, err := db.ListRoutingGroups(r.Context(), h.Pool)
		if err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		for _, x := range rows {
			if x.RoutingGroupID == id {
				_ = execT(w, h.ROGFragT, "rog_row", x)
				return
			}
		}
		http.NotFound(w, r)
	}
}

func (h *Handlers) UpdateRoutingGroup() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		_ = r.ParseForm()
		name := strings.TrimSpace(r.FormValue("name"))
		desc := strings.TrimSpace(r.FormValue("description"))
		status := strings.TrimSpace(r.FormValue("status"))
		errs := validateRoutingGroup(name, status)
		if len(errs) > 0 {
			w.WriteHeader(http.StatusUnprocessableEntity)
			_ = execT(w, h.ROGFragT, "rog_edit_form_row", struct {
				RoutingGroupID, CSRFToken, Name, Description, Status string
				Errors                                               map[string]string
			}{id, csrf.Token(r), name, desc, status, errs})
			return
		}
		err := db.UpdateRoutingGroup(r.Context(), h.Pool, id, db.UpsertRoutingGroupParams{
			Name: name, Description: strPtr(desc), Status: status,
		})
		if err == pgx.ErrNoRows {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			if errors.Is(err, db.ErrDuplicateRoutingGroupName) {
				w.WriteHeader(http.StatusUnprocessableEntity)
				_ = execT(w, h.ROGFragT, "rog_edit_form_row", struct {
					RoutingGroupID, CSRFToken, Name, Description, Status string
					Errors                                               map[string]string
				}{id, csrf.Token(r), name, desc, status, map[string]string{"name": "A routing group with this name already exists"}})
				return
			}
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		rows, e2 := db.ListRoutingGroups(r.Context(), h.Pool)
		if e2 != nil {
			ServerError(w, r, e2, h.Log, h.T500)
			return
		}
		for _, x := range rows {
			if x.RoutingGroupID == id {
				_ = execT(w, h.ROGFragT, "rog_row", x)
				return
			}
		}
		http.NotFound(w, r)
	}
}

func (h *Handlers) ToggleRoutingGroupStatus() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		newStatus, err := db.ToggleRoutingGroupStatus(r.Context(), h.Pool, id)
		if err == pgx.ErrNoRows {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		if newStatus == "inactive" {
			if n, _ := db.CountRoutingGroupClientRefs(r.Context(), h.Pool, id); n > 0 {
				w.Header().Set("HX-Trigger", "routingGroupDeactivatedWithClients")
			}
		}
		rows, e2 := db.ListRoutingGroups(r.Context(), h.Pool)
		if e2 != nil {
			ServerError(w, r, e2, h.Log, h.T500)
			return
		}
		for _, x := range rows {
			if x.RoutingGroupID == id {
				_ = execT(w, h.ROGFragT, "rog_row", x)
				return
			}
		}
		http.NotFound(w, r)
	}
}

func (h *Handlers) ShowRoutingGroupDetail() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		g, err := db.GetRoutingGroup(r.Context(), h.Pool, id)
		if err == pgx.ErrNoRows {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		carriers, err := db.ListCarrierChoices(r.Context(), h.Pool)
		if err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		_ = execT(w, h.ROGDetT, "base", struct {
			AdminView
			Title, CurrentPath, CSRFToken string
			Flash                         *Flash
			Group                         *db.RoutingGroup
			Carriers                      []db.CarrierChoice
		}{
			Title: "Routing Group", CurrentPath: "/admin/routing-groups", CSRFToken: csrf.Token(r),
			Flash: GetFlash(w, r, "/", h.Config.SecretKey, h.Config.IsProduction()),
			Group: g, Carriers: carriers,
		}, r)
	}
}

func (h *Handlers) ListRouteEntries() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		entries, err := db.ListRouteEntries(r.Context(), h.Pool, id)
		if err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		views := make([]RouteRowView, 0, len(entries))
		for _, x := range entries {
			views = append(views, mapRouteRow(x))
		}
		_ = execT(w, h.ROGFragT, "route_list", struct {
			RoutingGroupID string
			Rows           []RouteRowView
		}{id, views})
	}
}

func validateRouteEntryForm(prefix, status string, priority int, primary string, f1, f2 *string) map[string]string {
	m := map[string]string{}
	if !reRoutePrefix.MatchString(prefix) {
		m["prefix"] = "Prefix must be numeric or *"
	}
	if status != "active" && status != "inactive" {
		m["status"] = "Invalid status"
	}
	if primary == "" {
		m["primary_carrier_id"] = "Primary carrier is required"
	}
	if priority < 0 {
		m["priority"] = "Priority must be >= 0"
	}
	if f1 != nil && *f1 == primary {
		m["failover1_carrier_id"] = "Failover 1 must be different from primary"
	}
	if f2 != nil {
		if f1 == nil || *f1 == "" {
			m["failover2_carrier_id"] = "Failover 2 requires Failover 1"
		}
		if *f2 == primary {
			m["failover2_carrier_id"] = "Failover 2 must be different from primary"
		}
		if f1 != nil && *f1 == *f2 {
			m["failover2_carrier_id"] = "Failover 2 must be different from Failover 1"
		}
	}
	return m
}

func (h *Handlers) ShowAddRouteForm() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		if !isHTMX(r) {
			http.Redirect(w, r, "/admin/routing-groups/"+id, http.StatusFound)
			return
		}
		carriers, err := db.ListCarrierChoices(r.Context(), h.Pool)
		if err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		_ = execT(w, h.ROGFragT, "route_add_form_row", struct {
			RoutingGroupID, CSRFToken, Prefix, Description, Priority, Status, PrimaryCarrierID, Failover1CarrierID, Failover2CarrierID string
			Carriers                                                                                                                   []db.CarrierChoice
			Errors                                                                                                                     map[string]string
		}{id, csrf.Token(r), "", "", "100", "active", "", "", "", carriers, nil})
	}
}

func (h *Handlers) CreateRouteEntry() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		_ = r.ParseForm()
		prefix := strings.TrimSpace(r.FormValue("prefix"))
		desc := strings.TrimSpace(r.FormValue("description"))
		status := strings.TrimSpace(r.FormValue("status"))
		primary := strings.TrimSpace(r.FormValue("primary_carrier_id"))
		var f1, f2 *string
		if s := strings.TrimSpace(r.FormValue("failover1_carrier_id")); s != "" {
			f1 = &s
		}
		if s := strings.TrimSpace(r.FormValue("failover2_carrier_id")); s != "" {
			f2 = &s
		}
		p, _ := strconv.Atoi(strings.TrimSpace(r.FormValue("priority")))
		errs := validateRouteEntryForm(prefix, status, p, primary, f1, f2)
		if len(errs) == 0 {
			if c, e := db.GetCarrierByID(r.Context(), h.Pool, primary); e != nil || c.Status != "active" {
				errs["primary_carrier_id"] = "Primary carrier must exist and be active"
			}
			if f1 != nil {
				if _, e := db.GetCarrierByID(r.Context(), h.Pool, *f1); e != nil {
					errs["failover1_carrier_id"] = "Failover 1 carrier must exist"
				}
			}
			if f2 != nil {
				if _, e := db.GetCarrierByID(r.Context(), h.Pool, *f2); e != nil {
					errs["failover2_carrier_id"] = "Failover 2 carrier must exist"
				}
			}
			if ok, _ := db.ExistsRoutePrefix(r.Context(), h.Pool, id, prefix, nil); ok {
				errs["prefix"] = "A route for this prefix already exists"
			}
		}
		if len(errs) > 0 {
			carriers, _ := db.ListCarrierChoices(r.Context(), h.Pool)
			w.WriteHeader(http.StatusUnprocessableEntity)
			_ = execT(w, h.ROGFragT, "route_add_form_row", struct {
				RoutingGroupID, CSRFToken, Prefix, Description, Priority, Status, PrimaryCarrierID, Failover1CarrierID, Failover2CarrierID string
				Carriers                                                                                                                   []db.CarrierChoice
				Errors                                                                                                                     map[string]string
			}{id, csrf.Token(r), prefix, desc, r.FormValue("priority"), status, primary, r.FormValue("failover1_carrier_id"), r.FormValue("failover2_carrier_id"), carriers, errs})
			return
		}
		routeID, err := db.CreateRouteEntry(r.Context(), h.Pool, id, db.UpsertRouteEntryParams{
			Prefix: prefix, Description: strPtr(desc), Priority: p, Status: status, PrimaryCarrierID: primary, Failover1CarrierID: f1, Failover2CarrierID: f2,
		})
		if err != nil {
			if errors.Is(err, db.ErrDuplicateRoutePrefix) {
				carriers, _ := db.ListCarrierChoices(r.Context(), h.Pool)
				w.WriteHeader(http.StatusUnprocessableEntity)
				_ = execT(w, h.ROGFragT, "route_add_form_row", struct {
					RoutingGroupID, CSRFToken, Prefix, Description, Priority, Status, PrimaryCarrierID, Failover1CarrierID, Failover2CarrierID string
					Carriers                                                                                                                   []db.CarrierChoice
					Errors                                                                                                                     map[string]string
				}{id, csrf.Token(r), prefix, desc, r.FormValue("priority"), status, primary, r.FormValue("failover1_carrier_id"), r.FormValue("failover2_carrier_id"), carriers, map[string]string{"prefix": "A route for this prefix already exists"}})
				return
			}
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		detail, e2 := db.GetRouteEntryDetail(r.Context(), h.Pool, id, routeID)
		if e2 != nil {
			ServerError(w, r, e2, h.Log, h.T500)
			return
		}
		h.reloadRouteCache(r.Context())
		_ = execT(w, h.ROGFragT, "route_row", mapRouteRow(*detail))
	}
}

func (h *Handlers) ShowEditRouteForm() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		routeID := chi.URLParam(r, "route_id")
		if !isHTMX(r) {
			http.Redirect(w, r, "/admin/routing-groups/"+id, http.StatusFound)
			return
		}
		row, err := db.GetRouteEntryDetail(r.Context(), h.Pool, id, routeID)
		if err == pgx.ErrNoRows {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		carriers, err := db.ListCarrierChoices(r.Context(), h.Pool)
		if err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		desc := ""
		if row.Description != nil {
			desc = *row.Description
		}
		f1, f2 := "", ""
		if row.Failover1CarrierID != nil {
			f1 = *row.Failover1CarrierID
		}
		if row.Failover2CarrierID != nil {
			f2 = *row.Failover2CarrierID
		}
		_ = execT(w, h.ROGFragT, "route_edit_form_row", struct {
			RoutingGroupID, RouteID, CSRFToken, Prefix, Description, Priority, Status, PrimaryCarrierID, Failover1CarrierID, Failover2CarrierID string
			Carriers                                                                                                                            []db.CarrierChoice
			Errors                                                                                                                              map[string]string
		}{id, routeID, csrf.Token(r), row.Prefix, desc, strconv.Itoa(row.Priority), row.Status, row.PrimaryCarrierID, f1, f2, carriers, nil})
	}
}

func (h *Handlers) GetRouteRowFragment() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		routeID := chi.URLParam(r, "route_id")
		row, err := db.GetRouteEntryDetail(r.Context(), h.Pool, id, routeID)
		if err == pgx.ErrNoRows {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		_ = execT(w, h.ROGFragT, "route_row", mapRouteRow(*row))
	}
}

func (h *Handlers) UpdateRouteEntry() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		routeID := chi.URLParam(r, "route_id")
		_ = r.ParseForm()
		prefix := strings.TrimSpace(r.FormValue("prefix"))
		desc := strings.TrimSpace(r.FormValue("description"))
		status := strings.TrimSpace(r.FormValue("status"))
		primary := strings.TrimSpace(r.FormValue("primary_carrier_id"))
		var f1, f2 *string
		if s := strings.TrimSpace(r.FormValue("failover1_carrier_id")); s != "" {
			f1 = &s
		}
		if s := strings.TrimSpace(r.FormValue("failover2_carrier_id")); s != "" {
			f2 = &s
		}
		p, _ := strconv.Atoi(strings.TrimSpace(r.FormValue("priority")))
		errs := validateRouteEntryForm(prefix, status, p, primary, f1, f2)
		if len(errs) == 0 {
			if c, e := db.GetCarrierByID(r.Context(), h.Pool, primary); e != nil || c.Status != "active" {
				errs["primary_carrier_id"] = "Primary carrier must exist and be active"
			}
			if ok, _ := db.ExistsRoutePrefix(r.Context(), h.Pool, id, prefix, &routeID); ok {
				errs["prefix"] = "A route for this prefix already exists"
			}
		}
		if len(errs) > 0 {
			carriers, _ := db.ListCarrierChoices(r.Context(), h.Pool)
			w.WriteHeader(http.StatusUnprocessableEntity)
			_ = execT(w, h.ROGFragT, "route_edit_form_row", struct {
				RoutingGroupID, RouteID, CSRFToken, Prefix, Description, Priority, Status, PrimaryCarrierID, Failover1CarrierID, Failover2CarrierID string
				Carriers                                                                                                                            []db.CarrierChoice
				Errors                                                                                                                              map[string]string
			}{id, routeID, csrf.Token(r), prefix, desc, r.FormValue("priority"), status, primary, r.FormValue("failover1_carrier_id"), r.FormValue("failover2_carrier_id"), carriers, errs})
			return
		}
		err := db.UpdateRouteEntry(r.Context(), h.Pool, id, routeID, db.UpsertRouteEntryParams{
			Prefix: prefix, Description: strPtr(desc), Priority: p, Status: status, PrimaryCarrierID: primary, Failover1CarrierID: f1, Failover2CarrierID: f2,
		})
		if err == pgx.ErrNoRows {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		row, e2 := db.GetRouteEntryDetail(r.Context(), h.Pool, id, routeID)
		if e2 != nil {
			ServerError(w, r, e2, h.Log, h.T500)
			return
		}
		h.reloadRouteCache(r.Context())
		_ = execT(w, h.ROGFragT, "route_row", mapRouteRow(*row))
	}
}

func (h *Handlers) DeleteRouteEntry() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		routeID := chi.URLParam(r, "route_id")
		_, err := db.DeleteRouteEntry(r.Context(), h.Pool, id, routeID)
		if err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		h.reloadRouteCache(r.Context())
		w.WriteHeader(http.StatusOK)
	}
}
