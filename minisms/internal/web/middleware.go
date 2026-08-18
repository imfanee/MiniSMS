// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package web

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
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
	var extra []string
	if !cfg.IsProduction() {
		extra = append(extra,
			fmt.Sprintf("127.0.0.1:%s", cfg.Port),
			fmt.Sprintf("localhost:%s", cfg.Port),
			fmt.Sprintf("[::1]:%s", cfg.Port),
			"staging.example.com:18080",
		)
	}
	if hosts := csrfTrustedHosts(cfg.CSRFTrustedOrigins, extra...); len(hosts) > 0 {
		opts = append(opts, csrf.TrustedOrigins(hosts))
	}
	return csrf.Protect(cfg.CSRFSigningKey, opts...)
}

// sessionIdle returns admin session idle timeout: system_settings.admin_session_idle_minutes when valid, else cfg default.
func sessionIdle(ctx context.Context, pool *pgxpool.Pool, cfg *config.Config) time.Duration {
	v := db.Setting(ctx, pool, "admin_session_idle_minutes", "")
	if v == "" {
		return cfg.SessionIdle
	}
	mins, err := strconv.Atoi(v)
	if err != nil || mins < 1 {
		return cfg.SessionIdle
	}
	return time.Duration(mins) * time.Minute
}

// SessionAuth validates the session cookie and populates request context, or redirects to login.
func SessionAuth(pool *pgxpool.Pool, cfg *config.Config) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			now := time.Now()
			idle := sessionIdle(r.Context(), pool, cfg)
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
			if now.Sub(sess.LastActiveAt) > idle {
				redirectToLogin(w, r)
				return
			}
			if err := db.UpdateSessionLastActive(r.Context(), pool, sess.SessionID, idle); err != nil {
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
		idle := sessionIdle(r.Context(), pool, cfg)
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
		if sess.ExpiresAt.Before(now) || now.Sub(sess.LastActiveAt) > idle {
			http.Redirect(w, r, "/admin/login", http.StatusFound)
			return
		}
		http.Redirect(w, r, "/admin/dashboard", http.StatusFound)
	}
}

// trustedProxyNets is the set of peer networks whose X-Forwarded-For header we honor. It starts at
// loopback and is extended from TRUSTED_PROXIES at startup (SetTrustedProxies). Loopback stays trusted
// because the app runs behind a same-host reverse proxy.
var trustedProxyNets = defaultTrustedProxyNets()

func defaultTrustedProxyNets() []*net.IPNet {
	nets, _ := parseCIDRList([]string{"127.0.0.0/8", "::1/128"})
	return nets
}

// SetTrustedProxies extends the trusted-proxy set with the configured CIDRs/IPs (bare IPs become /32 or
// /128). Loopback always remains trusted. Called once at startup from config; invalid or empty input
// leaves the loopback-only default in place.
func SetTrustedProxies(entries []string) {
	if extra, _ := parseCIDRList(entries); len(extra) > 0 {
		trustedProxyNets = append(defaultTrustedProxyNets(), extra...)
	}
}

func parseCIDRList(entries []string) ([]*net.IPNet, bool) {
	var out []*net.IPNet
	saw := false
	for _, e := range entries {
		e = strings.TrimSpace(e)
		if e == "" {
			continue
		}
		saw = true
		if _, n, err := net.ParseCIDR(e); err == nil {
			out = append(out, n)
			continue
		}
		if ip := net.ParseIP(e); ip != nil {
			bits := 32
			if ip.To4() == nil {
				bits = 128
			}
			out = append(out, &net.IPNet{IP: ip, Mask: net.CIDRMask(bits, bits)})
		}
	}
	return out, saw
}

func isTrustedProxy(ip net.IP) bool {
	for _, n := range trustedProxyNets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// ClientIPString returns the client IP for throttling/audit. X-Forwarded-For is honored ONLY when the
// direct socket peer is a trusted proxy; otherwise the peer is the client and any XFF it sent is
// ignored, so a directly-connecting client cannot spoof its source IP. When the peer is trusted, the
// real client is the first address from the right of the XFF list that is not itself a trusted proxy
// (the reverse proxy appends the true client on the right, so left entries can be attacker-supplied).
func ClientIPString(r *http.Request) string {
	peer := r.RemoteAddr
	if h, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		peer = h
	}
	peerIP := net.ParseIP(peer)
	if peerIP != nil && isTrustedProxy(peerIP) {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.Split(xff, ",")
			for i := len(parts) - 1; i >= 0; i-- {
				cand := strings.TrimSpace(parts[i])
				ip := net.ParseIP(cand)
				if ip == nil {
					continue
				}
				if isTrustedProxy(ip) {
					continue // a hop inside our own proxy chain; keep walking left
				}
				return cand // first untrusted address from the right is the real client
			}
		}
	}
	return peer
}
