package web

import (
	"encoding/json"
	"html/template"
	"net/http"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/csrf"
	"github.com/minisms/minisms"
	"github.com/minisms/minisms/internal/db"
)

func (h *Handlers) GetCarrierSenderIDsPanel() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cid := chi.URLParam(r, "id")
		c, err := db.GetCarrier(r.Context(), h.Pool, cid)
		if err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		rows, _ := db.ListCarrierSenderIDs(r.Context(), h.Pool, cid)
		avail, _ := db.GetAvailableSenderIDsForCarrier(r.Context(), h.Pool, cid)
		var policy string
		var defaultSID *string
		_ = h.Pool.QueryRow(r.Context(), `SELECT sender_id_policy, default_sender_id_value FROM carriers WHERE carrier_id=$1::uuid`, cid).Scan(&policy, &defaultSID)
		t, err := template.ParseFS(minisms.TemplateFS, "templates/admin/carriers/sender_ids_panel.html")
		if err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		_ = t.ExecuteTemplate(w, "carrier_sender_ids_panel", struct {
			CarrierID  string
			CSRFToken  string
			Carrier    *db.CarrierFull
			Policy     string
			DefaultSID *string
			Rows       []db.CarrierSenderIDRow
			Available  []db.SenderID
		}{cid, csrf.Token(r), c, policy, defaultSID, rows, avail})
	}
}

func (h *Handlers) AddCarrierSenderID() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cid := chi.URLParam(r, "id")
		_ = r.ParseForm()
		sid := r.FormValue("sender_id")
		if sid != "" {
			_ = db.AddCarrierSenderID(r.Context(), h.Pool, cid, sid)
		}
		h.GetCarrierSenderIDsPanel().ServeHTTP(w, r)
	}
}

func (h *Handlers) RemoveCarrierSenderID() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cid := chi.URLParam(r, "id")
		csid := chi.URLParam(r, "cid")
		_ = db.RemoveCarrierSenderID(r.Context(), h.Pool, csid, cid)
		h.GetCarrierSenderIDsPanel().ServeHTTP(w, r)
	}
}

func (h *Handlers) SetCarrierSenderIDDefault() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cid := chi.URLParam(r, "id")
		csid := chi.URLParam(r, "cid")
		_ = db.SetCarrierSenderIDDefault(r.Context(), h.Pool, csid, cid)
		h.GetCarrierSenderIDsPanel().ServeHTTP(w, r)
	}
}

func maskTail(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if len(s) <= 4 {
		return strings.Repeat("•", len(s))
	}
	return strings.Repeat("•", len(s)-4) + s[len(s)-4:]
}

func (h *Handlers) GetCarrierDLRSettings() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cid := chi.URLParam(r, "id")
		c, err := db.GetCarrier(r.Context(), h.Pool, cid)
		if err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		maskedSecret := ""
		if c.DLRInboundSecret != nil && strings.TrimSpace(*c.DLRInboundSecret) != "" {
			if dec, derr := db.DecryptValue(h.Config.SecretKey, *c.DLRInboundSecret); derr == nil {
				maskedSecret = maskTail(dec)
			}
		}
		_ = execT(w, h.CarrFragT, "dlr_panel", struct {
			CarrierID           string
			CSRFToken           string
			Carrier             *db.CarrierFull
			MaskedInboundSecret string
			Success             string
			Errors              map[string]string
		}{cid, csrf.Token(r), c, maskedSecret, "", nil})
	}
}

