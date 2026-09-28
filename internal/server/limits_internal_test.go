package server

import (
	"net/http/httptest"
	"testing"
	"time"
)

func TestRateLimiterBurstAndRefill(t *testing.T) {
	now := time.Unix(1000, 0)
	l := newRateLimiter(3, time.Second)
	l.now = func() time.Time { return now }

	for i := 0; i < 3; i++ {
		if !l.allow("a") {
			t.Fatalf("request %d within burst was refused", i)
		}
	}
	if l.allow("a") {
		t.Fatal("request past burst was allowed")
	}
	if !l.allow("b") {
		t.Fatal("a different key should have its own bucket")
	}
	now = now.Add(1500 * time.Millisecond)
	if !l.allow("a") {
		t.Fatal("bucket should have refilled one token")
	}
	if l.allow("a") {
		t.Fatal("only one token should have refilled")
	}
}

func TestClientIPTrustedHeader(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "10.0.0.5:4321"
	r.Header.Set("CF-Connecting-IP", "203.0.113.9")

	if got := (&Server{}).clientIP(r); got != "10.0.0.5" {
		t.Fatalf("untrusted header must be ignored, got %q", got)
	}
	s := &Server{cfg: Config{TrustedIPHeader: "CF-Connecting-IP"}}
	if got := s.clientIP(r); got != "203.0.113.9" {
		t.Fatalf("trusted header not used, got %q", got)
	}
	r.Header.Set("CF-Connecting-IP", "6.6.6.6, 198.51.100.1")
	if got := s.clientIP(r); got != "198.51.100.1" {
		t.Fatalf("list header should yield the proxy-appended right-most entry, got %q", got)
	}
}

func TestCheckOrigin(t *testing.T) {
	s := &Server{cfg: Config{AllowedOrigins: []string{"http://localhost:5173"}}}
	cases := []struct {
		origin string
		want   bool
	}{
		{"", true},
		{"https://vote.example.com", true},
		{"http://localhost:5173", true},
		{"https://evil.example", false},
	}
	for _, c := range cases {
		r := httptest.NewRequest("GET", "https://vote.example.com/api/votes/x/ws", nil)
		if c.origin != "" {
			r.Header.Set("Origin", c.origin)
		}
		if got := s.checkOrigin(r); got != c.want {
			t.Errorf("origin %q: got %v, want %v", c.origin, got, c.want)
		}
	}
}

func TestHubConnectionCaps(t *testing.T) {
	h := newHub()
	for i := 0; i < maxWSPerIP; i++ {
		if !h.reserve("room", "1.2.3.4") {
			t.Fatalf("reserve %d refused below per-IP cap", i)
		}
	}
	if h.reserve("room", "1.2.3.4") {
		t.Fatal("reserve past per-IP cap allowed")
	}
	h.release("1.2.3.4")
	if !h.reserve("room", "1.2.3.4") {
		t.Fatal("released slot not reusable")
	}
}
