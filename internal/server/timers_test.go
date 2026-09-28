package server_test

import (
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/HendersonT/quick-vote/internal/server"
	"github.com/HendersonT/quick-vote/internal/store"
)

// TestRearmTimersFiresPastDueDeadline drives a vote into the voting phase with
// a deadline, then simulates a process restart (a brand-new Server on the same
// store) whose RearmTimers must pick up the persisted, now past-due deadline
// and advance the phase. Before the fix, the restarted server armed nothing and
// the vote stayed stuck in voting forever.
func TestRearmTimersFiresPastDueDeadline(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	s1 := server.New(st, nil)

	slug, creatorTok, aliceTok, _ := createVote(t, s1, map[string]any{
		"suggestAdvanceMode": "manual",
		"voteAdvanceMode":    "manual",
	})
	join(t, s1, slug, "Bob")

	if rec, _ := suggest(t, s1, slug, aliceTok, "Catan"); rec.Code != http.StatusOK {
		t.Fatalf("suggest Catan: %d", rec.Code)
	}
	if rec, _ := suggest(t, s1, slug, aliceTok, "Wingspan"); rec.Code != http.StatusOK {
		t.Fatalf("suggest Wingspan: %d", rec.Code)
	}
	// Advance suggesting -> voting.
	if rec, _ := doHdr(t, s1, http.MethodPost, "/api/votes/"+slug+"/advance", map[string]any{}, aliceTok, creatorTok); rec.Code != http.StatusOK {
		t.Fatalf("advance to voting: %d", rec.Code)
	}

	// Persist a past-due voting deadline as if the timer had been running when
	// the process died.
	v, err := st.GetVote(slug)
	if err != nil {
		t.Fatalf("get vote: %v", err)
	}
	past := time.Now().Add(-time.Second).Unix()
	v.PhaseDeadline = &past
	if err := st.UpdateVote(v); err != nil {
		t.Fatalf("update vote deadline: %v", err)
	}

	// Simulate restart: fresh server on the same store, then re-arm.
	s2 := server.New(st, nil)
	if err := s2.RearmTimers(); err != nil {
		t.Fatalf("RearmTimers: %v", err)
	}

	// The past-due deadline should fire promptly and advance voting -> results.
	deadline := time.Now().Add(2 * time.Second)
	for {
		cur, err := st.GetVote(slug)
		if err != nil {
			t.Fatalf("get vote: %v", err)
		}
		if cur.Phase == "results" {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("phase = %q, want results after re-armed timer fired", cur.Phase)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
