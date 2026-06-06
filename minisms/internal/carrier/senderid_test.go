// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package carrier

import (
	"context"
	"errors"
	"testing"

	"github.com/minisms/minisms/internal/db"
)

func TestResolveSenderIDAnyReturnsCarrierDefaultSignal(t *testing.T) {
	got, err := ResolveSenderID(context.Background(), nil, &db.Client{ClientID: "00000000-0000-0000-0000-000000000000"}, "any", "MiniSMS")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Source != "carrier_default" || got.Value != "MiniSMS" {
		t.Fatalf("unexpected resolution: %+v", got)
	}
}

func TestResolveSenderIDClientProvidedWithoutDBReturnsNotAllowed(t *testing.T) {
	_, err := ResolveSenderID(context.Background(), nil, &db.Client{ClientID: "00000000-0000-0000-0000-000000000000"}, "BrandX", "MiniSMS")
	if !errors.Is(err, ErrSenderNotAllowed) {
		t.Fatalf("expected ErrSenderNotAllowed, got %v", err)
	}
}

func TestResolveSenderIDEmptyUsesSystemDefaultInTestMode(t *testing.T) {
	got, err := ResolveSenderID(context.Background(), nil, &db.Client{ClientID: "00000000-0000-0000-0000-000000000000"}, "", "MiniSMS")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Source != "client_default" || got.Value != "MiniSMS" {
		t.Fatalf("unexpected resolution: %+v", got)
	}
}

