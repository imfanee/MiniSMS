// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package web

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/csrf"
	"github.com/minisms/minisms/internal/db"
)

type carrierSMPPPanelData struct {
	CarrierID      string
	CSRFToken      string
	Carrier        *db.CarrierFull
	MaskedPassword string
	Success        string
	Errors         map[string]string
}

func (h *Handlers) GetCarrierSMPPSettings() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cid := chi.URLParam(r, "id")
		c, err := db.GetCarrier(r.Context(), h.Pool, cid)
		if err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		_ = execT(w, h.CarrFragT, "carrier_smpp_panel", h.carrierSMPPPanelData(r, c, "", nil))
	}
}

func (h *Handlers) SaveCarrierSMPPSettings() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cid := chi.URLParam(r, "id")
		_ = r.ParseForm()
		c, err := db.GetCarrier(r.Context(), h.Pool, cid)
		if err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		errs := map[string]string{}
		if interconnectType(c) != "smpp" {
			errs["interconnect"] = "Set interconnect to SMPP before saving SMPP settings"
		}
		bindMode, ok := validateCarrierBindMode(r.FormValue("smpp_bind_mode"))
		if !ok {
			errs["smpp_bind_mode"] = "Must be tx or trx for carrier egress"
		}
		host := strings.TrimSpace(r.FormValue("smpp_host"))
		portStr := strings.TrimSpace(r.FormValue("smpp_port"))
		systemID := strings.TrimSpace(r.FormValue("smpp_system_id"))
		passwordRaw := strings.TrimSpace(r.FormValue("smpp_password"))
		systemType := strings.TrimSpace(r.FormValue("smpp_system_type"))
		tls := parseBoolCheckbox(r.FormValue("smpp_tls"))

		enquireS, err := strconv.Atoi(strings.TrimSpace(r.FormValue("smpp_enquire_link_s")))
		if err != nil || enquireS < 5 || enquireS > 3600 {
			errs["smpp_enquire_link_s"] = "Must be 5–3600"
		}
		window, err := strconv.Atoi(strings.TrimSpace(r.FormValue("smpp_window_size")))
		if err != nil || window < 1 || window > 1000 {
			errs["smpp_window_size"] = "Must be 1–1000"
		}
		throughput, err := strconv.Atoi(strings.TrimSpace(r.FormValue("smpp_throughput_per_s")))
		if err != nil || throughput < 1 || throughput > 10000 {
			errs["smpp_throughput_per_s"] = "Must be 1–10000"
		}

		var port *int
		if portStr != "" {
			p, e := strconv.Atoi(portStr)
			if e != nil || p < 1 || p > 65535 {
				errs["smpp_port"] = "Must be 1–65535"
			} else {
				port = &p
			}
		}

		if host == "" {
			errs["smpp_host"] = "SMSC host is required"
		}
		if port == nil {
			errs["smpp_port"] = "Port is required"
		}
		if systemID == "" {
			errs["smpp_system_id"] = "System ID is required"
		}
		hasPassword := c.SMPPPasswordEnc != nil && strings.TrimSpace(*c.SMPPPasswordEnc) != ""
		if passwordRaw == "" && !hasPassword {
			errs["smpp_password"] = "Password required for new SMPP egress"
		}

		keepPassword := passwordRaw == ""
		var passwordEnc *string
		if passwordRaw != "" {
			enc, e := db.EncryptValue(h.Config.SecretKey, passwordRaw)
			if e != nil {
				errs["smpp_password"] = "Failed to encrypt password"
			} else {
				passwordEnc = &enc
			}
		}

		if len(errs) == 0 {
			settings := db.CarrierSMPPSettings{
				SMPPHost:           strPtrOrNil(host),
				SMPPPort:           port,
				SMPPSystemID:       strPtrOrNil(systemID),
				SMPPPasswordEnc:    passwordEnc,
				SMPPSystemType:     strPtrOrNil(systemType),
				SMPPBindMode:       bindMode,
				SMPPTLS:            tls,
				SMPPEnquireLinkS:   enquireS,
				SMPPWindowSize:     window,
				SMPPThroughputPerS: throughput,
			}
			if err := db.UpdateCarrierSMPPSettings(r.Context(), h.Pool, cid, settings, keepPassword); err != nil {
				if errors.Is(err, db.ErrDuplicateSMPPSystemID) {
					errs["smpp_system_id"] = "System ID already in use"
				} else {
					ServerError(w, r, err, h.Log, h.T500)
					return
				}
			} else {
				c, _ = db.GetCarrier(r.Context(), h.Pool, cid)
				h.reloadRouteCache(r.Context())
			}
		}

		success := ""
		if len(errs) == 0 {
			success = "SMPP egress settings saved (sessions refresh within ~60s)"
		} else {
			w.WriteHeader(http.StatusUnprocessableEntity)
		}
		_ = execT(w, h.CarrFragT, "carrier_smpp_panel", h.carrierSMPPPanelData(r, c, success, errs))
	}
}

func (h *Handlers) carrierSMPPPanelData(r *http.Request, c *db.CarrierFull, success string, errs map[string]string) carrierSMPPPanelData {
	cid := chi.URLParam(r, "id")
	if c != nil {
		cid = c.CarrierID
	}
	masked := ""
	if c != nil && c.SMPPPasswordEnc != nil && strings.TrimSpace(*c.SMPPPasswordEnc) != "" {
		if dec, err := db.DecryptValue(h.Config.SecretKey, *c.SMPPPasswordEnc); err == nil {
			masked = maskTail(dec)
		}
	}
	return carrierSMPPPanelData{
		CarrierID:      cid,
		CSRFToken:      csrf.Token(r),
		Carrier:        c,
		MaskedPassword: masked,
		Success:        success,
		Errors:         errs,
	}
}
