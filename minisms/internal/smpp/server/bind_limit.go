// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package server

import (
	"strings"
	"sync"
	"time"
)

type bindAttempt struct {
	Count      int
	First      time.Time
	BlockedTil time.Time
}

var (
	bindMu       sync.Mutex
	bindAttempts = map[string]bindAttempt{}
)

const (
	bindWindow      = 10 * time.Minute
	bindMaxAttempts = 5
	bindBlockFor    = 15 * time.Minute
)

func bindThrottleKey(remoteHost, systemID string) string {
	return strings.ToLower(strings.TrimSpace(systemID)) + "|" + strings.TrimSpace(remoteHost)
}

func isBindBlocked(key string, now time.Time) bool {
	bindMu.Lock()
	defer bindMu.Unlock()
	a, ok := bindAttempts[key]
	if !ok {
		return false
	}
	if !a.BlockedTil.IsZero() && now.Before(a.BlockedTil) {
		return true
	}
	if now.Sub(a.First) > bindWindow {
		delete(bindAttempts, key)
		return false
	}
	return false
}

func markBindFailure(key string, now time.Time) {
	bindMu.Lock()
	defer bindMu.Unlock()
	a, ok := bindAttempts[key]
	if !ok || now.Sub(a.First) > bindWindow {
		a = bindAttempt{Count: 0, First: now}
	}
	a.Count++
	if a.Count >= bindMaxAttempts {
		a.BlockedTil = now.Add(bindBlockFor)
	}
	bindAttempts[key] = a
}

func clearBindFailures(key string) {
	bindMu.Lock()
	defer bindMu.Unlock()
	delete(bindAttempts, key)
}
