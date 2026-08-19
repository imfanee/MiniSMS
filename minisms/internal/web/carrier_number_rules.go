// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package web

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/csrf"

	"github.com/minisms/minisms/internal/carrier/numrules"
	"github.com/minisms/minisms/internal/db"
)

type numberRulesPanelData struct {
	CarrierID string
	CSRFToken string
	RulesJSON string // the saved Config as JSON, to seed the Alpine editor
	Success   string
	Error     string
}

func (h *Handlers) renderNumberRulesPanel(w http.ResponseWriter, r *http.Request, carrierID string, cfg numrules.Config, success, errMsg string) {
	b, _ := json.Marshal(cfg)
	_ = execT(w, h.CarrFragT, "number_rules_panel", numberRulesPanelData{
		CarrierID: carrierID, CSRFToken: csrf.Token(r), RulesJSON: string(b), Success: success, Error: errMsg,
	})
}

// GetCarrierNumberRules renders the per-carrier number-translation panel (A-number and B-number rule
// lists) seeded with the saved configuration.
func (h *Handlers) GetCarrierNumberRules() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cid := chi.URLParam(r, "id")
		cfg, err := db.GetCarrierNumberRules(r.Context(), h.Pool, cid)
		if err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		h.renderNumberRulesPanel(w, r, cid, cfg, "", "")
	}
}

// SaveCarrierNumberRules validates and persists the carrier's rules (JSON posted from the editor), then
// reloads the dispatch cache so the change takes effect immediately. Audited as carrier.number_rules.
func (h *Handlers) SaveCarrierNumberRules() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cid := chi.URLParam(r, "id")
		_ = r.ParseForm()
		var cfg numrules.Config
		if err := json.Unmarshal([]byte(r.FormValue("rules_json")), &cfg); err != nil {
			w.WriteHeader(http.StatusUnprocessableEntity)
			h.renderNumberRulesPanel(w, r, cid, cfg, "", "Could not read the rules payload.")
			return
		}
		if _, err := numrules.Compile(cfg); err != nil {
			w.WriteHeader(http.StatusUnprocessableEntity)
			h.renderNumberRulesPanel(w, r, cid, cfg, "", "Invalid rule: "+err.Error())
			return
		}
		if err := db.SaveCarrierNumberRules(r.Context(), h.Pool, cid, cfg); err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		h.reloadRouteCache(r.Context())
		h.recordAudit(r, "carrier.number_rules", "carrier", &cid, nil, nil)
		saved, _ := db.GetCarrierNumberRules(r.Context(), h.Pool, cid)
		h.renderNumberRulesPanel(w, r, cid, saved, "Number rules saved", "")
	}
}

// TestCarrierNumberRules applies the editor's current (unsaved) rules to a sample A-number and B-number
// and returns the transformed results. It runs the same Go RE2 engine as live dispatch, so the operator
// sees exactly what would be sent (a client-side JS preview could diverge on regex semantics).
func (h *Handlers) TestCarrierNumberRules() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		var cfg numrules.Config
		_ = json.Unmarshal([]byte(r.FormValue("rules_json")), &cfg)
		res := struct {
			SampleSender, SampleDest string
			Sender, Destination      string
			Error                    string
		}{SampleSender: r.FormValue("sample_sender"), SampleDest: r.FormValue("sample_dest")}
		compiled, err := numrules.Compile(cfg)
		if err != nil {
			res.Error = err.Error()
		} else {
			res.Sender = compiled.Sender(res.SampleSender)
			res.Destination = compiled.Destination(res.SampleDest)
		}
		_ = execT(w, h.CarrFragT, "number_rules_test_result", res)
	}
}
