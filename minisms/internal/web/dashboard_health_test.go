// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package web

import (
	"strings"
	"testing"
)

func TestHealthPct(t *testing.T) {
	cases := []struct {
		ok, bad int64
		want    float64
	}{
		{0, 0, 100},  // no traffic -> 100 (nothing failing)
		{10, 0, 100}, // all good
		{0, 10, 0},   // all bad
		{3, 1, 75},   // mixed
		{9, 1, 90},   // exactly at floor
	}
	for _, c := range cases {
		if got := healthPct(c.ok, c.bad); got != c.want {
			t.Errorf("healthPct(%d,%d)=%v want %v", c.ok, c.bad, got, c.want)
		}
	}
}

func TestBalanceBelow(t *testing.T) {
	cases := []struct {
		bal    string
		thresh float64
		want   bool
	}{
		{"5.00", 10, true},
		{"10.00", 10, false}, // equal is not below
		{"10.0001", 10, false},
		{"-2.5", 10, true},          // overdrafted carrier
		{"  3 ", 10, true},          // trimmed
		{"not-a-number", 10, false}, // parse error -> not low
		{"0", 1, true},
	}
	for _, c := range cases {
		if got := balanceBelow(c.bal, c.thresh); got != c.want {
			t.Errorf("balanceBelow(%q,%v)=%v want %v", c.bal, c.thresh, got, c.want)
		}
	}
}

func TestCarrierState(t *testing.T) {
	cases := []struct {
		name       string
		in         CarrierHealth
		wantState  HealthState
		wantReason string // substring expected in reasons ("" = no check)
	}{
		{"inactive is down", CarrierHealth{Status: "inactive"}, HealthDown, "carrier inactive"},
		{"smpp no binds is down", CarrierHealth{Status: "active", IsSMPP: true, BindsKnown: true, BindsReady: 0, BindsTotal: 8}, HealthDown, "no SMPP binds"},
		{"smpp partial binds warns", CarrierHealth{Status: "active", IsSMPP: true, BindsKnown: true, BindsReady: 5, BindsTotal: 8, SuccessPct1h: 100}, HealthWarn, "partial binds 5/8"},
		{"low balance warns", CarrierHealth{Status: "active", BalanceLow: true, SuccessPct1h: 100}, HealthWarn, "low balance"},
		{"unmatched dlr warns", CarrierHealth{Status: "active", UnmatchedDLR: 3, SuccessPct1h: 100}, HealthWarn, "unmatched DLRs"},
		{"low success with enough sample warns", CarrierHealth{Status: "active", Sent1h: 100, SuccessPct1h: 40}, HealthWarn, "1h success"},
		{"low success but tiny sample stays ok", CarrierHealth{Status: "active", Sent1h: 5, SuccessPct1h: 40}, HealthOK, ""},
		{"healthy full binds ok", CarrierHealth{Status: "active", IsSMPP: true, BindsKnown: true, BindsReady: 8, BindsTotal: 8, SuccessPct1h: 100}, HealthOK, ""},
		{"http healthy ok", CarrierHealth{Status: "active", IsSMPP: false, SuccessPct1h: 100}, HealthOK, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			gotState, reasons := carrierState(c.in)
			if gotState != c.wantState {
				t.Fatalf("state=%v want %v (reasons=%v)", gotState, c.wantState, reasons)
			}
			if c.wantReason != "" && !strings.Contains(strings.Join(reasons, "; "), c.wantReason) {
				t.Fatalf("reasons %v missing %q", reasons, c.wantReason)
			}
		})
	}
}

func TestClientState(t *testing.T) {
	cases := []struct {
		name      string
		in        ClientHealth
		wantState HealthState
	}{
		{"suspended is down", ClientHealth{Status: "suspended"}, HealthDown},
		{"disabled is down", ClientHealth{Status: "disabled"}, HealthDown},
		{"low balance warns", ClientHealth{Status: "active", BalanceLow: true, SuccessPct1h: 100}, HealthWarn},
		{"low success enough sample warns", ClientHealth{Status: "active", Sent1h: 50, SuccessPct1h: 20}, HealthWarn},
		{"low success tiny sample ok", ClientHealth{Status: "active", Sent1h: 3, SuccessPct1h: 20}, HealthOK},
		{"healthy ok", ClientHealth{Status: "active", SuccessPct1h: 100}, HealthOK},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got, _ := clientState(c.in); got != c.wantState {
				t.Fatalf("state=%v want %v", got, c.wantState)
			}
		})
	}
}

func TestHumanDurationS(t *testing.T) {
	cases := []struct {
		s    int64
		want string
	}{
		{0, "-"}, {-5, "-"}, {30, "30s"}, {90, "1m"}, {3600, "1h"}, {90000, "1d"},
	}
	for _, c := range cases {
		if got := humanDurationS(c.s); got != c.want {
			t.Errorf("humanDurationS(%d)=%q want %q", c.s, got, c.want)
		}
	}
}
