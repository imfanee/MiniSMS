// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package web

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/minisms/minisms/internal/adminauth"
	"github.com/minisms/minisms/internal/db"
)

// RequirePerm denies the request when the admin lacks the given permission.
func RequirePerm(perm string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !Can(r, perm) {
				Forbidden(w, r)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RequireSuperAdmin denies non-super-admin users.
func RequireSuperAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u := AdminFromContext(r.Context())
		if u == nil || !u.IsSuperAdmin {
			Forbidden(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Forbidden responds with 403 HTML or plain text for HTMX.
func Forbidden(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`<div class="alert alert-warning mb-0">You do not have permission for this action.</div>`))
		return
	}
	http.Error(w, "Forbidden", http.StatusForbidden)
}

// LoadAdminUserMiddleware loads the admin user after SessionAuth (requires session with admin_user_id).
func LoadAdminUserMiddleware(pool *pgxpool.Pool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			sess := SessionFromContext(r.Context())
			if sess == nil || sess.AdminUserID == nil || *sess.AdminUserID == "" {
				redirectToLogin(w, r)
				return
			}
			u, err := db.GetAdminUserByID(r.Context(), pool, *sess.AdminUserID)
			if err != nil {
				http.Error(w, "internal error", http.StatusInternalServerError)
				return
			}
			if u == nil || !u.IsActive {
				redirectToLogin(w, r)
				return
			}
			r = r.WithContext(WithAdminUser(r.Context(), u))
			next.ServeHTTP(w, r)
		})
	}
}

