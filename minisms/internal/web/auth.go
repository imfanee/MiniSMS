// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package web

import (
	"encoding/hex"
	"html/template"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/csrf"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/minisms/minisms/internal/config"
	"github.com/minisms/minisms/internal/db"
	"github.com/minisms/minisms/internal/routecache"
	"github.com/minisms/minisms/internal/sending"
)

// Page is common template data for admin pages.
type Page struct {
	AdminView
	Title        string
	CurrentPath  string
	CSRFToken    string
	Flash        *Flash
	Carriers     int64
	RateGroups   int64
	Clients      int64
	SMSSentToday int64
}

// Handlers bundles HTTP handlers and dependencies.
type Handlers struct {
	Config        *config.Config
	Pool          *pgxpool.Pool
	RouteCache    *routecache.Cache
	Send          *sending.Service
	Log           *slog.Logger
	LoginT        *template.Template
	DashT         *template.Template
	SimulateT     *template.Template
	CarrListT     *template.Template
	CarrDetT      *template.Template
	CarrFragT     *template.Template
	RGListT       *template.Template
	RGDetT        *template.Template
	RGFragT       *template.Template
	ROGListT      *template.Template
	ROGDetT       *template.Template
	ROGFragT      *template.Template
	CLIListT      *template.Template
	CLIDetT       *template.Template
	CLIFragT      *template.Template
	DashFragT     *template.Template
	SMSLogT       *template.Template
	SMSLogFragT   *template.Template
	AuditT        *template.Template
	SettingsT        *template.Template
	SettingsFragT    *template.Template
	AdminUsersListT  *template.Template
	AdminUsersFormT  *template.Template
	CurrenciesT      *template.Template
	SenderIDsT       *template.Template
	T500             *template.Template
}

type loginAttempt struct {
	Count      int
	First      time.Time
	BlockedTil time.Time
}

var (
	loginMu       sync.Mutex
	loginAttempts = map[string]loginAttempt{}
)

const (
	loginWindow      = 10 * time.Minute
	loginMaxAttempts = 5
	loginBlockFor    = 15 * time.Minute
)

func loginThrottleKey(r *http.Request, username string) string {
	return strings.ToLower(strings.TrimSpace(username)) + "|" + ClientIPString(r)
}

func isLoginBlocked(key string, now time.Time) bool {
	loginMu.Lock()
	defer loginMu.Unlock()
	a, ok := loginAttempts[key]
	if !ok {
		return false
	}
	if !a.BlockedTil.IsZero() && now.Before(a.BlockedTil) {
		return true
	}
	if now.Sub(a.First) > loginWindow {
		delete(loginAttempts, key)
		return false
	}
	return false
}

func markLoginFailure(key string, now time.Time) {
	loginMu.Lock()
	defer loginMu.Unlock()
	a, ok := loginAttempts[key]
	if !ok || now.Sub(a.First) > loginWindow {
		a = loginAttempt{Count: 0, First: now}
	}
	a.Count++
	if a.Count >= loginMaxAttempts {
		a.BlockedTil = now.Add(loginBlockFor)
	}
	loginAttempts[key] = a
}

func clearLoginFailures(key string) {
	loginMu.Lock()
	defer loginMu.Unlock()
	delete(loginAttempts, key)
}

// LoginGet renders the login form.
func (h *Handlers) LoginGet() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Never allow credential-like query params to remain in URL/history.
		if r.URL.Query().Has("username") || r.URL.Query().Has("password") {
			http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
			return
		}
		f := GetFlash(w, r, "/", h.Config.SecretKey, h.Config.IsProduction())
		p := Page{Title: "Sign in", CurrentPath: "/admin/login", CSRFToken: csrf.Token(r), Flash: f}
		if err := h.LoginT.ExecuteTemplate(w, "base", p); err != nil {
			ServerError(w, r, err, h.Log, h.T500)
		}
	}
}

