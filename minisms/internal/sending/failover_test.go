// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package sending

import (
	"context"
	"testing"
)

func strptr(s string) *string { return &s }

func TestOrderCarriers_FailoverDefault(t *testing.T) {
	s := &Service{rr: newRouteRR()}
	route := &RouteEntry{RouteEntryID: "r1", PrimaryCarrierID: "A",
		Failover1CarrierID: strptr("B"), Failover2CarrierID: strptr("C"), DistributionMode: "failover"}
	got := s.orderCarriers(context.Background(), route, true)
	if len(got) != 3 || got[0].id != "A" || got[1].id != "B" || got[2].id != "C" {
		t.Fatalf("failover order wrong: %+v", got)
	}
	if got[0].n != 0 || got[1].n != 1 || got[2].n != 2 {
		t.Fatalf("failover sequence numbers wrong: %+v", got)
	}
}

func TestOrderCarriers_FailoverDisabledPrimaryOnly(t *testing.T) {
	s := &Service{rr: newRouteRR()}
	route := &RouteEntry{RouteEntryID: "r1", PrimaryCarrierID: "A", Failover1CarrierID: strptr("B")}
	got := s.orderCarriers(context.Background(), route, false)
	if len(got) != 1 || got[0].id != "A" {
		t.Fatalf("failover disabled should be primary-only: %+v", got)
	}
}

func TestOrderLoadbalance_WeightedShare(t *testing.T) {
	s := &Service{rr: newRouteRR()}
	route := &RouteEntry{RouteEntryID: "r1", PrimaryCarrierID: "A", Failover1CarrierID: strptr("B"),
		DistributionMode: "loadbalance", PrimaryWeight: 7, Failover1Weight: 3}
	counts := map[string]int{}
	const N = 1000
	for i := 0; i < N; i++ {
		got := s.orderCarriers(context.Background(), route, true)
		if len(got) != 2 {
			t.Fatalf("loadbalance should keep both as chain: %+v", got)
		}
		counts[got[0].id]++ // first choice = the load-balanced pick
	}
	// 70/30 within a small tolerance.
	if counts["A"] < 660 || counts["A"] > 740 {
		t.Fatalf("weighted share off: A=%d B=%d (want ~700/300)", counts["A"], counts["B"])
	}
}

func TestOrderLoadbalance_ZeroWeightExcludedFromFirstChoice(t *testing.T) {
	s := &Service{rr: newRouteRR()}
	route := &RouteEntry{RouteEntryID: "r1", PrimaryCarrierID: "A", Failover1CarrierID: strptr("B"),
		DistributionMode: "loadbalance", PrimaryWeight: 0, Failover1Weight: 5}
	for i := 0; i < 50; i++ {
		got := s.orderCarriers(context.Background(), route, true)
		if got[0].id != "B" {
			t.Fatalf("zero-weight A must never be first choice: %+v", got)
		}
		if got[1].id != "A" {
			t.Fatalf("A must remain as failover: %+v", got)
		}
	}
}