// RegisterProtectedAdminRoutes mounts authenticated admin routes with permission checks.
func RegisterProtectedAdminRoutes(r chi.Router, h *Handlers) {
	r.Get("/dashboard", h.ShowDashboard())

	r.Group(func(r chi.Router) {
		r.Use(RequirePerm(adminauth.PermDashboardStats))
		r.Get("/dashboard/stats", h.DashboardStatsFragment())
		r.Get("/dashboard/reports", h.GetDashboardReports())
		r.Get("/dashboard/reports/sms-by-client", h.GetReportSMSByClient())
		r.Get("/dashboard/reports/sms-by-carrier", h.GetReportSMSByCarrier())
		r.Get("/dashboard/reports/success-clients", h.GetReportSuccessRatioClients())
		r.Get("/dashboard/reports/success-carriers", h.GetReportSuccessRatioCarriers())
		r.Get("/dashboard/reports/carrier-prefix", h.GetReportCarrierPrefixSuccess())
		r.Get("/dashboard/reports/bill-comparison", h.GetReportBillComparison())
		r.Get("/dashboard/reports/cost-comparison", h.GetReportCostComparison())
	})

	r.Group(func(r chi.Router) {
		r.Use(RequirePerm(adminauth.PermSimulate))
		r.Get("/diagnoses/simulate", h.ShowSimulate())
		r.Post("/diagnoses/simulate", h.RunSimulation())
		r.Post("/diagnoses/send", h.SendDiagnosticMessage())
		r.Get("/diagnoses/send-status/{message_id}", h.GetDiagnosticSendStatus())
		r.Get("/simulate", h.RedirectSimulateToDiagnoses())
	})

	r.Group(func(r chi.Router) {
		r.Use(RequirePerm(adminauth.PermCarriersView))
		r.Get("/carriers", h.ListCarriers())
		r.Get("/carriers/{id}", h.GetCarrierDetail())
		r.Get("/carriers/{id}/row", h.GetCarrierRowFragment())
		r.Get("/carriers/{id}/ledger", h.ListLedger())
		r.Get("/carriers/{id}/usage", h.GetUsagePanel())
		r.Get("/carriers/{id}/sender-ids", h.GetCarrierSenderIDsPanel())
		r.Get("/carriers/{id}/dlr-settings", h.GetCarrierDLRSettings())
		r.Get("/carriers/{id}/smpp-addressing", h.GetCarrierSMPPAddressing())
		r.Get("/carriers/{id}/interconnect", h.GetCarrierInterconnect())
		r.Get("/carriers/{id}/interconnect/http", h.GetCarrierHTTPInterconnect())
		r.Get("/carriers/{id}/smpp-settings", h.GetCarrierSMPPSettings())
		r.Get("/carriers/{id}/auth-headers", h.ListAuthHeaders())
		r.Get("/carriers/{id}/template", h.GetTemplatePanel())
		r.Get("/carriers/{id}/invoices", h.GetCarrierInvoicesPanel())
		r.Get("/carriers/{id}/invoices/{invoice_id}/pdf", h.DownloadCarrierInvoicePDF())
	})
	r.Group(func(r chi.Router) {
		r.Use(RequirePerm(adminauth.PermCarriersEdit))
		r.Get("/carriers/new", h.ShowAddForm())
		r.Post("/carriers", h.CreateCarrier())
		r.Get("/carriers/{id}/edit", h.ShowEditForm())
		r.Put("/carriers/{id}", h.UpdateCarrier())
		r.Post("/carriers/{id}/toggle-status", h.ToggleCarrierStatus())
		r.Get("/carriers/{id}/auth-headers/new", h.ShowAddAuthHeaderForm())
		r.Post("/carriers/{id}/auth-headers", h.CreateAuthHeader())
		r.Delete("/carriers/{id}/auth-headers/{header_id}", h.DeleteAuthHeader())
		r.Post("/carriers/{id}/template", h.SaveTemplate())
		r.Post("/carriers/{id}/sender-ids", h.AddCarrierSenderID())
		r.Delete("/carriers/{id}/sender-ids/{cid}", h.RemoveCarrierSenderID())
		r.Post("/carriers/{id}/sender-ids/{cid}/set-default", h.SetCarrierSenderIDDefault())
		r.Post("/carriers/{id}/dlr-settings", h.SaveCarrierDLRSettings())
		r.Post("/carriers/{id}/smpp-addressing", h.SaveCarrierSMPPAddressing())
		r.Post("/carriers/{id}/interconnect", h.SaveCarrierInterconnect())
		r.Post("/carriers/{id}/interconnect/http", h.SaveCarrierHTTPInterconnect())
		r.Post("/carriers/{id}/smpp-settings", h.SaveCarrierSMPPSettings())
		r.Post("/carriers/{id}/invoices/preview", h.PreviewCarrierInvoice())
		r.Post("/carriers/{id}/invoices/generate", h.GenerateCarrierInvoice())
	})
	r.With(RequirePerm(adminauth.PermCarriersPayment)).Post("/carriers/{id}/payments", h.RecordPayment())

	r.Group(func(r chi.Router) {
		r.Use(RequirePerm(adminauth.PermRateGroupsView))
		r.Get("/rate-groups", h.ListRateGroups())
		r.Get("/rate-groups/{id}", h.GetRateGroupDetail())
		r.Get("/rate-groups/{id}/row", h.GetRateGroupRowFragment())
	})
	r.Group(func(r chi.Router) {
		r.Use(RequirePerm(adminauth.PermRateGroupsEdit))
		r.Get("/rate-groups/new", h.ShowAddRateGroupForm())
		r.Post("/rate-groups", h.CreateRateGroup())
		r.Get("/rate-groups/{id}/edit", h.ShowEditRateGroupForm())
		r.Put("/rate-groups/{id}", h.UpdateRateGroup())
		r.Delete("/rate-groups/{id}", h.DeleteRateGroup())
	})
	r.Group(func(r chi.Router) {
		r.Use(RequirePerm(adminauth.PermRateGroupsManage))
		r.Get("/rate-groups/{id}/entries/new", h.ShowAddEntryForm())
		r.Post("/rate-groups/{id}/entries", h.CreateRateEntry())
		r.Get("/rate-groups/{id}/entries/{entry_id}/edit", h.ShowEditEntryForm())
		r.Get("/rate-groups/{id}/entries/{entry_id}/row", h.GetRateEntryRowFragment())
		r.Put("/rate-groups/{id}/entries/{entry_id}", h.UpdateRateEntry())
		r.Delete("/rate-groups/{id}/entries/{entry_id}", h.DeleteRateEntry())
	})

	r.Group(func(r chi.Router) {
		r.Use(RequirePerm(adminauth.PermRoutingGroupsView))
		r.Get("/routing-groups", h.ListRoutingGroups())
		r.Get("/routing-groups/{id}", h.ShowRoutingGroupDetail())
		r.Get("/routing-groups/{id}/row", h.GetRoutingGroupRowFragment())
		r.Get("/routing-groups/{id}/routes", h.ListRouteEntries())
	})
	r.Group(func(r chi.Router) {
		r.Use(RequirePerm(adminauth.PermRoutingGroupsEdit))
		r.Get("/routing-groups/new", h.ShowAddRoutingGroupForm())
		r.Post("/routing-groups", h.CreateRoutingGroup())
		r.Get("/routing-groups/{id}/edit", h.ShowEditRoutingGroupForm())
		r.Put("/routing-groups/{id}", h.UpdateRoutingGroup())
		r.Post("/routing-groups/{id}/toggle-status", h.ToggleRoutingGroupStatus())
	})
	r.Group(func(r chi.Router) {
		r.Use(RequirePerm(adminauth.PermRoutingGroupsManage))
		r.Get("/routing-groups/{id}/routes/new", h.ShowAddRouteForm())
		r.Post("/routing-groups/{id}/routes", h.CreateRouteEntry())
		r.Get("/routing-groups/{id}/routes/{route_id}/edit", h.ShowEditRouteForm())
		r.Get("/routing-groups/{id}/routes/{route_id}/row", h.GetRouteRowFragment())
		r.Put("/routing-groups/{id}/routes/{route_id}", h.UpdateRouteEntry())
		r.Delete("/routing-groups/{id}/routes/{route_id}", h.DeleteRouteEntry())
	})

	r.Group(func(r chi.Router) {
		r.Use(RequirePerm(adminauth.PermClientsView))
		r.Get("/clients", h.ListClients())
		r.Get("/clients/{id}", h.ShowClient())
		r.Get("/clients/{id}/row", h.GetClientRowFragment())
		r.Get("/clients/{id}/info", h.GetClientInfoPanel())
		r.Get("/clients/{id}/ledger", h.ListClientLedger())
		r.Get("/clients/{id}/api-key", h.GetAPIKeyPanel())
		r.Get("/clients/{id}/smpp-settings", h.GetClientSMPPSettings())
		r.Get("/clients/{id}/sender-ids", h.GetClientSenderIDsPanel())
		r.Get("/clients/{id}/invoices", h.GetClientInvoicesPanel())
		r.Get("/clients/{id}/invoices/{invoice_id}/pdf", h.DownloadClientInvoicePDF())
	})
	r.Group(func(r chi.Router) {
		r.Use(RequirePerm(adminauth.PermClientsEdit))
		r.Get("/clients/new", h.ShowAddClientForm())
		r.Post("/clients", h.CreateClient())
		r.Get("/clients/{id}/edit", h.ShowEditClientForm())
		r.Put("/clients/{id}", h.UpdateClient())
		r.Post("/clients/{id}/toggle-status", h.ToggleClientStatus())
		r.Post("/clients/{id}/api-key/generate", h.GenerateClientAPIKey())
		r.Post("/clients/{id}/api-key/revoke", h.RevokeClientAPIKey())
		r.Post("/clients/{id}/smpp-settings", h.SaveClientSMPPSettings())
		r.Post("/clients/{id}/sender-ids", h.AddClientSenderID())
		r.Delete("/clients/{id}/sender-ids/{cid}", h.RemoveClientSenderID())
		r.Post("/clients/{id}/sender-ids/{cid}/set-default", h.SetClientSenderIDDefault())
		r.Post("/clients/{id}/invoices/preview", h.PreviewClientInvoice())
		r.Post("/clients/{id}/invoices/generate", h.GenerateClientInvoice())
	})
	r.With(RequirePerm(adminauth.PermClientsPayment)).Post("/clients/{id}/credit", h.CreditClientBalance())

	r.Group(func(r chi.Router) {
		r.Use(RequirePerm(adminauth.PermSMSLogsView))
		r.Get("/sms-logs", h.ListSMSLogs())
		r.Get("/sms-logs/export.csv", h.ExportSMSLogsCSV())
		r.Get("/sms-logs/export.pdf", h.ExportSMSLogsPDF())
		r.Get("/sms-logs/{id}", h.SMSLogDetailModal())
	})

	r.Group(func(r chi.Router) {
		r.Use(RequireSuperAdmin)
		r.Get("/audit-log", h.ListAuditLog())
		r.Get("/settings", h.ShowSettings())
		r.Post("/settings/{key}", h.UpdateSetting())
		r.Post("/settings/invoice-header", h.UploadInvoiceHeader())
		r.Get("/admin-users", h.ListAdminUsers())
		r.Get("/admin-users/new", h.ShowNewAdminUser())
		r.Post("/admin-users", h.CreateAdminUser())
		r.Get("/admin-users/{id}/edit", h.ShowEditAdminUser())
		r.Post("/admin-users/{id}", h.UpdateAdminUser())
	})

	r.Route("/currencies", func(r chi.Router) {
		r.Group(func(r chi.Router) {
			r.Use(RequirePerm(adminauth.PermCurrenciesView))
			r.Get("/", h.ListCurrencies())
		})
		r.Group(func(r chi.Router) {
			r.Use(RequirePerm(adminauth.PermCurrenciesEdit))
			r.Post("/", h.CreateCurrency())
			r.Put("/{code}", h.UpdateCurrency())
			r.Post("/{code}/toggle", h.ToggleCurrencyActive())
		})
	})
	r.Route("/sender-ids", func(r chi.Router) {
		r.Group(func(r chi.Router) {
			r.Use(RequirePerm(adminauth.PermSenderIDsView))
			r.Get("/", h.ListSenderIDs())
			r.Get("/{id}/row", h.GetSenderIDRowView())
		})
		r.Group(func(r chi.Router) {
			r.Use(RequirePerm(adminauth.PermSenderIDsEdit))
			r.Get("/{id}/edit-row", h.GetSenderIDRowEdit())
			r.Post("/", h.CreateSenderID())
			r.Put("/{id}", h.UpdateSenderID())
			r.Post("/{id}/toggle", h.ToggleSenderIDActive())
		})
	})
}
