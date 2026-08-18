// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package web

import (
	"net/http"
	"testing"
)

func TestClientIPStringTrustedProxy(t *testing.T) {
	// Restore the default (loopback-only) set after the test.
	saved := trustedProxyNets
	defer func() { trustedProxyNets = saved }()
	trustedProxyNets = defaultTrustedProxyNets()

	req := func(remote, xff string) *http.Request {
		r := &http.Request{RemoteAddr: remote, Header: http.Header{}}
		if xff != "" {
			r.Header.Set("X-Forwarded-For", xff)
		}
		return r
	}

	cases := []struct {
		name   string
		remote string
		xff    string
		want   string
	}{
		{"direct client, no xff", "203.0.113.9:5555", "", "203.0.113.9"},
		{"direct client spoofing xff is ignored", "203.0.113.9:5555", "10.9.9.9", "203.0.113.9"},
		{"trusted loopback peer honors xff", "127.0.0.1:44012", "198.51.100.7", "198.51.100.7"},
		{"trusted peer, client-injected left hop ignored, rightmost wins", "127.0.0.1:44012", "1.2.3.4, 198.51.100.7", "198.51.100.7"},
		{"trusted peer, trailing trusted hop skipped", "127.0.0.1:44012", "198.51.100.7, 127.0.0.1", "198.51.100.7"},
		{"trusted peer, empty xff falls back to peer", "127.0.0.1:44012", "", "127.0.0.1"},
		{"no port in remoteaddr", "127.0.0.1", "198.51.100.7", "198.51.100.7"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ClientIPString(req(c.remote, c.xff)); got != c.want {
				t.Fatalf("ClientIPString(remote=%q xff=%q)=%q want %q", c.remote, c.xff, got, c.want)
			}
		})
	}
}

func TestSetTrustedProxiesExtendsButKeepsLoopback(t *testing.T) {
	saved := trustedProxyNets
	defer func() { trustedProxyNets = saved }()

	SetTrustedProxies([]string{"10.0.0.0/8"})

	// A configured proxy hop is now trusted, so its XFF is honored...
	r := &http.Request{RemoteAddr: "10.1.2.3:9000", Header: http.Header{}}
	r.Header.Set("X-Forwarded-For", "198.51.100.7")
	if got := ClientIPString(r); got != "198.51.100.7" {
		t.Fatalf("configured proxy: got %q want 198.51.100.7", got)
	}
	// ...and loopback stays trusted regardless of config.
	r2 := &http.Request{RemoteAddr: "127.0.0.1:9000", Header: http.Header{}}
	r2.Header.Set("X-Forwarded-For", "198.51.100.7")
	if got := ClientIPString(r2); got != "198.51.100.7" {
		t.Fatalf("loopback still trusted: got %q want 198.51.100.7", got)
	}
	// An untrusted peer still cannot spoof.
	r3 := &http.Request{RemoteAddr: "203.0.113.9:9000", Header: http.Header{}}
	r3.Header.Set("X-Forwarded-For", "10.0.0.1")
	if got := ClientIPString(r3); got != "203.0.113.9" {
		t.Fatalf("untrusted peer: got %q want 203.0.113.9", got)
	}
}
