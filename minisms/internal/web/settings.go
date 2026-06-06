// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package web

import (
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/csrf"
	"github.com/jackc/pgx/v5"

	"github.com/minisms/minisms/internal/pathutil"
)

type SettingRow struct {
	Key         string
	Value       string
	Description *string
	UpdatedAt   string
	Error       string
}

type SettingsPage struct {
	AdminView
	Title       string
	CurrentPath string
	CSRFToken   string
	Flash       *Flash
	Rows        []SettingRow
}

func (h *Handlers) ShowSettings() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := h.listSettingsRows(r)
		if err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		p := SettingsPage{
			Title:       "Settings",
			CurrentPath: "/admin/settings",
			CSRFToken:   csrf.Token(r),
			Flash:       GetFlash(w, r, "/", h.Config.SecretKey, h.Config.IsProduction()),
			Rows:        rows,
		}
		if err := execT(w, h.SettingsT, "base", p, r); err != nil {
			ServerError(w, r, err, h.Log, h.T500)
		}
	}
}

func (h *Handlers) UpdateSetting() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		key := chi.URLParam(r, "key")
		if err := r.ParseForm(); err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		value := strings.TrimSpace(r.FormValue("value"))
		if r.FormValue("value_bool") != "" {
			if r.FormValue("value_bool") == "on" {
				value = "true"
			} else {
				value = "false"
			}
		}
		row, err := h.getSettingRow(r, key)
		if err != nil {
			if err == pgx.ErrNoRows {
				http.NotFound(w, r)
				return
			}
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		if msg := validateSettingValue(key, value); msg != "" {
			row.Error = msg
			row.Value = value
			if err := execT(w, h.SettingsFragT, "setting_row", row); err != nil {
				ServerError(w, r, err, h.Log, h.T500)
			}
			return
		}
		if _, err := h.Pool.Exec(r.Context(), `UPDATE system_settings SET value=$1, updated_at=now() WHERE key=$2`, value, key); err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		h.recordAudit(r, "setting.update", "system_setting", nil, &key, map[string]string{"value": value})
		row, err = h.getSettingRow(r, key)
		if err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		w.Header().Set("HX-Trigger", "settingSaved")
		if err := execT(w, h.SettingsFragT, "setting_row", row); err != nil {
			ServerError(w, r, err, h.Log, h.T500)
		}
	}
}

func (h *Handlers) listSettingsRows(r *http.Request) ([]SettingRow, error) {
	rows, err := h.Pool.Query(r.Context(), `SELECT key, value, description, updated_at::text FROM system_settings ORDER BY key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SettingRow
	for rows.Next() {
		var x SettingRow
		if err := rows.Scan(&x.Key, &x.Value, &x.Description, &x.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

func (h *Handlers) getSettingRow(r *http.Request, key string) (SettingRow, error) {
	var x SettingRow
	err := h.Pool.QueryRow(r.Context(), `SELECT key, value, description, updated_at::text FROM system_settings WHERE key=$1`, key).
		Scan(&x.Key, &x.Value, &x.Description, &x.UpdatedAt)
	return x, err
}

func validateSettingValue(key, value string) string {
	alphaNum := regexp.MustCompile(`^[A-Za-z0-9]{1,11}$`)
	switch key {
	case "default_sender_id":
		if !alphaNum.MatchString(value) {
			return "must be 1-11 alphanumeric characters"
		}
	case "sender_id_any_allowed_pattern":
		if _, err := regexp.Compile(value); err != nil {
			return "must be a valid regular expression"
		}
		if len(value) > 500 {
			return "pattern must be at most 500 characters"
		}
	case "carrier_dispatch_timeout_s":
		if !inIntRange(value, 1, 60) {
			return "must be integer 1-60"
		}
	case "low_balance_alert_threshold", "carrier_low_balance_alert":
		if !isNonNegativeDecimal(value) {
			return "must be decimal >= 0"
		}
	case "refund_on_carrier_failure", "failover_enabled":
		if value != "true" && value != "false" {
			return "must be true or false"
		}
	case "max_sms_segments":
		if !inIntRange(value, 1, 8) {
			return "must be integer 1-8"
		}
	case "admin_session_idle_minutes":
		if !inIntRange(value, 15, 1440) {
			return "must be integer 15-1440"
		}
	case "api_rate_limit_per_minute":
		if !inIntRange(value, 1, 1000) {
			return "must be integer 1-1000"
		}
	case "invoice_header_image":
		if err := pathutil.ValidateRelativeDataPath(value, "assets"); err != nil {
			return "must be a relative path under assets/ (no ..)"
		}
	default:
		return "unknown setting key"
	}
	return ""
}

func inIntRange(s string, min, max int) bool {
	n, err := strconv.Atoi(s)
	return err == nil && n >= min && n <= max
}

func isNonNegativeDecimal(s string) bool {
	f, err := strconv.ParseFloat(s, 64)
	return err == nil && f >= 0
}
