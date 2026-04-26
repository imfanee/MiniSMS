package web

import (
	"html/template"
	"log/slog"
	"net/http"
	"strings"
)

// ServerError logs err and returns 500; renders html 500 for browser, plain for API.
func ServerError(w http.ResponseWriter, r *http.Request, err error, log *slog.Logger, t500 *template.Template) {
	if log != nil {
		log.Error("handler error", "err", err, "request_id", RequestIDFromContext(r.Context()))
	}
	accept := r.Header.Get("Accept")
	if (strings.Contains(accept, "text/html") || accept == "" || accept == "*/*") && t500 != nil {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusInternalServerError)
		if err2 := t500.Execute(w, nil); err2 != nil {
			if log != nil {
				log.Error("500 template", "err", err2)
			}
		}
		return
	}
	http.Error(w, "internal server error", http.StatusInternalServerError)
}
