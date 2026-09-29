package server

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/HendersonT/quick-vote/internal/store"
)

func TestBroadcastQueryCountIndependentOfConnections(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	s := New(st, nil)
	if err := st.CreateVote(store.VoteRow{Slug: "r", Title: "R", Phase: "suggesting", Settings: `{"creditsPerOption":3,"maxSuggestionsPerUser":3,"suggestAdvanceMode":"manual","voteAdvanceMode":"manual","tiebreaker":"most-backers","revoteThresholdPct":33}`, CreatorToken: "c", CreatedAt: 1}); err != nil {
		t.Fatal(err)
	}
	if err := st.AddParticipant(store.ParticipantRow{ID: "p1", VoteSlug: "r", Name: "Ann", Token: "tok-1", IsCreator: true, JoinedAt: 1}); err != nil {
		t.Fatal(err)
	}
	// Register fake connections directly in the hub (no network needed).
	// Alternate participant and spectator connections so per-connection
	// requester resolution is exercised too.
	var conns []*wsConn
	register := func(n int) {
		for i := 0; i < n; i++ {
			c := &wsConn{send: make(chan []byte, 64), slug: "r", done: make(chan struct{})}
			if len(conns)%2 == 0 {
				c.token = "tok-1"
			}
			conns = append(conns, c)
			s.hub.add(c)
		}
	}
	register(1)
	before := st.QueryCount()
	s.broadcast("r")
	one := st.QueryCount() - before

	register(49)
	before = st.QueryCount()
	s.broadcast("r")
	fifty := st.QueryCount() - before

	// One room load: the vote, its participants, options and ballots.
	if one != 4 || fifty != 4 {
		t.Fatalf("broadcast queries: 1 conn=%d, 50 conns=%d, want 4 each (one room load, not per connection)", one, fifty)
	}

	// Every connection still gets its own personalized snapshot.
	for i, c := range conns {
		var msg []byte
		for len(c.send) > 0 {
			msg = <-c.send
		}
		if msg == nil {
			t.Fatalf("conn %d received no snapshot", i)
		}
		var state map[string]any
		if err := json.Unmarshal(msg, &state); err != nil {
			t.Fatal(err)
		}
		you, _ := state["you"].(map[string]any)
		if c.token == "tok-1" && (you == nil || you["participantId"] != "p1") {
			t.Fatalf("conn %d (participant) got you=%v", i, state["you"])
		}
		if c.token == "" && state["you"] != nil {
			t.Fatalf("conn %d (spectator) got you=%v", i, state["you"])
		}
	}
}

func TestRoomDataRequesterConstantTimeMatch(t *testing.T) {
	d := roomData{parts: []store.ParticipantRow{{ID: "a", Token: "tok-a"}, {ID: "b", Token: "tok-b"}}}
	if p := d.requester("tok-b"); p == nil || p.ID != "b" {
		t.Fatalf("requester(tok-b) = %v", p)
	}
	if d.requester("") != nil || d.requester("nope") != nil {
		t.Fatal("empty/unknown token must resolve to spectator")
	}
	// The returned pointer is a copy: mutating it must not alter the
	// shared participant list other connections are built from.
	d.requester("tok-a").Name = "mutated"
	if d.parts[0].Name == "mutated" {
		t.Fatal("requester must return a copy, not a pointer into parts")
	}
}