func (h *Handlers) SaveCarrierDLRSettings() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cid := chi.URLParam(r, "id")
		_ = r.ParseForm()
		c, err := db.GetCarrier(r.Context(), h.Pool, cid)
		if err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		errs := map[string]string{}
		callbackURLTemplate := strings.TrimSpace(r.FormValue("dlr_callback_url_template"))
		dlrFieldName := strings.TrimSpace(r.FormValue("dlr_field_name"))
		dlrMessageIDField := strings.TrimSpace(r.FormValue("dlr_message_id_field"))
		dlrStatusField := strings.TrimSpace(r.FormValue("dlr_status_field"))
		dlrStatusMap := strings.TrimSpace(r.FormValue("dlr_status_map"))
		dlrInboundSecretRaw := strings.TrimSpace(r.FormValue("dlr_inbound_secret"))
		smppSourceTON := strings.TrimSpace(r.FormValue("smpp_source_addr_ton"))
		smppSourceNPI := strings.TrimSpace(r.FormValue("smpp_source_addr_npi"))
		smppDestTON := strings.TrimSpace(r.FormValue("smpp_dest_addr_ton"))
		smppDestNPI := strings.TrimSpace(r.FormValue("smpp_dest_addr_npi"))
		if callbackURLTemplate != "" {
			resolved := strings.ReplaceAll(callbackURLTemplate, "{{message_id}}", "00000000-0000-0000-0000-000000000000")
			u, e := url.Parse(resolved)
			if e != nil || !strings.EqualFold(u.Scheme, "https") || strings.TrimSpace(u.Host) == "" {
				errs["dlr_callback_url_template"] = "Must be a valid https:// URL template"
			}
		}
		if dlrStatusMap != "" {
			var obj map[string]string
			if e := json.Unmarshal([]byte(dlrStatusMap), &obj); e != nil {
				errs["dlr_status_map"] = "Must be a valid JSON object"
			}
		}
		if !isValidSmppValue(smppSourceTON, true) {
			errs["smpp_source_addr_ton"] = "Invalid TON value"
		}
		if !isValidSmppValue(smppDestTON, true) {
			errs["smpp_dest_addr_ton"] = "Invalid TON value"
		}
		if !isValidSmppValue(smppSourceNPI, false) {
			errs["smpp_source_addr_npi"] = "Invalid NPI value"
		}
		if !isValidSmppValue(smppDestNPI, false) {
			errs["smpp_dest_addr_npi"] = "Invalid NPI value"
		}

		inboundSecret := c.DLRInboundSecret
		if dlrInboundSecretRaw != "" {
			enc, e := db.EncryptValue(h.Config.SecretKey, dlrInboundSecretRaw)
			if e != nil {
				errs["dlr_inbound_secret"] = "Failed to encrypt inbound secret"
			} else {
				inboundSecret = &enc
			}
		}
		if len(errs) == 0 {
			err = db.UpdateCarrierDLRSettings(r.Context(), h.Pool, cid, db.CarrierDLRSettings{
				DLRCallbackURLTemplate: strPtr(callbackURLTemplate),
				DLRFieldName:           strPtr(dlrFieldName),
				DLRInboundSecret:       inboundSecret,
				DLRMessageIDField:      strPtr(dlrMessageIDField),
				DLRStatusField:         strPtr(dlrStatusField),
				DLRStatusMap:           strPtr(dlrStatusMap),
				SMPPSourceAddrTON:      defaultSmpp(smppSourceTON),
				SMPPSourceAddrNPI:      defaultSmpp(smppSourceNPI),
				SMPPDestAddrTON:        defaultSmpp(smppDestTON),
				SMPPDestAddrNPI:        defaultSmpp(smppDestNPI),
			})
			if err != nil {
				ServerError(w, r, err, h.Log, h.T500)
				return
			}
			c, _ = db.GetCarrier(r.Context(), h.Pool, cid)
		}
		maskedSecret := ""
		if c != nil && c.DLRInboundSecret != nil && strings.TrimSpace(*c.DLRInboundSecret) != "" {
			if dec, derr := db.DecryptValue(h.Config.SecretKey, *c.DLRInboundSecret); derr == nil {
				maskedSecret = maskTail(dec)
			}
		}
		if len(errs) > 0 {
			w.WriteHeader(http.StatusUnprocessableEntity)
		}
		_ = execT(w, h.CarrFragT, "dlr_panel", struct {
			CarrierID           string
			CSRFToken           string
			Carrier             *db.CarrierFull
			MaskedInboundSecret string
			Success             string
			Errors              map[string]string
		}{cid, csrf.Token(r), c, maskedSecret, map[bool]string{true: "", false: "DLR settings saved"}[len(errs) > 0], errs})
	}
}

func defaultSmpp(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return "dynamic"
	}
	return v
}

func isValidSmppValue(v string, ton bool) bool {
	v = defaultSmpp(v)
	if v == "dynamic" {
		return true
	}
	if ton {
		switch v {
		case "0", "1", "2", "3", "4", "5", "6":
			return true
		default:
			return false
		}
	}
	switch v {
	case "0", "1", "3", "4", "6", "8", "9", "10", "18":
		return true
	default:
		return false
	}
}
