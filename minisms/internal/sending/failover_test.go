// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package sending

import "testing"

func TestBuildFailoverCarriers_DisabledUsesPrimaryOnly(t *testing.T) {
	f1 := "failover-1"
	f2 := "failover-2"
	route := &RouteEntry{
		PrimaryCarrierID:   "primary",
		Failover1CarrierID: &f1,
		Failover2CarrierID: &f2,
	}
	got := buildFailoverCarriers(route, false)
	if len(got) != 1 || got[0].id != "primary" || got[0].n != 0 {
		t.Fatalf("got %+v, want primary only", got)
	}
}

func TestBuildFailoverCarriers_EnabledIncludesConfigured(t *testing.T) {
	f1 := "failover-1"
	f2 := "failover-2"
	route := &RouteEntry{
		PrimaryCarrierID:   "primary",
		Failover1CarrierID: &f1,
		Failover2CarrierID: &f2,
	}
	got := buildFailoverCarriers(route, true)
	if len(got) != 3 {
		t.Fatalf("len=%d want 3", len(got))
	}
	if got[0].id != "primary" || got[1].id != f1 || got[2].id != f2 {
		t.Fatalf("got %+v", got)
	}
}

func TestBuildFailoverCarriers_EnabledSkipsEmptyFailoverIDs(t *testing.T) {
	empty := ""
	route := &RouteEntry{
		PrimaryCarrierID:   "primary",
		Failover1CarrierID: &empty,
		Failover2CarrierID: nil,
	}
	got := buildFailoverCarriers(route, true)
	if len(got) != 1 {
		t.Fatalf("got %+v", got)
	}
}
