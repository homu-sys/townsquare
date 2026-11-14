package web

import (
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const maxRateLimitEntries = 20000

type rateEntry struct {
	count int
	reset time.Time
}

type rateLimiter struct {
	mu        sync.Mutex
	limit     int
	window    time.Duration
	entries   map[string]*rateEntry
	lastSweep time.Time
}

func newRateLimiter(limit int, window time.Duration) *rateLimiter {
	return &rateLimiter{
		limit:     limit,
		window:    window,
		entries:   make(map[string]*rateEntry),
		lastSweep: time.Now(),
	}
}

func (l *rateLimiter) Allow(key string) (bool, time.Duration) {
	now := time.Now()

	l.mu.Lock()
	defer l.mu.Unlock()

	if now.Sub(l.lastSweep) >= l.window {
		for k, e := range l.entries {
			if now.After(e.reset) {
				delete(l.entries, k)
			}
		}
		l.lastSweep = now
	}

	e, ok := l.entries[key]
	if !ok || now.After(e.reset) {
		if !ok && len(l.entries) >= maxRateLimitEntries {
			l.entries = make(map[string]*rateEntry)
		}
		e = &rateEntry{reset: now.Add(l.window)}
		l.entries[key] = e
	}
	e.count++
	if e.count > l.limit {
		return false, time.Until(e.reset)
	}
	return true, 0
}

func (s *Server) clientIP(r *http.Request) string {
	if s.cfg.TrustProxy {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.Split(xff, ",")
			for i := len(parts) - 1; i >= 0; i-- {
				if ip := net.ParseIP(strings.TrimSpace(parts[i])); ip != nil {
					return ip.String()
				}
			}
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func retryAfter(d time.Duration) string {
	seconds := int(d.Seconds()) + 1
	if seconds < 1 {
		seconds = 1
	}
	return strconv.Itoa(seconds)
}

func (s *Server) limitHTML(l *rateLimiter, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ok, retry := l.Allow(s.clientIP(r))
		if !ok {
			w.Header().Set("Retry-After", retryAfter(retry))
			s.renderError(w, r, http.StatusTooManyRequests, "Too many attempts. Please wait and try again.")
			return
		}
		next(w, r)
	}
}

func (s *Server) limitAPI(l *rateLimiter, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ok, retry := l.Allow(s.clientIP(r))
		if !ok {
			w.Header().Set("Retry-After", retryAfter(retry))
			apiError(w, http.StatusTooManyRequests, "too many requests")
			return
		}
		next(w, r)
	}
}
