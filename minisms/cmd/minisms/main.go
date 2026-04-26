package main

import (
	"context"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres" // registers postgres:// and postgresql://
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/minisms/minisms"
	"github.com/minisms/minisms/internal/api"
	"github.com/minisms/minisms/internal/config"
	"github.com/minisms/minisms/internal/db"
	"github.com/minisms/minisms/internal/web"
)

var (
	version   = "1.0.0"
	commit    = "dev"
	buildTime = "unknown"
)

func main() {
	ctx := context.Background()
	cfg, err := config.Load()
	if err != nil {
		slog.Error("config", "err", err)
		os.Exit(1)
	}
	log := newLogger(cfg.LogLevel)

	mDir, err := findMigrationsDir()
	if err != nil {
		log.Error("migrations path", "err", err)
		os.Exit(1)
	}
	m, err := migrate.New("file://"+filepath.ToSlash(mDir), cfg.DatabaseURL)
	if err != nil {
		log.Error("migrate new", "err", err)
		os.Exit(1)
	}
	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		_, _ = m.Close()
		log.Error("migrate up", "err", err)
		os.Exit(1)
	}
	if se, serr := m.Close(); serr != nil {
		_ = se
	}

	pool, err := db.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Error("database", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	tfs := minisms.TemplateFS
	loginT, err := parseTemplateFS(
		tfs,
		"templates/layout/base.html",
		"templates/layout/partials/navbar.html",
		"templates/layout/partials/flash.html",
		"templates/admin/login.html",
	)
	if err != nil {
		log.Error("template login", "err", err)
		os.Exit(1)
	}
	dashT, err := parseTemplateFS(
		tfs,
		"templates/layout/base.html",
		"templates/layout/partials/navbar.html",
		"templates/layout/partials/flash.html",
		"templates/admin/dashboard.html",
		"templates/admin/dashboard_stats.html",
		"templates/admin/dashboard_reports.html",
	)
	dashFragT, err := parseTemplateFS(
		tfs,
		"templates/admin/dashboard_stats.html",
	)
	if err != nil {
		log.Error("template dashboard fragment", "err", err)
		os.Exit(1)
	}
	if err != nil {
		log.Error("template dashboard", "err", err)
		os.Exit(1)
	}
	simulateT, err := parseTemplateFS(
		tfs,
		"templates/layout/base.html",
		"templates/layout/partials/navbar.html",
		"templates/layout/partials/flash.html",
		"templates/admin/simulate.html",
	)
	if err != nil {
		log.Error("template simulate", "err", err)
		os.Exit(1)
	}
	carrierListT, err := parseTemplateFS(
		tfs,
		"templates/layout/base.html",
		"templates/layout/partials/navbar.html",
		"templates/layout/partials/flash.html",
		"templates/admin/carriers/list.html",
		"templates/admin/carriers/row.html",
		"templates/admin/carriers/add_form_row.html",
		"templates/admin/carriers/edit_form_row.html",
	)
	if err != nil {
		log.Error("template carriers list", "err", err)
		os.Exit(1)
	}
	carrierDetT, err := parseTemplateFS(
		tfs,
		"templates/layout/base.html",
		"templates/layout/partials/navbar.html",
		"templates/layout/partials/flash.html",
		"templates/admin/carriers/detail.html",
	)
	if err != nil {
		log.Error("template carriers detail", "err", err)
		os.Exit(1)
	}
	carrierFragT, err := parseTemplateFS(
		tfs,
		"templates/admin/carriers/row.html",
		"templates/admin/carriers/add_form_row.html",
		"templates/admin/carriers/edit_form_row.html",
		"templates/admin/carriers/headers_table.html",
		"templates/admin/carriers/header_row.html",
		"templates/admin/carriers/add_auth_header_row.html",
		"templates/admin/carriers/template_form.html",
		"templates/admin/carriers/dlr_panel.html",
		"templates/admin/carriers/ledger_panel.html",
		"templates/admin/carriers/usage_panel.html",
	)
	if err != nil {
		log.Error("template carrier fragments", "err", err)
		os.Exit(1)
	}
	rateGroupListT, err := parseTemplateFS(
		tfs,
		"templates/layout/base.html",
		"templates/layout/partials/navbar.html",
		"templates/layout/partials/flash.html",
		"templates/admin/rate_groups/list.html",
		"templates/admin/rate_groups/row.html",
		"templates/admin/rate_groups/add_form_row.html",
		"templates/admin/rate_groups/edit_form_row.html",
	)
	if err != nil {
		log.Error("template rate groups list", "err", err)
		os.Exit(1)
	}
	rateGroupDetT, err := parseTemplateFS(
		tfs,
		"templates/layout/base.html",
		"templates/layout/partials/navbar.html",
		"templates/layout/partials/flash.html",
		"templates/admin/rate_groups/detail.html",
		"templates/admin/rate_groups/entry_row.html",
	)
	if err != nil {
		log.Error("template rate group detail", "err", err)
		os.Exit(1)
	}
	rateGroupFragT, err := parseTemplateFS(
		tfs,
		"templates/admin/rate_groups/row.html",
		"templates/admin/rate_groups/add_form_row.html",
		"templates/admin/rate_groups/edit_form_row.html",
		"templates/admin/rate_groups/entry_row.html",
		"templates/admin/rate_groups/entry_add_row.html",
		"templates/admin/rate_groups/entry_edit_row.html",
	)
	if err != nil {
		log.Error("template rate group fragments", "err", err)
		os.Exit(1)
	}
	routingGroupListT, err := parseTemplateFS(
		tfs,
		"templates/layout/base.html",
		"templates/layout/partials/navbar.html",
		"templates/layout/partials/flash.html",
		"templates/admin/routing_groups/list.html",
		"templates/admin/routing_groups/row.html",
		"templates/admin/routing_groups/add_form_row.html",
		"templates/admin/routing_groups/edit_form_row.html",
	)
	if err != nil {
		log.Error("template routing groups list", "err", err)
		os.Exit(1)
	}
	routingGroupDetT, err := parseTemplateFS(
		tfs,
		"templates/layout/base.html",
		"templates/layout/partials/navbar.html",
		"templates/layout/partials/flash.html",
		"templates/admin/routing_groups/detail.html",
	)
	if err != nil {
		log.Error("template routing group detail", "err", err)
		os.Exit(1)
	}
	routingGroupFragT, err := parseTemplateFS(
		tfs,
		"templates/admin/routing_groups/row.html",
		"templates/admin/routing_groups/add_form_row.html",
		"templates/admin/routing_groups/edit_form_row.html",
		"templates/admin/routing_groups/route_list.html",
		"templates/admin/routing_groups/route_row.html",
		"templates/admin/routing_groups/route_add_form_row.html",
		"templates/admin/routing_groups/route_edit_form_row.html",
	)
	if err != nil {
		log.Error("template routing group fragments", "err", err)
		os.Exit(1)
	}
	clientListT, err := parseTemplateFS(
		tfs,
		"templates/layout/base.html",
		"templates/layout/partials/navbar.html",
		"templates/layout/partials/flash.html",
		"templates/admin/clients/list.html",
		"templates/admin/clients/row.html",
		"templates/admin/clients/add_form_row.html",
		"templates/admin/clients/edit_form_row.html",
	)
	if err != nil {
		log.Error("template clients list", "err", err)
		os.Exit(1)
	}
	clientDetT, err := parseTemplateFS(
		tfs,
		"templates/layout/base.html",
		"templates/layout/partials/navbar.html",
		"templates/layout/partials/flash.html",
		"templates/admin/clients/detail.html",
	)
	if err != nil {
		log.Error("template clients detail", "err", err)
		os.Exit(1)
	}
	clientFragT, err := parseTemplateFS(
		tfs,
		"templates/admin/clients/row.html",
		"templates/admin/clients/add_form_row.html",
		"templates/admin/clients/edit_form_row.html",
		"templates/admin/clients/info_form.html",
		"templates/admin/clients/ledger_panel.html",
		"templates/admin/clients/apikey_panel.html",
		"templates/admin/clients/apikey_display.html",
	)
	if err != nil {
		log.Error("template client fragments", "err", err)
		os.Exit(1)
	}
	smsLogT, err := parseTemplateFS(
		tfs,
		"templates/layout/base.html",
		"templates/layout/partials/navbar.html",
		"templates/layout/partials/flash.html",
		"templates/admin/sms_logs/list.html",
		"templates/admin/sms_logs/table.html",
	)
	if err != nil {
		log.Error("template sms logs", "err", err)
		os.Exit(1)
	}
	smsLogFragT, err := parseTemplateFS(
		tfs,
		"templates/admin/sms_logs/table.html",
		"templates/admin/sms_logs/detail_modal.html",
	)
	if err != nil {
		log.Error("template sms logs fragments", "err", err)
		os.Exit(1)
	}
	auditLogT, err := parseTemplateFS(
		tfs,
		"templates/layout/base.html",
		"templates/layout/partials/navbar.html",
		"templates/layout/partials/flash.html",
		"templates/admin/audit_log/list.html",
	)
	if err != nil {
		log.Error("template audit log", "err", err)
		os.Exit(1)
	}
	settingsT, err := parseTemplateFS(
		tfs,
		"templates/layout/base.html",
		"templates/layout/partials/navbar.html",
		"templates/layout/partials/flash.html",
		"templates/admin/settings/form.html",
		"templates/admin/settings/row.html",
	)
	if err != nil {
		log.Error("template settings", "err", err)
		os.Exit(1)
	}
	settingsFragT, err := parseTemplateFS(
		tfs,
		"templates/admin/settings/row.html",
	)
	if err != nil {
		log.Error("template settings fragments", "err", err)
		os.Exit(1)
	}
	t500, err := template.ParseFS(tfs, "templates/errors/500.html")
	if err != nil {
		log.Error("template 500", "err", err)
		os.Exit(1)
	}

	h := &web.Handlers{
		Config:        cfg,
		Pool:          pool,
		Log:           log,
		LoginT:        loginT,
		DashT:         dashT,
		SimulateT:     simulateT,
		CarrListT:     carrierListT,
		CarrDetT:      carrierDetT,
		CarrFragT:     carrierFragT,
		RGListT:       rateGroupListT,
		RGDetT:        rateGroupDetT,
		RGFragT:       rateGroupFragT,
		ROGListT:      routingGroupListT,
		ROGDetT:       routingGroupDetT,
		ROGFragT:      routingGroupFragT,
		CLIListT:      clientListT,
		CLIDetT:       clientDetT,
		CLIFragT:      clientFragT,
		DashFragT:     dashFragT,
		SMSLogT:       smsLogT,
		SMSLogFragT:   smsLogFragT,
		AuditT:        auditLogT,
		SettingsT:     settingsT,
		SettingsFragT: settingsFragT,
		T500:          t500,
	}
	apiHandlers := api.NewHandlers(pool, cfg)
	r := chi.NewRouter()
	r.Use(web.WithRequestID)

	entryRedirect := web.AdminEntryRedirect(pool, cfg)
	r.Get("/", entryRedirect)
	r.Get("/admin", entryRedirect)
	r.Get("/healthz", healthz)
	st, err := fs.Sub(minisms.StaticFS, "static")
	if err != nil {
		log.Error("static fs", "err", err)
		os.Exit(1)
	}
	r.Get("/static/*", http.StripPrefix("/static/", http.FileServer(http.FS(st))).ServeHTTP)

	r.Route("/admin", func(r chi.Router) {
		r.Use(web.CSRF(cfg))
		r.Get("/", entryRedirect)
		r.Get("/login", h.LoginGet())
		r.Post("/login", h.LoginPost())
		r.Get("/logout", h.Logout())
		r.Group(func(r chi.Router) {
			r.Use(web.SessionAuth(pool, cfg))
			r.Get("/dashboard", h.ShowDashboard())
			r.Get("/simulate", h.ShowSimulate())
			r.Post("/simulate", h.RunSimulation())
			r.Get("/dashboard/stats", h.DashboardStatsFragment())
			r.Get("/dashboard/reports", h.GetDashboardReports())
			r.Get("/dashboard/reports/sms-by-client", h.GetReportSMSByClient())
			r.Get("/dashboard/reports/sms-by-carrier", h.GetReportSMSByCarrier())
			r.Get("/dashboard/reports/success-clients", h.GetReportSuccessRatioClients())
			r.Get("/dashboard/reports/success-carriers", h.GetReportSuccessRatioCarriers())
			r.Get("/dashboard/reports/carrier-prefix", h.GetReportCarrierPrefixSuccess())
			r.Get("/dashboard/reports/bill-comparison", h.GetReportBillComparison())
			r.Get("/dashboard/reports/cost-comparison", h.GetReportCostComparison())
			r.Get("/carriers", h.ListCarriers())
			r.Get("/carriers/new", h.ShowAddForm())
			r.Post("/carriers", h.CreateCarrier())
			r.Get("/carriers/{id}/edit", h.ShowEditForm())
			r.Get("/carriers/{id}/row", h.GetCarrierRowFragment())
			r.Put("/carriers/{id}", h.UpdateCarrier())
			r.Post("/carriers/{id}/toggle-status", h.ToggleCarrierStatus())
			r.Get("/carriers/{id}/auth-headers", h.ListAuthHeaders())
			r.Get("/carriers/{id}/auth-headers/new", h.ShowAddAuthHeaderForm())
			r.Post("/carriers/{id}/auth-headers", h.CreateAuthHeader())
			r.Delete("/carriers/{id}/auth-headers/{header_id}", h.DeleteAuthHeader())
			r.Get("/carriers/{id}/template", h.GetTemplatePanel())
			r.Post("/carriers/{id}/template", h.SaveTemplate())
			r.Get("/carriers/{id}/ledger", h.ListLedger())
			r.Post("/carriers/{id}/payments", h.RecordPayment())
			r.Get("/carriers/{id}/usage", h.GetUsagePanel())
			r.Get("/carriers/{id}/sender-ids", h.GetCarrierSenderIDsPanel())
			r.Post("/carriers/{id}/sender-ids", h.AddCarrierSenderID())
			r.Delete("/carriers/{id}/sender-ids/{cid}", h.RemoveCarrierSenderID())
			r.Post("/carriers/{id}/sender-ids/{cid}/set-default", h.SetCarrierSenderIDDefault())
			r.Get("/carriers/{id}/dlr-settings", h.GetCarrierDLRSettings())
			r.Post("/carriers/{id}/dlr-settings", h.SaveCarrierDLRSettings())
			r.Get("/carriers/{id}", h.GetCarrierDetail())
			r.Get("/rate-groups", h.ListRateGroups())
			r.Get("/rate-groups/new", h.ShowAddRateGroupForm())
			r.Post("/rate-groups", h.CreateRateGroup())
			r.Get("/rate-groups/{id}/edit", h.ShowEditRateGroupForm())
			r.Get("/rate-groups/{id}/row", h.GetRateGroupRowFragment())
			r.Put("/rate-groups/{id}", h.UpdateRateGroup())
			r.Delete("/rate-groups/{id}", h.DeleteRateGroup())
			r.Get("/rate-groups/{id}", h.GetRateGroupDetail())
			r.Get("/rate-groups/{id}/entries/new", h.ShowAddEntryForm())
			r.Post("/rate-groups/{id}/entries", h.CreateRateEntry())
			r.Get("/rate-groups/{id}/entries/{entry_id}/edit", h.ShowEditEntryForm())
			r.Get("/rate-groups/{id}/entries/{entry_id}/row", h.GetRateEntryRowFragment())
			r.Put("/rate-groups/{id}/entries/{entry_id}", h.UpdateRateEntry())
			r.Delete("/rate-groups/{id}/entries/{entry_id}", h.DeleteRateEntry())
			r.Get("/routing-groups", h.ListRoutingGroups())
			r.Get("/routing-groups/new", h.ShowAddRoutingGroupForm())
			r.Post("/routing-groups", h.CreateRoutingGroup())
			r.Get("/routing-groups/{id}/edit", h.ShowEditRoutingGroupForm())
			r.Get("/routing-groups/{id}/row", h.GetRoutingGroupRowFragment())
			r.Put("/routing-groups/{id}", h.UpdateRoutingGroup())
			r.Post("/routing-groups/{id}/toggle-status", h.ToggleRoutingGroupStatus())
			r.Get("/routing-groups/{id}", h.ShowRoutingGroupDetail())
			r.Get("/routing-groups/{id}/routes", h.ListRouteEntries())
			r.Get("/routing-groups/{id}/routes/new", h.ShowAddRouteForm())
			r.Post("/routing-groups/{id}/routes", h.CreateRouteEntry())
			r.Get("/routing-groups/{id}/routes/{route_id}/edit", h.ShowEditRouteForm())
			r.Get("/routing-groups/{id}/routes/{route_id}/row", h.GetRouteRowFragment())
			r.Put("/routing-groups/{id}/routes/{route_id}", h.UpdateRouteEntry())
			r.Delete("/routing-groups/{id}/routes/{route_id}", h.DeleteRouteEntry())
			r.Get("/clients", h.ListClients())
			r.Get("/clients/new", h.ShowAddClientForm())
			r.Post("/clients", h.CreateClient())
			r.Get("/clients/{id}/edit", h.ShowEditClientForm())
			r.Get("/clients/{id}/row", h.GetClientRowFragment())
			r.Get("/clients/{id}", h.ShowClient())
			r.Get("/clients/{id}/info", h.GetClientInfoPanel())
			r.Put("/clients/{id}", h.UpdateClient())
			r.Post("/clients/{id}/toggle-status", h.ToggleClientStatus())
			r.Get("/clients/{id}/ledger", h.ListClientLedger())
			r.Post("/clients/{id}/credit", h.CreditClientBalance())
			r.Get("/clients/{id}/api-key", h.GetAPIKeyPanel())
			r.Post("/clients/{id}/api-key/generate", h.GenerateClientAPIKey())
			r.Post("/clients/{id}/api-key/revoke", h.RevokeClientAPIKey())
			r.Get("/clients/{id}/sender-ids", h.GetClientSenderIDsPanel())
			r.Post("/clients/{id}/sender-ids", h.AddClientSenderID())
			r.Delete("/clients/{id}/sender-ids/{cid}", h.RemoveClientSenderID())
			r.Post("/clients/{id}/sender-ids/{cid}/set-default", h.SetClientSenderIDDefault())
			r.Get("/sms-logs", h.ListSMSLogs())
			r.Get("/sms-logs/export.csv", h.ExportSMSLogsCSV())
			r.Get("/sms-logs/export.pdf", h.ExportSMSLogsPDF())
			r.Get("/sms-logs/{id}", h.SMSLogDetailModal())
			r.Get("/audit-log", h.ListAuditLog())
			r.Get("/settings", h.ShowSettings())
			r.Post("/settings/{key}", h.UpdateSetting())
			r.Route("/currencies", func(r chi.Router) {
				r.Get("/", h.ListCurrencies())
				r.Post("/", h.CreateCurrency())
				r.Put("/{code}", h.UpdateCurrency())
				r.Post("/{code}/toggle", h.ToggleCurrencyActive())
			})
			r.Route("/sender-ids", func(r chi.Router) {
				r.Get("/{id}/row", h.GetSenderIDRowView())
				r.Get("/{id}/edit-row", h.GetSenderIDRowEdit())
				r.Get("/", h.ListSenderIDs())
				r.Post("/", h.CreateSenderID())
				r.Put("/{id}", h.UpdateSenderID())
				r.Post("/{id}/toggle", h.ToggleSenderIDActive())
			})
		})
	})
	r.Route("/api/v1", func(r chi.Router) {
		r.MethodFunc(http.MethodGet, "/dlr/{message_id}", apiHandlers.HandleDLR())
		r.MethodFunc(http.MethodPost, "/dlr/{message_id}", apiHandlers.HandleDLR())
		r.MethodFunc(http.MethodGet, "/dlr", apiHandlers.HandleDLR())
		r.MethodFunc(http.MethodPost, "/dlr", apiHandlers.HandleDLR())
		r.Group(func(r chi.Router) {
			r.Use(api.APIKeyAuth(pool))
			r.Post("/sms/send", apiHandlers.SendSMS())
			r.Get("/account/balance", apiHandlers.GetBalance())
			r.Get("/sms/status/{message_id}", apiHandlers.GetMessageStatus())
		})
	})
	addr := ":" + cfg.Port
	srv := &http.Server{Addr: addr, Handler: r, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		var err error
		if cfg.TLSEnabled {
			err = srv.ListenAndServeTLS(cfg.TLSCertFile, cfg.TLSKeyFile)
		} else {
			err = srv.ListenAndServe()
		}
		if err != nil && err != http.ErrServerClosed {
			log.Error("http server", "err", err, "tls_enabled", cfg.TLSEnabled)
			os.Exit(1)
		}
	}()
	scheme := "http"
	if cfg.TLSEnabled {
		scheme = "https"
	}
	log.Info("listening", "addr", addr, "scheme", scheme, "env", cfg.AppEnv, "version", version)
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	shCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_ = srv.Shutdown(shCtx)
}

func healthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"status":"ok","version":"` + version + `","commit":"` + commit + `","build_time":"` + buildTime + `"}`))
}

func newLogger(level string) *slog.Logger {
	var lv slog.Level
	switch level {
	case "debug":
		lv = slog.LevelDebug
	case "warn":
		lv = slog.LevelWarn
	case "error":
		lv = slog.LevelError
	default:
		lv = slog.LevelInfo
	}
	opts := &slog.HandlerOptions{Level: lv}
	if os.Getenv("APP_ENV") == "production" {
		return slog.New(slog.NewJSONHandler(os.Stdout, opts))
	}
	return slog.New(slog.NewTextHandler(os.Stdout, opts))
}

func findMigrationsDir() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	// If started from repo root, migrations live in ./migrations; if from minisms, ./migrations; if from cmd, ../migrations
	candidates := []string{
		filepath.Join(wd, "migrations"),
		filepath.Join(wd, "minisms", "migrations"),
		filepath.Join(wd, "..", "migrations"),
	}
	for _, c := range candidates {
		if st, e := os.Stat(c); e == nil && st.IsDir() {
			abs, e2 := filepath.Abs(c)
			if e2 == nil {
				return abs, nil
			}
		}
	}
	return "", os.ErrNotExist
}

func parseTemplateFS(fsys fs.FS, patterns ...string) (*template.Template, error) {
	name := "base"
	if len(patterns) > 0 {
		name = filepath.Base(patterns[0])
	}
	return template.New(name).Funcs(template.FuncMap{
		"hasPrefix": strings.HasPrefix,
	}).ParseFS(fsys, patterns...)
}
