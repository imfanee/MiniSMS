package carrier

import (
	"strconv"
	"strings"

	"github.com/nyaruka/phonenumbers"
)

type SMPPConfig struct {
	SourceAddrTON string
	SourceAddrNPI string
	DestAddrTON   string
	DestAddrNPI   string
}

type SMPPParams struct {
	SourceAddrTON int16
	SourceAddrNPI int16
	DestAddrTON   int16
	DestAddrNPI   int16
}

func ResolveTONNPI(cfg SMPPConfig, senderID, destination string) SMPPParams {
	out := SMPPParams{}
	out.SourceAddrTON = resolveTON(cfg.SourceAddrTON, senderID, true)
	out.SourceAddrNPI = resolveNPI(cfg.SourceAddrNPI, out.SourceAddrTON)
	out.DestAddrTON = resolveTON(cfg.DestAddrTON, destination, false)
	out.DestAddrNPI = resolveNPI(cfg.DestAddrNPI, out.DestAddrTON)
	return out
}

func resolveTON(v, value string, isSource bool) int16 {
	v = strings.TrimSpace(v)
	if v != "" && v != "dynamic" {
		if n, err := strconv.Atoi(v); err == nil {
			return int16(n)
		}
	}
	if isSource {
		s := strings.TrimSpace(value)
		if s == "" {
			return 5
		}
		if n, err := phonenumbers.Parse(s, "ZZ"); err == nil && phonenumbers.IsPossibleNumber(n) {
			return 1
		}
		if isDigitsOnly(s) {
			return 2
		}
		return 5
	}
	if n, err := phonenumbers.Parse(strings.TrimSpace(value), "ZZ"); err == nil && phonenumbers.IsPossibleNumber(n) {
		return 1
	}
	return 0
}

func resolveNPI(v string, ton int16) int16 {
	v = strings.TrimSpace(v)
	if v != "" && v != "dynamic" {
		if n, err := strconv.Atoi(v); err == nil {
			return int16(n)
		}
	}
	if ton == 1 || ton == 2 {
		return 1
	}
	return 0
}

func isDigitsOnly(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
