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

type clientSMPPPanelData struct {
	ClientID       string
	CSRFToken      string
	Client          *db.Client
	MaskedPassword  string
	CurrentPassword string
	Success         string
	Errors          map[string]string
	BindsKnown      bool
	BindsConnected  int
}

func (h *Handlers) GetClientSMPPSettings() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		c, err := db.GetClient(r.Context(), h.Pool, id)
		if err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		_ = execT(w, h.CLIFragT, "client_smpp_panel", h.clientSMPPPanelData(r, c, "", nil))
	}
}

func (h *Handlers) SaveClientSMPPSettings() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		_ = r.ParseForm()
		c, err := db.GetClient(r.Context(), h.Pool, id)
		if err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		errs := map[string]string{}
		enabled := parseBoolCheckbox(r.FormValue("smpp_ingress_enabled"))
		systemID := strings.TrimSpace(r.FormValue("smpp_system_id"))
		passwordRaw := strings.TrimSpace(r.FormValue("smpp_password"))
		allowedCIDRs := strings.TrimSpace(r.FormValue("smpp_allowed_cidrs"))
		dlrMode, ok := validateDLRDeliveryMode(r.FormValue("dlr_delivery_mode"))
		if !ok {
			errs["dlr_delivery_mode"] = "Must be http, smpp, or both"
		}
		maxBinds, err := strconv.Atoi(strings.TrimSpace(r.FormValue("smpp_max_binds")))
		if err != nil || maxBinds < 0 || maxBinds > 100 {
			errs["smpp_max_binds"] = "Must be 0–100"
		}
		throughput, err := strconv.Atoi(strings.TrimSpace(r.FormValue("smpp_throughput_per_s")))
		if err != nil || throughput < 1 || throughput > 10000 {
			errs["smpp_throughput_per_s"] = "Must be 1–10000"
		}
		srcTON, err := parseOptionalInt16(r.FormValue("smpp_default_src_ton"))
		if err != nil {
			errs["smpp_default_src_ton"] = "Invalid TON"
		}
		srcNPI, err := parseOptionalInt16(r.FormValue("smpp_default_src_npi"))
		if err != nil {
			errs["smpp_default_src_npi"] = "Invalid NPI"
		}
		if e := validateSMPPCIDRs(allowedCIDRs); e != nil {
			errs["smpp_allowed_cidrs"] = e.Error()
		}

		if enabled {
			if systemID == "" {
				errs["smpp_system_id"] = "Required when SMPP ingress is enabled"
			}
			hasPassword := c.SMPPPasswordEnc != nil && strings.TrimSpace(*c.SMPPPasswordEnc) != ""
			if passwordRaw == "" && !hasPassword {
				errs["smpp_password"] = "Password required for new SMPP ingress"
			}
		}
		if (dlrMode == "smpp" || dlrMode == "both") && !enabled {
			errs["dlr_delivery_mode"] = "Enable SMPP ingress when delivering DLR via SMPP"
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

		var cidrs *string
		if allowedCIDRs != "" {
			cidrs = &allowedCIDRs
		}

		if len(errs) == 0 {
			settings := db.ClientSMPPSettings{
				SMPPIngressEnabled: enabled,
				SMPPSystemID:       strPtrOrNil(systemID),
				SMPPPasswordEnc:    passwordEnc,
				SMPPAllowedCIDRs:   cidrs,
				SMPPMaxBinds:       maxBinds,
				SMPPDefaultSrcTON:  srcTON,
				SMPPDefaultSrcNPI:  srcNPI,
				SMPPThroughputPerS: throughput,
				DLRDeliveryMode:    dlrMode,
			}
			if err := db.UpdateClientSMPPSettings(r.Context(), h.Pool, id, settings, keepPassword); err != nil {
				if errors.Is(err, db.ErrDuplicateSMPPSystemID) {
					errs["smpp_system_id"] = "System ID already in use"
				} else {
					ServerError(w, r, err, h.Log, h.T500)
					return
				}
			} else {
				c, _ = db.GetClient(r.Context(), h.Pool, id)
			}
		}

		success := ""
		if len(errs) == 0 {
			success = "SMPP ingress settings saved"
		} else {
			w.WriteHeader(http.StatusUnprocessableEntity)
		}
		_ = execT(w, h.CLIFragT, "client_smpp_panel", h.clientSMPPPanelData(r, c, success, errs))
	}
}

func (h *Handlers) clientSMPPPanelData(r *http.Request, c *db.Client, success string, errs map[string]string) clientSMPPPanelData {
	masked := ""
	current := ""
	if c != nil && c.SMPPPasswordEnc != nil && strings.TrimSpace(*c.SMPPPasswordEnc) != "" {
		if dec, err := db.DecryptValue(h.Config.SecretKey, *c.SMPPPasswordEnc); err == nil {
			masked = maskTail(dec)
			current = dec
		}
	}
	cid := ""
	if c != nil {
		cid = c.ClientID
	}
	data := clientSMPPPanelData{
		ClientID:        cid,
		CSRFToken:       csrf.Token(r),
		Client:          c,
		MaskedPassword:  masked,
		CurrentPassword: current,
		Success:         success,
		Errors:          errs,
	}
	if h.SMPPIngress != nil && cid != "" {
		data.BindsKnown = true
		data.BindsConnected = h.SMPPIngress.BindCount(cid)
	}
	return data
}
