// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package web

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/csrf"
	"github.com/minisms/minisms/internal/db"
)

type interconnectPanelData struct {
	CarrierID   string
	CSRFToken   string
	Carrier     *db.CarrierFull
	Interconnect string // http | smpp
	Success     string
	Errors      map[string]string
}

func interconnectType(c *db.CarrierFull) string {
	if c == nil {
		return "http"
	}
	if strings.ToLower(strings.TrimSpace(c.EgressTransport)) == "smpp" {
		return "smpp"
	}
	return "http"
}

func (h *Handlers) GetCarrierInterconnect() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cid := chi.URLParam(r, "id")
		c, err := db.GetCarrier(r.Context(), h.Pool, cid)
		if err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		_ = execT(w, h.CarrFragT, "interconnect_panel", interconnectPanelData{
			CarrierID: cid, CSRFToken: csrf.Token(r), Carrier: c, Interconnect: interconnectType(c),
		})
	}
}

func (h *Handlers) SaveCarrierInterconnect() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cid := chi.URLParam(r, "id")
		_ = r.ParseForm()
		transport, ok := validateInterconnectType(r.FormValue("interconnect"))
		errs := map[string]string{}
		if !ok {
			errs["interconnect"] = "Select HTTP or SMPP"
		}
		if len(errs) == 0 {
			if err := db.UpdateCarrierInterconnect(r.Context(), h.Pool, cid, transport); err != nil {
				ServerError(w, r, err, h.Log, h.T500)
				return
			}
			h.reloadRouteCache(r.Context())
		}
		c, _ := db.GetCarrier(r.Context(), h.Pool, cid)
		if len(errs) > 0 {
			w.WriteHeader(http.StatusUnprocessableEntity)
		}
		success := ""
		if len(errs) == 0 {
			success = "Interconnect updated"
		}
		_ = execT(w, h.CarrFragT, "interconnect_panel", interconnectPanelData{
			CarrierID: cid, CSRFToken: csrf.Token(r), Carrier: c,
			Interconnect: interconnectType(c), Success: success, Errors: errs,
		})
	}
}

func (h *Handlers) GetCarrierHTTPInterconnect() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cid := chi.URLParam(r, "id")
		c, err := db.GetCarrier(r.Context(), h.Pool, cid)
		if err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		_ = execT(w, h.CarrFragT, "http_interconnect_panel", struct {
			CarrierID, CSRFToken string
			Carrier              *db.CarrierFull
			Success              string
			Errors               map[string]string
		}{cid, csrf.Token(r), c, "", nil})
	}
}

func (h *Handlers) SaveCarrierHTTPInterconnect() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cid := chi.URLParam(r, "id")
		_ = r.ParseForm()
		endpoint := strings.TrimSpace(r.FormValue("endpoint_url"))
		method := strings.ToUpper(strings.TrimSpace(r.FormValue("http_method")))
		errs := map[string]string{}
		if endpoint == "" {
			errs["endpoint_url"] = "Endpoint URL is required"
		} else if em := validateHTTPEndpoint(endpoint); em != "" {
			errs["endpoint_url"] = em
		}
		if method != "GET" && method != "POST" {
			errs["http_method"] = "Method must be GET or POST"
		}
		if len(errs) == 0 {
			if err := db.UpdateCarrierHTTPInterconnect(r.Context(), h.Pool, cid, endpoint, method); err != nil {
				ServerError(w, r, err, h.Log, h.T500)
				return
			}
			h.reloadRouteCache(r.Context())
		}
		c, _ := db.GetCarrier(r.Context(), h.Pool, cid)
		if len(errs) > 0 {
			w.WriteHeader(http.StatusUnprocessableEntity)
		}
		success := ""
		if len(errs) == 0 {
			success = "HTTP settings saved"
			w.Header().Set("HX-Trigger", "carrierHttpMethodChanged")
		}
		_ = execT(w, h.CarrFragT, "http_interconnect_panel", struct {
			CarrierID, CSRFToken string
			Carrier              *db.CarrierFull
			Success              string
			Errors               map[string]string
		}{cid, csrf.Token(r), c, success, errs})
	}
}

func validateHTTPEndpoint(endpoint string) string {
	u, err := url.Parse(endpoint)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return "Enter a valid http or https URL with host"
	}
	return ""
}
