package httpapi

import (
	"errors"
	"net"
	"net/http"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

var errRateLimited = errors.New("rate limit exceeded, try again later")

type ipRateLimiter struct {
	mu       sync.Mutex
	limiters map[string]*ipLimiterEntry
	r        rate.Limit
	b        int
}

type ipLimiterEntry struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

func newIPRateLimiter(r rate.Limit, b int) *ipRateLimiter {
	l := &ipRateLimiter{limiters: make(map[string]*ipLimiterEntry), r: r, b: b}
	go l.evictStaleLoop()
	return l
}

func (l *ipRateLimiter) allow(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	e, ok := l.limiters[ip]
	if !ok {
		e = &ipLimiterEntry{limiter: rate.NewLimiter(l.r, l.b)}
		l.limiters[ip] = e
	}
	e.lastSeen = time.Now()
	return e.limiter.Allow()
}

func (l *ipRateLimiter) evictStaleLoop() {
	const idleTTL = 10 * time.Minute
	ticker := time.NewTicker(idleTTL)
	defer ticker.Stop()
	for range ticker.C {
		l.mu.Lock()
		for ip, e := range l.limiters {
			if time.Since(e.lastSeen) > idleTTL {
				delete(l.limiters, ip)
			}
		}
		l.mu.Unlock()
	}
}

func rateLimit(limiter *ipRateLimiter, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !limiter.allow(clientIP(r)) {
			writeError(w, http.StatusTooManyRequests, errRateLimited)
			return
		}
		next(w, r)
	}
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
