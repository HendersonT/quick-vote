package store

import (
	"errors"
	"path/filepath"
	"testing"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	st, err := Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func TestCreateAndGetVote(t *testing.T) {
	st := openTestStore(t)

	v := VoteRow{
		Slug:         "abc123",
		Title:        "Friday game night",
		Phase:        "suggesting",
		Settings:     `{"maxSuggestionsPerUser":3}`,
		CreatorToken: "creatortok",
		CreatedAt:    1000,
	}
	if err := st.CreateVote(v); err != nil {
		t.Fatalf("CreateVote: %v", err)
	}

	got, err := st.GetVote("abc123")
	if err != nil {
		t.Fatalf("GetVote: %v", err)
	}
	if got.Slug != v.Slug || got.Title != v.Title || got.Phase != v.Phase ||
		got.Settings != v.Settings || got.CreatorToken != v.CreatorToken ||
		got.CreatedAt != v.CreatedAt {
		t.Fatalf("roundtrip mismatch: got %+v, want %+v", got, v)
	}
	if got.PhaseDeadline != nil {
		t.Fatalf("expected nil PhaseDeadline, got %v", got.PhaseDeadline)
	}
	if got.Results != nil {
		t.Fatalf("expected nil Results, got %v", got.Results)
	}
}

func TestGetVoteNotFound(t *testing.T) {
	st := openTestStore(t)

	_, err := st.GetVote("nosuch")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestUpdateVote(t *testing.T) {
	st := openTestStore(t)

	v := VoteRow{
		Slug: "vote1", Title: "T", Phase: "suggesting",
		Settings: "{}", CreatorToken: "ct", CreatedAt: 1,
	}
	if err := st.CreateVote(v); err != nil {
		t.Fatalf("CreateVote: %v", err)
	}

	deadline := int64(5555)
	results := `{"winnerOptionId":"o1"}`
	v.Phase = "voting"
	v.PhaseDeadline = &deadline
	v.Results = &results
	if err := st.UpdateVote(v); err != nil {
		t.Fatalf("UpdateVote: %v", err)
	}

	got, err := st.GetVote("vote1")
	if err != nil {
		t.Fatalf("GetVote: %v", err)
	}
	if got.Phase != "voting" {
		t.Fatalf("phase not updated: %v", got.Phase)
	}
	if got.PhaseDeadline == nil || *got.PhaseDeadline != deadline {
		t.Fatalf("deadline not updated: %v", got.PhaseDeadline)
	}
	if got.Results == nil || *got.Results != results {
		t.Fatalf("results not updated: %v", got.Results)
	}

	// clear deadline/results back to nil
	v.PhaseDeadline = nil
	v.Results = nil
	v.Phase = "results"
	if err := st.UpdateVote(v); err != nil {
		t.Fatalf("UpdateVote clear: %v", err)
	}
	got, err = st.GetVote("vote1")
	if err != nil {
		t.Fatalf("GetVote: %v", err)
	}
	if got.PhaseDeadline != nil {
		t.Fatalf("expected nil deadline after clear, got %v", got.PhaseDeadline)
	}
	if got.Results != nil {
		t.Fatalf("expected nil results after clear, got %v", got.Results)
	}
}

func TestParticipants(t *testing.T) {
	st := openTestStore(t)

	v := VoteRow{Slug: "vote1", Title: "T", Phase: "suggesting", Settings: "{}", CreatorToken: "ct", CreatedAt: 1}
	if err := st.CreateVote(v); err != nil {
		t.Fatalf("CreateVote: %v", err)
	}

	p1 := ParticipantRow{ID: "p1", VoteSlug: "vote1", Name: "Alice", Token: "tok1", IsCreator: true, JoinedAt: 10}
	p2 := ParticipantRow{ID: "p2", VoteSlug: "vote1", Name: "Bob", Token: "tok2", IsCreator: false, JoinedAt: 20}
	if err := st.AddParticipant(p1); err != nil {
		t.Fatalf("AddParticipant p1: %v", err)
	}
	if err := st.AddParticipant(p2); err != nil {
		t.Fatalf("AddParticipant p2: %v", err)
	}

	list, err := st.Participants("vote1")
	if err != nil {
		t.Fatalf("Participants: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 participants, got %d", len(list))
	}
	if list[0].ID != "p1" || list[1].ID != "p2" {
		t.Fatalf("expected joined order p1,p2; got %v", list)
	}
	if !list[0].IsCreator {
		t.Fatalf("expected p1 to be creator")
	}

	got, err := st.ParticipantByToken("vote1", "tok2")
	if err != nil {
		t.Fatalf("ParticipantByToken: %v", err)
	}
	if got.ID != "p2" || got.Name != "Bob" {
		t.Fatalf("unexpected participant: %+v", got)
	}

	if _, err := st.ParticipantByToken("vote1", "nosuch"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for bad token, got %v", err)
	}
}

func TestWantsRevote(t *testing.T) {
	st := openTestStore(t)

	v := VoteRow{Slug: "vote1", Title: "T", Phase: "results", Settings: "{}", CreatorToken: "ct", CreatedAt: 1}
	if err := st.CreateVote(v); err != nil {
		t.Fatalf("CreateVote: %v", err)
	}
	p1 := ParticipantRow{ID: "p1", VoteSlug: "vote1", Name: "Alice", Token: "tok1", IsCreator: true, JoinedAt: 10}
	p2 := ParticipantRow{ID: "p2", VoteSlug: "vote1", Name: "Bob", Token: "tok2", JoinedAt: 20}
	if err := st.AddParticipant(p1); err != nil {
		t.Fatalf("AddParticipant: %v", err)
	}
	if err := st.AddParticipant(p2); err != nil {
		t.Fatalf("AddParticipant: %v", err)
	}

	if err := st.SetWantsRevote("p1", true); err != nil {
		t.Fatalf("SetWantsRevote: %v", err)
	}
	if err := st.SetWantsRevote("p2", true); err != nil {
		t.Fatalf("SetWantsRevote: %v", err)
	}

	list, err := st.Participants("vote1")
	if err != nil {
		t.Fatalf("Participants: %v", err)
	}
	for _, p := range list {
		if !p.WantsRevote {
			t.Fatalf("expected wants_revote true for %s", p.ID)
		}
	}

	if err := st.ResetRevotes("vote1"); err != nil {
		t.Fatalf("ResetRevotes: %v", err)
	}
	list, err = st.Participants("vote1")
	if err != nil {
		t.Fatalf("Participants: %v", err)
	}
	for _, p := range list {
		if p.WantsRevote {
			t.Fatalf("expected wants_revote false for %s after reset", p.ID)
		}
	}
}

func TestOptions(t *testing.T) {
	st := openTestStore(t)

	v := VoteRow{Slug: "vote1", Title: "T", Phase: "suggesting", Settings: "{}", CreatorToken: "ct", CreatedAt: 1}
	if err := st.CreateVote(v); err != nil {
		t.Fatalf("CreateVote: %v", err)
	}
	p1 := ParticipantRow{ID: "p1", VoteSlug: "vote1", Name: "Alice", Token: "tok1", IsCreator: true, JoinedAt: 10}
	p2 := ParticipantRow{ID: "p2", VoteSlug: "vote1", Name: "Bob", Token: "tok2", JoinedAt: 20}
	if err := st.AddParticipant(p1); err != nil {
		t.Fatalf("AddParticipant: %v", err)
	}
	if err := st.AddParticipant(p2); err != nil {
		t.Fatalf("AddParticipant: %v", err)
	}

	o1 := OptionRow{ID: "o1", VoteSlug: "vote1", ParticipantID: "p1", Title: "Catan", CreatedAt: 100}
	o2 := OptionRow{ID: "o2", VoteSlug: "vote1", ParticipantID: "p2", Title: "Wingspan", CreatedAt: 200}
	if err := st.AddOption(o1); err != nil {
		t.Fatalf("AddOption o1: %v", err)
	}
	if err := st.AddOption(o2); err != nil {
		t.Fatalf("AddOption o2: %v", err)
	}

	opts, err := st.Options("vote1")
	if err != nil {
		t.Fatalf("Options: %v", err)
	}
	if len(opts) != 2 || opts[0].ID != "o1" || opts[1].ID != "o2" {
		t.Fatalf("expected creation order o1,o2; got %v", opts)
	}

	// deleting someone else's option is a no-op error
	if err := st.DeleteOption("vote1", "o1", "p2"); err == nil {
		t.Fatalf("expected error deleting non-owned option")
	}
	opts, err = st.Options("vote1")
	if err != nil {
		t.Fatalf("Options: %v", err)
	}
	if len(opts) != 2 {
		t.Fatalf("expected option not deleted, got %d options", len(opts))
	}

	// deleting own option succeeds
	if err := st.DeleteOption("vote1", "o1", "p1"); err != nil {
		t.Fatalf("DeleteOption own: %v", err)
	}
	opts, err = st.Options("vote1")
	if err != nil {
		t.Fatalf("Options: %v", err)
	}
	if len(opts) != 1 || opts[0].ID != "o2" {
		t.Fatalf("expected only o2 remaining, got %v", opts)
	}
}

func TestBallots(t *testing.T) {
	st := openTestStore(t)

	v := VoteRow{Slug: "vote1", Title: "T", Phase: "voting", Settings: "{}", CreatorToken: "ct", CreatedAt: 1}
	if err := st.CreateVote(v); err != nil {
		t.Fatalf("CreateVote: %v", err)
	}
	p1 := ParticipantRow{ID: "p1", VoteSlug: "vote1", Name: "Alice", Token: "tok1", IsCreator: true, JoinedAt: 10}
	if err := st.AddParticipant(p1); err != nil {
		t.Fatalf("AddParticipant: %v", err)
	}

	if err := st.PutBallot("vote1", "p1", `{"o1":2,"o2":1}`); err != nil {
		t.Fatalf("PutBallot: %v", err)
	}
	// upsert overwrites
	if err := st.PutBallot("vote1", "p1", `{"o1":1}`); err != nil {
		t.Fatalf("PutBallot overwrite: %v", err)
	}

	ballots, err := st.Ballots("vote1")
	if err != nil {
		t.Fatalf("Ballots: %v", err)
	}
	if len(ballots) != 1 {
		t.Fatalf("expected 1 ballot, got %d", len(ballots))
	}
	pb, ok := ballots["p1"]
	if !ok {
		t.Fatalf("expected ballot for p1")
	}
	if len(pb) != 1 || pb["o1"] != 1 {
		t.Fatalf("expected overwritten ballot {o1:1}, got %v", pb)
	}

	if err := st.DeleteBallots("vote1"); err != nil {
		t.Fatalf("DeleteBallots: %v", err)
	}
	ballots, err = st.Ballots("vote1")
	if err != nil {
		t.Fatalf("Ballots after delete: %v", err)
	}
	if len(ballots) != 0 {
		t.Fatalf("expected no ballots after delete, got %v", ballots)
	}
}

func TestActiveDeadlines(t *testing.T) {
	st := openTestStore(t)

	withDeadline := int64(1234567890)
	rows := []VoteRow{
		{Slug: "with", Title: "t", Phase: "voting", Settings: "{}", CreatorToken: "c", CreatedAt: 1, PhaseDeadline: &withDeadline},
		{Slug: "without", Title: "t", Phase: "suggesting", Settings: "{}", CreatorToken: "c", CreatedAt: 1},
	}
	for _, v := range rows {
		if err := st.CreateVote(v); err != nil {
			t.Fatalf("CreateVote %s: %v", v.Slug, err)
		}
	}

	got, err := st.ActiveDeadlines()
	if err != nil {
		t.Fatalf("ActiveDeadlines: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 active deadline, got %d (%v)", len(got), got)
	}
	if got["with"] != withDeadline {
		t.Fatalf("deadline for 'with' = %d, want %d", got["with"], withDeadline)
	}
	if _, ok := got["without"]; ok {
		t.Fatalf("did not expect a deadline for 'without'")
	}
}
