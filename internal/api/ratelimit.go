package api

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// maxRateWindow bounds how long a hit is remembered; it must be >= every
// window passed to rateLimiter methods.
const maxRateWindow = time.Hour

const rateSweepThreshold = 4096

// rateLimiter is a small in-memory sliding-window counter keyed by string.
type rateLimiter struct {
	mu   sync.Mutex
	hits map[string][]time.Time
	now  func() time.Time
}

func newRateLimiter() *rateLimiter {
	return &rateLimiter{hits: make(map[string][]time.Time), now: time.Now}
}

func (l *rateLimiter) prune(key string, window time.Duration) []time.Time {
	cutoff := l.now().Add(-window)
	recent := l.hits[key][:0]
	for _, t := range l.hits[key] {
		if t.After(cutoff) {
			recent = append(recent, t)
		}
	}
	if len(recent) == 0 {
		delete(l.hits, key)
		return nil
	}
	l.hits[key] = recent
	return recent
}

// count returns how many hits were recorded for key within window.
func (l *rateLimiter) count(key string, window time.Duration) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.prune(key, window))
}

// record stores a hit for key.
func (l *rateLimiter) record(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.recordLocked(key)
}

func (l *rateLimiter) recordLocked(key string) {
	if len(l.hits) >= rateSweepThreshold {
		for k := range l.hits {
			l.prune(k, maxRateWindow)
		}
	}
	l.hits[key] = append(l.hits[key], l.now())
}

// take records a hit and reports whether the caller is still within limit
// hits per window. Rejected calls are not recorded.
func (l *rateLimiter) take(key string, limit int, window time.Duration) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.prune(key, window)) >= limit {
		return false
	}
	l.recordLocked(key)
	return true
}

// clientIP returns the address used for rate limiting. Behind a reverse proxy
// (TRUST_PROXY=true) it is the right-most X-Forwarded-For entry, i.e. the
// address the trusted proxy itself saw; otherwise the TCP peer address.
func (s *Server) clientIP(r *http.Request) string {
	if s.cfg != nil && s.cfg.TrustProxy {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.Split(xff, ",")
			if ip := net.ParseIP(strings.TrimSpace(parts[len(parts)-1])); ip != nil {
				return ip.String()
			}
		}
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil && host != "" {
		return host
	}
	return r.RemoteAddr
}
