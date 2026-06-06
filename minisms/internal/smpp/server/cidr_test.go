// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package server

import (
	"net"
	"testing"
)

func TestCIDRAllowedEmptyDenies(t *testing.T) {
	addr, _ := net.ResolveTCPAddr("tcp", "203.0.113.1:2775")
	if cidrAllowed(addr, nil) {
		t.Fatal("nil CIDR should deny")
	}
	empty := ""
	if cidrAllowed(addr, &empty) {
		t.Fatal("empty CIDR should deny")
	}
}

func TestCIDRAllowedMatch(t *testing.T) {
	addr, _ := net.ResolveTCPAddr("tcp", "203.0.113.50:2775")
	allowed := "203.0.113.0/24"
	if !cidrAllowed(addr, &allowed) {
		t.Fatal("expected allow in CIDR")
	}
}
