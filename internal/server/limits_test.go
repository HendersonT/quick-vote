package server_test

import (
	"fmt"
	"net/http"
	"testing"
	"time"
)

func TestJoinCapsParticipants(t *testing.T) {
	s := newTestServer(t)
	_, out := doJSON(t, s, http.MethodPost, "/api/votes", map[string]any{
		"title": "Cap", "creatorName": "Creator",
	}, "")
	slug := out["slug"].(string)

	// The creator is participant #1.
	for i := 1; i < 100; i++ {
		rec, _ := doJSON(t, s, http.MethodPost, "/api/votes/"+slug+"/join",
			map[string]any{"name": fmt.Sprintf("p%d", i)}, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("join %d: status %d, body %s", i, rec.Code, rec.Body.String())
		}
	}
	rec, _ := doJSON(t, s, http.MethodPost, "/api/votes/"+slug+"/join",
		map[string]any{"name": "one too many"}, "")
	if rec.Code != http.StatusConflict {
		t.Fatalf("join past cap: status %d, want 409", rec.Code)
	}
}

func TestCreateVoteRateLimited(t *testing.T) {
	s := newTestServer(t)
	var last int
	for i := 0; i < 11; i++ {
		rec, _ := doJSON(t, s, http.MethodPost, "/api/votes", map[string]any{
			"title": "Spam", "creatorName": "Bot",
		}, "")
		last = rec.Code
		if i < 10 && rec.Code != http.StatusCreated {
			t.Fatalf("create %d: status %d", i, rec.Code)
		}
	}
	if last != http.StatusTooManyRequests {
		t.Fatalf("11th create: status %d, want 429", last)
	}
}

func TestPruneExpired(t *testing.T) {
	s := newTestServer(t)
	_, out := doJSON(t, s, http.MethodPost, "/api/votes", map[string]any{
		"title": "Old", "creatorName": "A",
	}, "")
	slug := out["slug"].(string)
	doJSON(t, s, http.MethodPost, "/api/votes/"+slug+"/join", map[string]any{"name": "B"}, "")

	if n, err := s.PruneExpired(time.Hour); err != nil || n != 0 {
		t.Fatalf("fresh vote pruned: n=%d err=%v", n, err)
	}
	// Negative retention puts the cutoff in the future: everything expires.
	if n, err := s.PruneExpired(-time.Hour); err != nil || n != 1 {
		t.Fatalf("expired vote not pruned: n=%d err=%v", n, err)
	}
	rec, _ := doJSON(t, s, http.MethodGet, "/api/votes/"+slug, nil, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("pruned vote still served: status %d", rec.Code)
	}
}

func TestSecurityHeaders(t *testing.T) {
	s := newTestServer(t)
	rec, _ := doJSON(t, s, http.MethodGet, "/api/votes/nope", nil, "")
	for _, h := range []string{"Content-Security-Policy", "X-Content-Type-Options", "X-Frame-Options", "Referrer-Policy"} {
		if rec.Header().Get(h) == "" {
			t.Errorf("missing %s header", h)
		}
	}
}
