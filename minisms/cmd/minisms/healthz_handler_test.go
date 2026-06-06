// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/minisms/minisms/internal/config"
)

func TestHealthz(t *testing.T) {
	cfg := &config.Config{SMPPServerEnabled: false, SMPPListenAddr: ":2775"}
	rr := httptest.NewRecorder()
	healthzHandler(cfg, nil)(rr, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("code %d", rr.Code)
	}
	var b struct {
		Status  string `json:"status"`
		Version string `json:"version"`
		SMPP    struct {
			IngressEnabled bool `json:"ingress_enabled"`
		} `json:"smpp"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &b); err != nil {
		t.Fatal(err)
	}
	if b.Status != "ok" || b.Version != "1.0.0" || b.SMPP.IngressEnabled {
		t.Fatalf("unexpected %+v", b)
	}
}
