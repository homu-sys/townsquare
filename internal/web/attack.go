package web

import (
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	autoLockdownWindow = time.Minute
	offenderTTL        = 15 * time.Minute
	maxFloodOffenders  = 20000
)

var autoLockdownThreshold = 100

type floodGuard struct {
	mu          sync.Mutex
	strikes     int
	windowStart time.Time
	offenders   map[string]time.Time
}

func newFloodGuard() *floodGuard {
	return &floodGuard{
		windowStart: time.Now(),
		offenders:   make(map[string]time.Time),
	}
}

func (s *Server) noteRateLimited(ip string) {
	now := time.Now()

	s.flood.mu.Lock()
	if now.Sub(s.flood.windowStart) >= autoLockdownWindow {
		s.flood.windowStart = now
		s.flood.strikes = 0
	}
	s.flood.strikes++
	if len(s.flood.offenders) >= maxFloodOffenders {
		s.flood.offenders = make(map[string]time.Time)
	}
	s.flood.offenders[ip] = now
	tripped := s.flood.strikes >= autoLockdownThreshold
	s.flood.mu.Unlock()

	if !tripped || s.lockdown.Load() {
		return
	}
	if err := s.enableLockdown(); err != nil {
		log.Printf("attack mode: %v", err)
		return
	}
	log.Printf("attack mode enabled automatically after %d rate-limited requests", autoLockdownThreshold)
}

func (s *Server) isFloodOffender(ip string) bool {
	s.flood.mu.Lock()
	defer s.flood.mu.Unlock()
	at, ok := s.flood.offenders[ip]
	return ok && time.Since(at) < offenderTTL
}

func (s *Server) resetFlood() {
	s.flood.mu.Lock()
	s.flood.strikes = 0
	s.flood.windowStart = time.Now()
	s.flood.offenders = make(map[string]time.Time)
	s.flood.mu.Unlock()
}

func (s *Server) enableLockdown() error {
	if err := s.store.SetLockdown(true); err != nil {
		return err
	}
	s.lockdown.Store(true)
	return nil
}

func (s *Server) disableLockdown() error {
	if err := s.store.SetLockdown(false); err != nil {
		return err
	}
	s.lockdown.Store(false)
	s.resetFlood()
	return nil
}

func (s *Server) attackMode(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.lockdown.Load() || s.attackAllowed(r) {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Retry-After", "3600")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			apiError(w, http.StatusServiceUnavailable, "this website is under attack")
			return
		}
		page := s.newPage(r)
		page.Heading = "This website is under attack."
		page.Error = "This website is under attack. Only previously recorded visitors may enter."
		page.NoIndex = true
		s.render(w, r, http.StatusServiceUnavailable, "error.html", page)
	})
}

func (s *Server) attackAllowed(r *http.Request) bool {
	p := r.URL.Path
	if p == "/healthz" || p == "/favicon.ico" || p == "/federation/latest.json" ||
		strings.HasPrefix(p, "/static/") {
		return true
	}
	if u := userFrom(r); u != nil && u.IsStaff() {
		return true
	}
	ip := s.clientIP(r)
	if ip == "127.0.0.1" || ip == "::1" {
		return true
	}
	if s.isFloodOffender(ip) {
		return false
	}
	ok, err := s.store.IsRecordedVisitor(ip)
	if err != nil {
		log.Printf("attack mode: visitor check: %v", err)
		return false
	}
	return ok
}
