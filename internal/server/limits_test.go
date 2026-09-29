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

// TestPruneUsesLastActivity pins the retention rule to last activity, not
// creation: a months-old vote that is still in use must survive a prune.
func TestPruneUsesLastActivity(t *testing.T) {
	s, fc := newFakeClockServer(t)
	_, out := doJSON(t, s, http.MethodPost, "/api/votes", map[string]any{"title": "Busy", "creatorName": "A"}, "")
	busy, busyTok := out["slug"].(string), out["sessionToken"].(string)
	_, out = doJSON(t, s, http.MethodPost, "/api/votes", map[string]any{"title": "Idle", "creatorName": "B"}, "")
	idle := out["slug"].(string)

	fc.Advance(100 * 24 * time.Hour)
	suggest(t, s, busy, busyTok, "fresh activity") // bumps last_activity via changed()

	n, err := s.PruneExpired(90 * 24 * time.Hour)
	if err != nil || n != 1 {
		t.Fatalf("pruned n=%d err=%v, want 1", n, err)
	}
	if rec, _ := doJSON(t, s, http.MethodGet, "/api/votes/"+idle, nil, ""); rec.Code != http.StatusNotFound {
		t.Fatalf("idle vote survived: %d", rec.Code)
	}
	if rec, _ := doJSON(t, s, http.MethodGet, "/api/votes/"+busy, nil, ""); rec.Code != http.StatusOK {
		t.Fatalf("active vote pruned: %d", rec.Code)
	}
}

// TestJoinCountsAsActivity: joining is a mutation like any other, so a vote
// someone just joined must not be pruned as idle.
func TestJoinCountsAsActivity(t *testing.T) {
	s, fc := newFakeClockServer(t)
	_, out := doJSON(t, s, http.MethodPost, "/api/votes", map[string]any{"title": "Late", "creatorName": "A"}, "")
	slug := out["slug"].(string)

	fc.Advance(100 * 24 * time.Hour)
	if rec, _ := doJSON(t, s, http.MethodPost, "/api/votes/"+slug+"/join", map[string]any{"name": "B"}, ""); rec.Code != http.StatusOK {
		t.Fatalf("join: %d", rec.Code)
	}
	if n, err := s.PruneExpired(90 * 24 * time.Hour); err != nil || n != 0 {
		t.Fatalf("pruned n=%d err=%v, want 0", n, err)
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
