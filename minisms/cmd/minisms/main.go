// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package main

import (
	"context"
	"encoding/json"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/minisms/minisms"
	"github.com/minisms/minisms/internal/api"
	"github.com/minisms/minisms/internal/carrier"
	"github.com/minisms/minisms/internal/config"
	"github.com/minisms/minisms/internal/db"
	"github.com/minisms/minisms/internal/runtime"
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
	carrier.SetDispatchInsecureTLS(cfg.HTTPCarrierInsecureTLS)
	log := newLogger(cfg.LogLevel)

	pool, err := db.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Error("database", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	if err := db.EnsureBootstrapSuperAdmin(ctx, pool, cfg.AdminUsername, cfg.AdminPasswordHash); err != nil {
		log.Error("bootstrap admin", "err", err)
		os.Exit(1)
	}

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
	if err != nil {
		log.Error("template dashboard", "err", err)
		os.Exit(1)
	}
	dashFragT, err := parseTemplateFS(
		tfs,
		"templates/admin/dashboard_stats.html",
	)
	if err != nil {
		log.Error("template dashboard fragment", "err", err)
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
		"templates/admin/carriers/smpp_addressing_panel.html",
		"templates/admin/carriers/smpp_panel.html",
		"templates/admin/carriers/interconnect_panel.html",
		"templates/admin/carriers/http_interconnect_panel.html",
		"templates/admin/carriers/ledger_panel.html",
		"templates/admin/carriers/usage_panel.html",
		"templates/admin/invoices/panel.html",
		"templates/admin/shared/payment_reference_fields.html",
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
		"templates/admin/shared/payment_reference_fields.html",
		"templates/admin/clients/apikey_panel.html",
		"templates/admin/clients/apikey_display.html",
		"templates/admin/clients/smpp_panel.html",
		"templates/admin/invoices/panel.html",
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
	adminUsersListT, err := parseTemplateFS(
		tfs,
		"templates/layout/base.html",
		"templates/layout/partials/navbar.html",
		"templates/layout/partials/flash.html",
		"templates/admin/admin_users/list.html",
	)
	if err != nil {
		log.Error("template admin users list", "err", err)
		os.Exit(1)
	}
	adminUsersFormT, err := parseTemplateFS(
		tfs,
		"templates/layout/base.html",
		"templates/layout/partials/navbar.html",
		"templates/layout/partials/flash.html",
		"templates/admin/admin_users/form.html",
	)
	if err != nil {
		log.Error("template admin users form", "err", err)
		os.Exit(1)
	}
	currenciesT, err := parseTemplateFS(
		tfs,
		"templates/layout/base.html",
		"templates/layout/partials/navbar.html",
		"templates/layout/partials/flash.html",
		"templates/admin/currencies/list.html",
	)
	if err != nil {
		log.Error("template currencies", "err", err)
		os.Exit(1)
	}
	senderIDsT, err := parseTemplateFS(
		tfs,
		"templates/layout/base.html",
		"templates/layout/partials/navbar.html",
		"templates/layout/partials/flash.html",
		"templates/admin/sender_ids/list.html",
	)
	if err != nil {
		log.Error("template sender ids", "err", err)
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
		SettingsT:        settingsT,
		SettingsFragT:    settingsFragT,
		AdminUsersListT:  adminUsersListT,
		AdminUsersFormT:  adminUsersFormT,
		CurrenciesT:      currenciesT,
		SenderIDsT:       senderIDsT,
		T500:             t500,
	}
	app, err := runtime.Start(ctx, pool, cfg)
	if err != nil {
		log.Error("runtime start", "err", err)
		os.Exit(1)
	}
	defer app.Stop()
	h.RouteCache = app.Routes
	h.Send = app.Send

	apiHandlers := api.NewHandlers(pool, cfg, app.Egress, app.Send)
	if app.SMPPServer != nil {
		apiHandlers.DLR.SMPP = app.SMPPServer
	}
	r := chi.NewRouter()
	r.Use(web.WithRequestID)

	entryRedirect := web.AdminEntryRedirect(pool, cfg)
	r.Get("/", entryRedirect)
	r.Get("/admin", entryRedirect)
	r.Get("/healthz", healthzHandler(cfg, app))
	st, err := fs.Sub(minisms.StaticFS, "static")
	if err != nil {
		log.Error("static fs", "err", err)
		os.Exit(1)
	}
	r.Get("/static/*", http.StripPrefix("/static/", http.FileServer(http.FS(st))).ServeHTTP)

	r.Route("/admin", func(r chi.Router) {
		r.Use(web.UseForwardedHeaders())
		r.Use(web.CSRF(cfg))
		r.Get("/", entryRedirect)
		r.Get("/login", h.LoginGet())
		r.Post("/login", h.LoginPost())
		r.Post("/logout", h.Logout())
		r.Group(func(r chi.Router) {
			r.Use(web.SessionAuth(pool, cfg))
			r.Use(web.LoadAdminUserMiddleware(pool))
			web.RegisterProtectedAdminRoutes(r, h)
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
	addr := cfg.HTTPAddr()
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
	log.Info("listening",
		"addr", addr, "scheme", scheme, "env", cfg.AppEnv, "version", version,
		"smpp_ingress", cfg.SMPPServerEnabled, "smpp_listen", cfg.SMPPListenAddr,
	)
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Info("shutdown", "phase", "http")
	shCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_ = srv.Shutdown(shCtx)
	log.Info("shutdown", "phase", "smpp")
	app.Stop()
}

func healthzHandler(cfg *config.Config, app *runtime.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		payload := map[string]any{
			"status":     "ok",
			"version":    version,
			"commit":     commit,
			"build_time": buildTime,
			"smpp": map[string]any{
				"ingress_enabled": cfg.SMPPServerEnabled,
				"listen_addr":     cfg.SMPPListenAddr,
				"ingress_active":  app != nil && app.SMPPEnabled(),
				"tls":             cfg.SMPPTLSEnabled,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(payload)
	}
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

func parseTemplateFS(fsys fs.FS, patterns ...string) (*template.Template, error) {
	name := "base"
	if len(patterns) > 0 {
		name = filepath.Base(patterns[0])
	}
	return template.New(name).Funcs(web.TemplateFuncs()).ParseFS(fsys, patterns...)
}
