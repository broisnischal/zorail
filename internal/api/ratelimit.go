package api

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// rateLimiter is a dependency-free, per-client token-bucket limiter. Each client
// (identified by API key when present, else source IP) gets a bucket that refills
// at rps tokens/second up to burst. Idle buckets are swept periodically so the
// map does not grow without bound.
type rateLimiter struct {
	rps   float64
	burst float64

	mu      sync.Mutex
	buckets map[string]*bucket
	lastGC  time.Time
}

type bucket struct {
	tokens float64
	last   time.Time
}

func newRateLimiter(rps float64, burst int) *rateLimiter {
	if burst < 1 {
		burst = 1
	}
	return &rateLimiter{
		rps:     rps,
		burst:   float64(burst),
		buckets: make(map[string]*bucket),
		lastGC:  time.Now(),
	}
}

// allow reports whether a request from key may proceed, consuming one token.
func (rl *rateLimiter) allow(key string) bool {
	now := time.Now()
	rl.mu.Lock()
	defer rl.mu.Unlock()

	if now.Sub(rl.lastGC) > 10*time.Minute {
		rl.gc(now)
		rl.lastGC = now
	}

	b := rl.buckets[key]
	if b == nil {
		b = &bucket{tokens: rl.burst, last: now}
		rl.buckets[key] = b
	}
	// Lazily refill based on elapsed time.
	b.tokens += now.Sub(b.last).Seconds() * rl.rps
	if b.tokens > rl.burst {
		b.tokens = rl.burst
	}
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// gc drops buckets that have fully refilled (idle clients).
func (rl *rateLimiter) gc(now time.Time) {
	for k, b := range rl.buckets {
		if b.tokens+now.Sub(b.last).Seconds()*rl.rps >= rl.burst {
			delete(rl.buckets, k)
		}
	}
}

// clientKey identifies the caller for rate-limiting: the bearer token if one was
// presented (so each API key gets its own budget), otherwise the source IP.
func clientKey(r *http.Request, trustProxy bool) string {
	if tok := bearer(r); tok != "" {
		return "k:" + tok
	}
	return "ip:" + clientIP(r, trustProxy)
}

func clientIP(r *http.Request, trustProxy bool) string {
	if trustProxy {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			if i := strings.IndexByte(xff, ','); i >= 0 {
				return strings.TrimSpace(xff[:i])
			}
			return strings.TrimSpace(xff)
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// limitAPI wraps next, rejecting over-budget /api requests with 429. Health
// checks are exempt so load balancers are never throttled. A nil limiter is a
// pass-through.
func (rl *rateLimiter) limitAPI(trustProxy bool, next http.Handler) http.Handler {
	if rl == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") && r.URL.Path != "/api/health" {
			if !rl.allow(clientKey(r, trustProxy)) {
				w.Header().Set("Retry-After", "1")
				writeError(w, http.StatusTooManyRequests, "rate limit exceeded")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
