// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package server

import (
	"net"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

type bindMode int

const (
	bindTX bindMode = iota
	bindRX
	bindTRX
)

func (m bindMode) String() string {
	switch m {
	case bindTX:
		return "tx"
	case bindRX:
		return "rx"
	default:
		return "trx"
	}
}

func (m bindMode) canSubmit() bool  { return m == bindTX || m == bindTRX }
func (m bindMode) canDeliver() bool { return m == bindRX || m == bindTRX }

type session struct {
	clientID string
	mode     bindMode
	remote   net.Addr
	conn     *conn
	limiter  *rate.Limiter
}

type sessionRegistry struct {
	mu       sync.Mutex
	wg       sync.WaitGroup
	byClient map[string]int
	sessions map[*session]struct{}
	deliver  map[string][]*session
}

func newSessionRegistry() *sessionRegistry {
	return &sessionRegistry{
		byClient: make(map[string]int),
		sessions: make(map[*session]struct{}),
		deliver:  make(map[string][]*session),
	}
}

func (r *sessionRegistry) add(s *session) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sessions[s] = struct{}{}
	r.byClient[s.clientID]++
	if s.mode.canDeliver() {
		r.deliver[s.clientID] = append(r.deliver[s.clientID], s)
	}
}

// remove drops a session and reports whether it was present (so callers log the
// lifecycle event exactly once even if remove races with another path).
func (r *sessionRegistry) remove(s *session) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.sessions[s]; !ok {
		return false
	}
	delete(r.sessions, s)
	if r.byClient[s.clientID] > 0 {
		r.byClient[s.clientID]--
	}
	if r.byClient[s.clientID] == 0 {
		delete(r.byClient, s.clientID)
	}
	list := r.deliver[s.clientID]
	for i, x := range list {
		if x == s {
			r.deliver[s.clientID] = append(list[:i], list[i+1:]...)
			break
		}
	}
	if len(r.deliver[s.clientID]) == 0 {
		delete(r.deliver, s.clientID)
	}
	return true
}

// closeClient closes every connection bound for a client. The handleConn loops
// then exit on read error and remove their own sessions via the disconnect path.
func (r *sessionRegistry) closeClient(clientID string) {
	r.mu.Lock()
	var conns []*conn
	for s := range r.sessions {
		if s.clientID == clientID && s.conn != nil {
			conns = append(conns, s.conn)
		}
	}
	r.mu.Unlock()
	for _, c := range conns {
		_ = c.Close()
	}
}

func (r *sessionRegistry) bindCount(clientID string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.byClient[clientID]
}

func (r *sessionRegistry) pickDeliver(clientID string) *session {
	r.mu.Lock()
	defer r.mu.Unlock()
	list := r.deliver[clientID]
	if len(list) == 0 {
		return nil
	}
	return list[0]
}

func (r *sessionRegistry) trackConn() {
	r.wg.Add(1)
}

func (r *sessionRegistry) untrackConn() {
	r.wg.Done()
}

// closeAll closes every bound session connection.
func (r *sessionRegistry) closeAll() {
	r.mu.Lock()
	sessions := make([]*session, 0, len(r.sessions))
	for s := range r.sessions {
		sessions = append(sessions, s)
	}
	r.mu.Unlock()
	for _, s := range sessions {
		if s.conn != nil {
			_ = s.conn.Close()
		}
	}
}

// wait blocks until all handleConn goroutines finish or timeout elapses.
func (r *sessionRegistry) wait(timeout time.Duration) {
	done := make(chan struct{})
	go func() {
		r.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(timeout):
	}
}
