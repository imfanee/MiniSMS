// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package web

import (
	"net/http"
	"net/url"
	"strings"
)

// UseForwardedHeaders sets r.Host (and r.URL scheme) from X-Forwarded-* when behind nginx TLS.
func UseForwardedHeaders() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if h := strings.TrimSpace(r.Header.Get("X-Forwarded-Host")); h != "" {
				r.Host = h
			}
			if p := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")); p != "" {
				r.URL.Scheme = p
			}
			next.ServeHTTP(w, r)
		})
	}
}

// csrfTrustedHosts normalizes CSRF_TRUSTED_ORIGINS entries to host[:port] as gorilla/csrf expects.
func csrfTrustedHosts(origins []string, extra ...string) []string {
	seen := make(map[string]struct{})
	var out []string
	add := func(h string) {
		h = strings.TrimSpace(h)
		if h == "" {
			return
		}
		if strings.Contains(h, "://") {
			if u, err := url.Parse(h); err == nil && u.Host != "" {
				h = u.Host
			}
		}
		h = strings.TrimSuffix(h, "/")
		if _, ok := seen[h]; ok {
			return
		}
		seen[h] = struct{}{}
		out = append(out, h)
	}
	for _, o := range origins {
		add(o)
	}
	for _, o := range extra {
		add(o)
	}
	return out
}
