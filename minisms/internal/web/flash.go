package web

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const (
	flashCookieName = "minisms_flash"
	flashMaxAge     = 120 // seconds; one page load
)

// Flash is a one-time message shown on the next HTML response, then cleared (signed cookie, consumed once).
type Flash struct {
	Message string `json:"m"`
	Type    string `json:"t"` // success, danger, warning, info
}

// SetFlash stores flash in a signed HttpOnly cookie (HMAC with app secret, consumed on read).
func SetFlash(w http.ResponseWriter, r *http.Request, baseURLPath string, secret []byte, secure bool, f *Flash) {
	if f == nil || f.Message == "" {
		return
	}
	b, _ := json.Marshal(f)
	issued := time.Now().UTC().Unix()
	mac := signFlash(secret, issued, b)
	payload := struct {
		Issued int64  `json:"i"`
		Body   []byte `json:"b"`
	}{Issued: issued, Body: b}
	raw, _ := json.Marshal(payload)
	token := base64.RawURLEncoding.EncodeToString(raw) + "." + base64.RawURLEncoding.EncodeToString(mac)
	c := &http.Cookie{
		Name:     flashCookieName,
		Value:    token,
		Path:     baseURLPath,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   flashMaxAge,
	}
	http.SetCookie(w, c)
}

// GetFlash returns flash from the cookie and clears the cookie. Nil if none / invalid.
func GetFlash(w http.ResponseWriter, r *http.Request, baseURLPath string, secret []byte, secure bool) *Flash {
	c, err := r.Cookie(flashCookieName)
	if err != nil || c.Value == "" {
		return nil
	}
	parts := strings.Split(c.Value, ".")
	if len(parts) != 2 {
		return nil
	}
	raw, err1 := base64.RawURLEncoding.DecodeString(parts[0])
	mac, err2 := base64.RawURLEncoding.DecodeString(parts[1])
	if err1 != nil || err2 != nil {
		return nil
	}
	var payload struct {
		Issued int64  `json:"i"`
		Body   []byte `json:"b"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil
	}
	if time.Since(time.Unix(payload.Issued, 0)) > 5*time.Minute {
		return nil
	}
	if !hmac.Equal(signFlash(secret, payload.Issued, payload.Body), mac) {
		return nil
	}
	var f Flash
	if err := json.Unmarshal(payload.Body, &f); err != nil {
		return nil
	}
	if f.Message == "" {
		return nil
	}
	clearFlash(w, r, baseURLPath, secure)
	return &f
}

func signFlash(secret []byte, issued int64, body []byte) []byte {
	m := hmac.New(sha256.New, secret)
	_, _ = m.Write([]byte("flash1"))
	_, _ = fmt.Fprintf(m, "%d", issued)
	_, _ = m.Write(body)
	return m.Sum(nil)
}

func clearFlash(w http.ResponseWriter, r *http.Request, path string, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     flashCookieName,
		Value:    "",
		Path:     path,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
	})
}
