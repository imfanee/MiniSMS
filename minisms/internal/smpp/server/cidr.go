// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package server

import (
	"net"
	"strings"
)

func remoteIP(addr net.Addr) net.IP {
	if addr == nil {
		return nil
	}
	host, _, err := net.SplitHostPort(addr.String())
	if err != nil {
		host = addr.String()
	}
	return net.ParseIP(host)
}

func cidrAllowed(addr net.Addr, allowed *string) bool {
	if allowed == nil || strings.TrimSpace(*allowed) == "" {
		return false
	}
	ip := remoteIP(addr)
	if ip == nil {
		return false
	}
	for _, part := range strings.Split(*allowed, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if strings.Contains(part, "/") {
			_, network, err := net.ParseCIDR(part)
			if err == nil && network.Contains(ip) {
				return true
			}
			continue
		}
		if pip := net.ParseIP(part); pip != nil && pip.Equal(ip) {
			return true
		}
	}
	return false
}