// LoginPost processes credentials and creates a session.
func (h *Handlers) LoginPost() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		username := strings.TrimSpace(r.FormValue("username"))
		password := r.FormValue("password")
		now := time.Now().UTC()
		throttleKey := loginThrottleKey(r, username)
		if isLoginBlocked(throttleKey, now) {
			p := Page{Title: "Sign in", CurrentPath: "/admin/login", CSRFToken: csrf.Token(r), Flash: &Flash{Type: "danger", Message: "Too many login attempts. Please try again later."}}
			w.WriteHeader(http.StatusTooManyRequests)
			if err := h.LoginT.ExecuteTemplate(w, "base", p); err != nil {
				ServerError(w, r, err, h.Log, h.T500)
			}
			return
		}
		adminUser, err := db.AuthenticateAdmin(r.Context(), h.Pool, username, password)
		if err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		if adminUser == nil {
			markLoginFailure(throttleKey, now)
			p := Page{Title: "Sign in", CurrentPath: "/admin/login", CSRFToken: csrf.Token(r), Flash: &Flash{Type: "danger", Message: "Invalid username or password"}}
			w.WriteHeader(http.StatusUnauthorized)
			if err := h.LoginT.ExecuteTemplate(w, "base", p); err != nil {
				ServerError(w, r, err, h.Log, h.T500)
			}
			return
		}
		clearLoginFailures(throttleKey)
		_ = db.TouchAdminUserLastLogin(r.Context(), h.Pool, adminUser.AdminUserID)
		raw, tokenHash, err := db.NewSessionToken()
		if err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		ip := ClientIPString(r)
		ua := r.UserAgent()
		var uap *string
		if ua != "" {
			uap = &ua
		}
		var ipPtr *string
		if ip != "" {
			ipPtr = &ip
		}
		idle := sessionIdle(r.Context(), h.Pool, h.Config)
		sessID, err := db.CreateAdminSession(r.Context(), h.Pool, adminUser.AdminUserID, tokenHash, idle, ipPtr, uap)
		if err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		name := adminUser.DisplayName
		if name == "" {
			name = adminUser.Username
		}
		h.recordAuditSession(r, &sessID, &adminUser.AdminUserID, "admin.login", "admin_user", &adminUser.AdminUserID, &name, map[string]string{
			"username": adminUser.Username,
		})
		http.SetCookie(w, &http.Cookie{
			Name:     SessionCookieName,
			Value:    hex.EncodeToString(raw[:]),
			Path:     "/",
			HttpOnly: true,
			Secure:   h.Config.IsProduction(),
			SameSite: http.SameSiteStrictMode,
			MaxAge:   int(idle / time.Second),
		})
		http.Redirect(w, r, "/admin/dashboard", http.StatusFound)
	}
}

// Logout revokes the session and clears the cookie, then redirects to login.
func (h *Handlers) Logout() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		raw, err := readSessionCookie(r)
		if err == nil {
			hash := db.HashTokenHex(raw)
			sess, _ := db.GetSessionByTokenHash(r.Context(), h.Pool, hash)
			if sess != nil {
				adminID := sess.AdminUserID
				h.recordAuditSession(r, &sess.SessionID, adminID, "admin.logout", "admin_session", &sess.SessionID, nil, nil)
			}
			_ = db.RevokeSession(r.Context(), h.Pool, hash)
		}
		http.SetCookie(w, &http.Cookie{
			Name:     SessionCookieName,
			Value:    "",
			Path:     "/",
			HttpOnly: true,
			Secure:   h.Config.IsProduction(),
			SameSite: http.SameSiteStrictMode,
			MaxAge:   -1,
		})
		SetFlash(w, r, "/", h.Config.SecretKey, h.Config.IsProduction(), &Flash{Type: "success", Message: "You are signed out"})
		http.Redirect(w, r, "/admin/login", http.StatusFound)
	}
}

// Dashboard renders the admin home with live counts.
func (h *Handlers) Dashboard() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		f := GetFlash(w, r, "/", h.Config.SecretKey, h.Config.IsProduction())
		ctx := r.Context()
		car, err1 := db.CountCarriers(ctx, h.Pool)
		rg, err2 := db.CountRateGroups(ctx, h.Pool)
		cl, err3 := db.CountClients(ctx, h.Pool)
		sms, err4 := db.CountSMSSentToday(ctx, h.Pool)
		if err := firstErr(err1, err2, err3, err4); err != nil {
			ServerError(w, r, err, h.Log, h.T500)
			return
		}
		p := Page{
			Title:        "Dashboard",
			CurrentPath:  r.URL.Path,
			CSRFToken:    csrf.Token(r),
			Flash:        f,
			Carriers:     car,
			RateGroups:   rg,
			Clients:      cl,
			SMSSentToday: sms,
		}
		if err := h.DashT.ExecuteTemplate(w, "base", p); err != nil {
			ServerError(w, r, err, h.Log, h.T500)
		}
	}
}

func firstErr(errs ...error) error {
	for _, e := range errs {
		if e != nil {
			return e
		}
	}
	return nil
}
