// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package routing

import "testing"

func TestLongestPrefixMatch(t *testing.T) {
	tests := []struct {
		name        string
		destination string
		entries     []RouteEntry
		wantPrefix  string
		wantErr     bool
	}{
		{
			name:        "longer prefix wins",
			destination: "+447700123456",
			entries: []RouteEntry{
				{Prefix: "44", Status: "active"},
				{Prefix: "447", Status: "active"},
				{Prefix: "*", Status: "active"},
			},
			wantPrefix: "447",
		},
		{
			name:        "exact one prefix",
			destination: "+12125550100",
			entries: []RouteEntry{
				{Prefix: "1", Status: "active"},
				{Prefix: "*", Status: "active"},
			},
			wantPrefix: "1",
		},
		{
			name:        "catch all fallback",
			destination: "+999123",
			entries: []RouteEntry{
				{Prefix: "*", Status: "active"},
			},
			wantPrefix: "*",
		},
		{
			name:        "no match",
			destination: "+333123",
			entries: []RouteEntry{
				{Prefix: "44", Status: "active"},
			},
			wantErr: true,
		},
		{
			name:        "inactive skipped",
			destination: "+447",
			entries: []RouteEntry{
				{Prefix: "447", Status: "inactive"},
				{Prefix: "44", Status: "active"},
			},
			wantPrefix: "44",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := LongestPrefixMatch(tc.entries, tc.destination)
			if tc.wantErr {
				if err == nil || err != ErrNoRoute {
					t.Fatalf("expected ErrNoRoute, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if got == nil || got.Prefix != tc.wantPrefix {
				t.Fatalf("want prefix %q got %+v", tc.wantPrefix, got)
			}
		})
	}
}
