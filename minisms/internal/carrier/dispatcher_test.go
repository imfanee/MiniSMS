package carrier

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestDispatchToCarrierSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`ok`))
	}))
	defer srv.Close()

	res, err := DispatchToCarrier(DispatchRequest{
		Method:      http.MethodPost,
		EndpointURL: srv.URL,
		ContentType: "application/json",
		Body:        `{"x":1}`,
		Timeout:     2 * time.Second,
	})
	if err != nil {
		t.Fatalf("dispatch failed: %v", err)
	}
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 got %d", res.StatusCode)
	}
}

func TestDispatchToCarrierServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`fail`))
	}))
	defer srv.Close()

	res, err := DispatchToCarrier(DispatchRequest{
		Method:      http.MethodPost,
		EndpointURL: srv.URL,
		ContentType: "application/json",
		Body:        `{"x":1}`,
		Timeout:     2 * time.Second,
	})
	if err != nil {
		t.Fatalf("dispatch failed: %v", err)
	}
	if res.StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected 500 got %d", res.StatusCode)
	}
}

func TestDispatchToCarrierTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(120 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	_, err := DispatchToCarrier(DispatchRequest{
		Method:      http.MethodPost,
		EndpointURL: srv.URL,
		ContentType: "application/json",
		Body:        `{"x":1}`,
		Timeout:     20 * time.Millisecond,
	})
	if err == nil {
		t.Fatalf("expected timeout error")
	}
}
