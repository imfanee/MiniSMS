// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package carrier

import (
	"context"
	"testing"
)

func TestCheckCarrierEligibilityPolicyAnyEligible(t *testing.T) {
	sid, ok, reason, err := CheckCarrierEligibility(context.Background(), nil, "c1", "Carrier 1", "any", nil, nil, SenderIDResolution{Value: "BrandX", Source: "client_provided"}, "0.010000", "cli1", "447700")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok || reason != "" || sid != "BrandX" {
		t.Fatalf("unexpected result sid=%q ok=%v reason=%q", sid, ok, reason)
	}
}

func TestCheckCarrierEligibilityNumericPolicy(t *testing.T) {
	_, ok, reason, _ := CheckCarrierEligibility(context.Background(), nil, "c1", "Carrier 1", "numeric", nil, nil, SenderIDResolution{Value: "12345", Source: "client_provided"}, "0.010000", "cli1", "447700")
	if !ok || reason != "" {
		t.Fatalf("expected eligible numeric SID")
	}
	_, ok, reason, _ = CheckCarrierEligibility(context.Background(), nil, "c1", "Carrier 1", "numeric", nil, nil, SenderIDResolution{Value: "BrandX", Source: "client_provided"}, "0.010000", "cli1", "447700")
	if ok || reason != "sender_id_policy" {
		t.Fatalf("expected ineligible sender_id_policy, got ok=%v reason=%q", ok, reason)
	}
}

func TestCheckCarrierEligibilityE164Policy(t *testing.T) {
	_, ok, reason, _ := CheckCarrierEligibility(context.Background(), nil, "c1", "Carrier 1", "e164", nil, nil, SenderIDResolution{Value: "+447700900123", Source: "client_provided"}, "0.010000", "cli1", "447700")
	if !ok || reason != "" {
		t.Fatalf("expected eligible e164 SID")
	}
	_, ok, reason, _ = CheckCarrierEligibility(context.Background(), nil, "c1", "Carrier 1", "e164", nil, nil, SenderIDResolution{Value: "447700900123", Source: "client_provided"}, "0.010000", "cli1", "447700")
	if ok || reason != "sender_id_policy" {
		t.Fatalf("expected ineligible sender_id_policy, got ok=%v reason=%q", ok, reason)
	}
}

func TestCheckCarrierEligibilityListPolicyWithoutDBIneligible(t *testing.T) {
	_, ok, reason, err := CheckCarrierEligibility(context.Background(), nil, "c1", "Carrier 1", "list", nil, nil, SenderIDResolution{Value: "BrandX", Source: "client_provided"}, "0.010000", "cli1", "447700")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok || reason != "sender_id_policy" {
		t.Fatalf("expected list policy to be ineligible in test mode, got ok=%v reason=%q", ok, reason)
	}
}

