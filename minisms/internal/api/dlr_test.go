// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/minisms/minisms/internal/config"
	"github.com/minisms/minisms/internal/db"
	"github.com/minisms/minisms/internal/dlr"
)

func TestHandleDLR_MissingMessageID(t *testing.T) {
	h := &Handlers{}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/dlr", strings.NewReader(`{"status":"DELIVRD"}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	h.HandleDLR().ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "DLR_ERR_INVALID_REQUEST") {
		t.Fatalf("expected error code in response, got: %s", rr.Body.String())
	}
}

func TestNormalizeDLRStatus_WithMap(t *testing.T) {
	row := &db.DLRMessage{
		CarrierDLRStatusField: strPtr("dlrstatus"),
		CarrierDLRStatusMap: map[string]string{
			"DELIVRD": "delivered",
			"UNDELIV": "undelivered",
		},
	}
	fields := map[string]string{"dlrstatus": "DELIVRD"}
	got := dlr.NormalizeFromFields(fields, row.CarrierDLRStatusField, row.CarrierDLRStatusMap)
	if got != "delivered" {
		t.Fatalf("expected delivered, got %s", got)
	}
}

func TestNormalizeDLRStatus_UnknownWhenMissing(t *testing.T) {
	row := &db.DLRMessage{CarrierDLRStatusField: strPtr("status")}
	fields := map[string]string{"other": "anything"}
	got := dlr.NormalizeFromFields(fields, row.CarrierDLRStatusField, row.CarrierDLRStatusMap)
	if got != "unknown" {
		t.Fatalf("expected unknown, got %s", got)
	}
}

func TestVerifyInboundDLRSecret(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	cfg := &config.Config{SecretKey: key}
	h := &Handlers{Config: cfg}
	enc, err := db.EncryptValue(key, "carrier-secret")
	if err != nil {
		t.Fatalf("encrypt failed: %v", err)
	}
	row := &db.DLRMessage{CarrierInboundSecret: &enc}

	reqBad := httptest.NewRequest(http.MethodGet, "/api/v1/dlr/00000000-0000-0000-0000-000000000000?secret=wrong", nil)
	if dlr.VerifyInboundSecret(h.Config.SecretKey, reqBad, row) {
		t.Fatalf("expected secret verification to fail")
	}
	reqGood := httptest.NewRequest(http.MethodGet, "/api/v1/dlr/00000000-0000-0000-0000-000000000000", nil)
	reqGood.Header.Set("X-DLR-Secret", "carrier-secret")
	if !dlr.VerifyInboundSecret(h.Config.SecretKey, reqGood, row) {
		t.Fatalf("expected secret verification to pass")
	}
}

func strPtr(s string) *string { return &s }
