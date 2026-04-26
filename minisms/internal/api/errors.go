package api

import (
	"encoding/json"
	"net/http"
)

type errorBody struct {
	Error  string `json:"error"`
	Detail string `json:"detail,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeJSONError(w http.ResponseWriter, status int, code, detail string) {
	writeJSON(w, status, errorBody{
		Error:  code,
		Detail: detail,
	})
}
