package main

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	rlMaxFailures = 5
	rlWindow      = 15 * time.Minute
)

type ipState struct {
	failures  int
	windowEnd time.Time
}

// RateLimiter limits login attempts per source IP.
type RateLimiter struct {
	mu      sync.Mutex
	entries map[string]*ipState
}

func newRateLimiter(ctx context.Context) *RateLimiter {
	rl := &RateLimiter{entries: make(map[string]*ipState)}
	go rl.sweep(ctx)
	return rl
}

// allow returns true if the IP may attempt a login.
func (rl *RateLimiter) allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	e := rl.entries[ip]
	if e == nil || time.Now().After(e.windowEnd) {
		return true
	}
	return e.failures < rlMaxFailures
}

// recordFailure increments the failure count for the IP.
func (rl *RateLimiter) recordFailure(ip string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	now := time.Now()
	e := rl.entries[ip]
	if e == nil || now.After(e.windowEnd) {
		rl.entries[ip] = &ipState{failures: 1, windowEnd: now.Add(rlWindow)}
		return
	}
	e.failures++
}

// recordSuccess clears the failure count on a successful login.
func (rl *RateLimiter) recordSuccess(ip string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	delete(rl.entries, ip)
}

// sweep removes stale entries periodically and stops when ctx is cancelled.
func (rl *RateLimiter) sweep(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			now := time.Now()
			rl.mu.Lock()
			for ip, e := range rl.entries {
				if now.After(e.windowEnd) {
					delete(rl.entries, ip)
				}
			}
			rl.mu.Unlock()
		}
	}
}

// clientIP returns the best-guess client IP.
// When trustProxy is true it reads X-Forwarded-For (set by a trusted front-end
// proxy such as nginx or Traefik). Enable via TRUST_PROXY_HEADERS=true.
// When false (the default) RemoteAddr is used directly, which is safe for
// deployments where the server is directly exposed.
func clientIP(r *http.Request, trustProxy bool) string {
	if trustProxy {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			ip := xff
			if i := strings.IndexByte(xff, ','); i != -1 {
				ip = xff[:i]
			}
			return strings.TrimSpace(ip)
		}
	}
	addr := r.RemoteAddr
	if i := strings.LastIndexByte(addr, ':'); i != -1 {
		return addr[:i]
	}
	return addr
}
