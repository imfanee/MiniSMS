// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package routing

import (
	"errors"
	"strings"
)

type RouteEntry struct {
	Prefix string
	Status string
}

var ErrNoRoute = errors.New("SMS_ERR_NO_ROUTE")

func LongestPrefixMatch(entries []RouteEntry, destination string) (*RouteEntry, error) {
	d := strings.TrimPrefix(strings.TrimSpace(destination), "+")
	var digits strings.Builder
	for _, r := range d {
		if r >= '0' && r <= '9' {
			digits.WriteRune(r)
		}
	}
	dst := digits.String()
	var catchAll *RouteEntry
	var best *RouteEntry
	bestLen := -1
	for i := range entries {
		e := &entries[i]
		if e.Status != "active" {
			continue
		}
		if e.Prefix == "*" {
			if catchAll == nil {
				catchAll = e
			}
			continue
		}
		if strings.HasPrefix(dst, e.Prefix) && len(e.Prefix) > bestLen {
			best = e
			bestLen = len(e.Prefix)
		}
	}
	if best != nil {
		return best, nil
	}
	if catchAll != nil {
		return catchAll, nil
	}
	return nil, ErrNoRoute
}
