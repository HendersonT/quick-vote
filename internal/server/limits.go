package server

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Abuse limits for a public, account-less deployment. They are sized so a
// whole group sharing one IP (a party on the same Wi-Fi, a classroom behind
// NAT) never notices them, while a single client can't fill the disk or pin
// the server.
const (
	// maxParticipantsPerVote caps joins per room so a leaked link can't be
	// flooded with fake participants (which would also stall "everyone has
	// voted" auto-advance).
	maxParticipantsPerVote = 100

	// maxWSPerVote and maxWSPerIP cap concurrent live-update connections.
	// Every room change rebuilds one snapshot per connection, so unbounded
	// connections turn each mutation into unbounded database work.
	maxWSPerVote = 200
	maxWSPerIP   = 50
)

// rateLimiter is a per-key token bucket: each key holds up to burst tokens,
// refilled at one token per every interval. Idle buckets are swept so the
// map can't grow without bound.
type rateLimiter struct {
	mu        sync.Mutex
	burst     float64
	perSecond float64
	buckets   map[string]*bucket
	lastSweep time.Time
	now       func() time.Time
}

type bucket struct {
	tokens float64
	last   time.Time
}

func newRateLimiter(burst int, every time.Duration) *rateLimiter {
	return &rateLimiter{
		burst:     float64(burst),
		perSecond: 1 / every.Seconds(),
		buckets:   make(map[string]*bucket),
		now:       time.Now,
	}
}

// allow reports whether key may proceed, consuming one token if so.
func (l *rateLimiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	if now.Sub(l.lastSweep) > 10*time.Minute {
		l.sweep(now)
	}
	b, ok := l.buckets[key]
	if !ok {
		b = &bucket{tokens: l.burst, last: now}
		l.buckets[key] = b
	}
	b.tokens += now.Sub(b.last).Seconds() * l.perSecond
	if b.tokens > l.burst {
		b.tokens = l.burst
	}
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// sweep drops buckets that have refilled completely (indistinguishable from a
// fresh bucket). Caller holds l.mu.
func (l *rateLimiter) sweep(now time.Time) {
	full := time.Duration(l.burst / l.perSecond * float64(time.Second))
	for k, b := range l.buckets {
		if now.Sub(b.last) > full {
			delete(l.buckets, k)
		}
	}
	l.lastSweep = now
}

// limit returns middleware rejecting requests with 429 once the client's
// bucket in any of the given limiters is empty. A limiter keyed on the
// constant "" acts as a global limit.
func (s *Server) limit(perIP *rateLimiter, global *rateLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if perIP != nil && !perIP.allow(s.clientIP(r)) {
				writeError(w, http.StatusTooManyRequests, "too many requests, slow down")
				return
			}
			if global != nil && !global.allow("") {
				writeError(w, http.StatusTooManyRequests, "server is busy, try again later")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// clientIP returns the address used to key rate limits. Behind a reverse
// proxy every request arrives from the proxy, so Config.TrustedIPHeader
// (e.g. "CF-Connecting-IP") names a header the proxy sets. It must only be
// configured when the server is unreachable except through that proxy,
// otherwise clients can spoof it.
func (s *Server) clientIP(r *http.Request) string {
	if h := s.cfg.TrustedIPHeader; h != "" {
		if v := strings.TrimSpace(r.Header.Get(h)); v != "" {
			// X-Forwarded-For style lists: the right-most entry was appended
			// by the trusted proxy; anything left of it is client-supplied
			// and spoofable.
			if i := strings.LastIndexByte(v, ','); i >= 0 {
				v = strings.TrimSpace(v[i+1:])
			}
			return v
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// checkOrigin allows WebSocket upgrades from the page's own origin, from
// explicitly configured extra origins (e.g. the Vite dev server), and from
// non-browser clients that send no Origin header.
func (s *Server) checkOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	for _, o := range s.cfg.AllowedOrigins {
		if strings.EqualFold(origin, o) {
			return true
		}
	}
	if i := strings.Index(origin, "://"); i >= 0 {
		return strings.EqualFold(origin[i+3:], r.Host)
	}
	return false
}

// securityHeaders sets conservative browser hardening headers on every
// response. Referrer-Policy matters here: room links are the only access
// control, so they must never leak to other sites via the Referer header.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy",
			"default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; "+
				"img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; "+
				"base-uri 'none'; form-action 'self'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}
