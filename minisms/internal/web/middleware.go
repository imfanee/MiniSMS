package web

import (
	"encoding/hex"
	"errors"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/csrf"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/minisms/minisms/internal/config"
	"github.com/minisms/minisms/internal/db"
)

const (
	SessionCookieName = "minisms_session"
)

// CSRF returns a gorilla/csrf middleware for /admin, configured from application settings.
func CSRF(cfg *config.Config) func(http.Handler) http.Handler {
	opts := []csrf.Option{
		csrf.Secure(cfg.IsProduction()),
		csrf.SameSite(csrf.SameSiteStrictMode),
		csrf.Path("/"),
		csrf.RequestHeader("X-CSRF-Token"),
	}
	return csrf.Protect(cfg.CSRFSigningKey, opts...)
}

// SessionAuth validates the session cookie and populates request context, or redirects to login.
func SessionAuth(pool *pgxpool.Pool, cfg *config.Config) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			now := time.Now()
			raw, err := readSessionCookie(r)
			if err != nil {
				redirectToLogin(w, r)
				return
			}
			hash := db.HashTokenHex(raw)
			sess, err := db.GetSessionByTokenHash(r.Context(), pool, hash)
			if err != nil {
				http.Error(w, "internal error", http.StatusInternalServerError)
				return
			}
			if sess == nil {
				redirectToLogin(w, r)
				return
			}
			if sess.ExpiresAt.Before(now) {
				redirectToLogin(w, r)
				return
			}
			if now.Sub(sess.LastActiveAt) > cfg.SessionIdle {
				redirectToLogin(w, r)
				return
			}
			if err := db.UpdateSessionLastActive(r.Context(), pool, sess.SessionID, cfg.SessionIdle); err != nil {
				if err == pgx.ErrNoRows {
					redirectToLogin(w, r)
					return
				}
				http.Error(w, "internal error", http.StatusInternalServerError)
				return
			}
			r = r.WithContext(WithSession(r.Context(), sess))
			next.ServeHTTP(w, r)
		})
	}
}

func readSessionCookie(r *http.Request) (raw [32]byte, err error) {
	c, err := r.Cookie(SessionCookieName)
	if err != nil {
		return raw, err
	}
	b, err := hex.DecodeString(c.Value)
	if err != nil {
		return raw, err
	}
	if len(b) != 32 {
		return raw, errInvalidSessionCookie
	}
	copy(raw[:], b)
	return raw, nil
}

var errInvalidSessionCookie = errors.New("invalid session cookie")

func redirectToLogin(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/admin/login", http.StatusFound)
}

// AdminEntryRedirect sends users to dashboard when session is valid, else login.
func AdminEntryRedirect(pool *pgxpool.Pool, cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		now := time.Now()
		raw, err := readSessionCookie(r)
		if err != nil {
			http.Redirect(w, r, "/admin/login", http.StatusFound)
			return
		}
		hash := db.HashTokenHex(raw)
		sess, err := db.GetSessionByTokenHash(r.Context(), pool, hash)
		if err != nil || sess == nil {
			http.Redirect(w, r, "/admin/login", http.StatusFound)
			return
		}
		if sess.ExpiresAt.Before(now) || now.Sub(sess.LastActiveAt) > cfg.SessionIdle {
			http.Redirect(w, r, "/admin/login", http.StatusFound)
			return
		}
		http.Redirect(w, r, "/admin/dashboard", http.StatusFound)
	}
}

// ClientIPString returns a client IP for storage (host part of RemoteAddr or first X-Forwarded-For hop).
func ClientIPString(r *http.Request) string {
	if h := r.Header.Get("X-Forwarded-For"); h != "" {
		p := strings.IndexByte(h, ',')
		if p > 0 {
			h = h[:p]
		}
		return strings.TrimSpace(h)
	}
	h, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return h
}
