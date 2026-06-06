// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package carrier

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

// ValidateEndpointURL rejects SSRF-prone carrier dispatch targets (loopback, RFC1918, link-local, metadata).
func ValidateEndpointURL(raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return fmt.Errorf("invalid endpoint URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("endpoint URL scheme must be http or https")
	}
	host := strings.TrimSpace(u.Hostname())
	if host == "" {
		return fmt.Errorf("endpoint URL host required")
	}
	if blockedHostname(host) {
		return fmt.Errorf("endpoint URL host %q is not allowed", host)
	}
	if ip := net.ParseIP(host); ip != nil {
		if blockedIP(ip) {
			return fmt.Errorf("endpoint URL IP %s is not allowed", host)
		}
		return nil
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		return fmt.Errorf("endpoint URL host lookup failed: %w", err)
	}
	if len(ips) == 0 {
		return fmt.Errorf("endpoint URL host resolved to no addresses")
	}
	for _, ip := range ips {
		if blockedIP(ip) {
			return fmt.Errorf("endpoint URL host %q resolves to blocked IP %s", host, ip.String())
		}
	}
	return nil
}

func blockedHostname(host string) bool {
	h := strings.ToLower(strings.TrimSuffix(host, "."))
	switch h {
	case "localhost", "localhost.localdomain", "metadata", "metadata.google.internal":
		return true
	}
	return strings.HasSuffix(h, ".localhost") || strings.HasSuffix(h, ".local")
}

func blockedIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsPrivate() || ip.IsUnspecified() {
		return true
	}
	// AWS/GCP/Azure metadata
	if ip.Equal(net.ParseIP("169.254.169.254")) {
		return true
	}
	return false
}
