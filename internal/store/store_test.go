package store

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

// oldSchema is the votes/participants/options/ballots schema as it existed
// before the "advanced options" round added participants.done_suggesting and
// votes.active_options. It backs TestMigrateAddsColumnsToExistingDB, which
// simulates opening a database created by an older build of quick-vote.
const oldSchema = `
CREATE TABLE IF NOT EXISTS votes (
  slug TEXT PRIMARY KEY, title TEXT NOT NULL,
  phase TEXT NOT NULL DEFAULT 'suggesting',
  settings TEXT NOT NULL, creator_token TEXT NOT NULL,
  phase_deadline INTEGER, results TEXT, created_at INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS participants (
  id TEXT PRIMARY KEY, vote_slug TEXT NOT NULL REFERENCES votes(slug),
  name TEXT NOT NULL, token TEXT NOT NULL UNIQUE,
  is_creator INTEGER NOT NULL DEFAULT 0,
  wants_revote INTEGER NOT NULL DEFAULT 0, joined_at INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS options (
  id TEXT PRIMARY KEY, vote_slug TEXT NOT NULL REFERENCES votes(slug),
  participant_id TEXT NOT NULL REFERENCES participants(id),
  title TEXT NOT NULL, created_at INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS ballots (
  vote_slug TEXT NOT NULL REFERENCES votes(slug),
  participant_id TEXT NOT NULL REFERENCES participants(id),
  votes TEXT NOT NULL, updated_at INTEGER NOT NULL,
  PRIMARY KEY (vote_slug, participant_id));
`

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
	if got.ActiveOptions != nil {
		t.Fatalf("expected nil ActiveOptions, got %v", *got.ActiveOptions)
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

func TestUpdateVoteActiveOptions(t *testing.T) {
	st := openTestStore(t)

	v := VoteRow{
		Slug: "vote1", Title: "T", Phase: "voting",
		Settings: "{}", CreatorToken: "ct", CreatedAt: 1,
	}
	if err := st.CreateVote(v); err != nil {
		t.Fatalf("CreateVote: %v", err)
	}
	got, err := st.GetVote("vote1")
	if err != nil {
		t.Fatalf("GetVote: %v", err)
	}
	if got.ActiveOptions != nil {
		t.Fatalf("expected nil ActiveOptions on creation, got %v", *got.ActiveOptions)
	}

	// A runoff round sets active_options to the tied option IDs.
	active := `["o1","o2"]`
	v.ActiveOptions = &active
	if err := st.UpdateVote(v); err != nil {
		t.Fatalf("UpdateVote: %v", err)
	}
	got, err = st.GetVote("vote1")
	if err != nil {
		t.Fatalf("GetVote: %v", err)
	}
	if got.ActiveOptions == nil || *got.ActiveOptions != active {
		t.Fatalf("ActiveOptions = %v, want %q", got.ActiveOptions, active)
	}

	// Transitions back to "all options active" clear it to NULL.
	v.ActiveOptions = nil
	if err := st.UpdateVote(v); err != nil {
		t.Fatalf("UpdateVote clear: %v", err)
	}
	got, err = st.GetVote("vote1")
	if err != nil {
		t.Fatalf("GetVote: %v", err)
	}
	if got.ActiveOptions != nil {
		t.Fatalf("expected nil ActiveOptions after clear, got %v", *got.ActiveOptions)
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
	if list[0].DoneSuggesting || list[1].DoneSuggesting {
		t.Fatalf("expected done_suggesting false by default, got %+v", list)
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

func TestSetDoneSuggesting(t *testing.T) {
	st := openTestStore(t)

	v := VoteRow{Slug: "vote1", Title: "T", Phase: "suggesting", Settings: "{}", CreatorToken: "ct", CreatedAt: 1}
	if err := st.CreateVote(v); err != nil {
		t.Fatalf("CreateVote: %v", err)
	}
	p1 := ParticipantRow{ID: "p1", VoteSlug: "vote1", Name: "Alice", Token: "tok1", IsCreator: true, JoinedAt: 10}
	p2 := ParticipantRow{ID: "p2", VoteSlug: "vote1", Name: "Bob", Token: "tok2", JoinedAt: 20}
	if err := st.AddParticipant(p1); err != nil {
		t.Fatalf("AddParticipant p1: %v", err)
	}
	if err := st.AddParticipant(p2); err != nil {
		t.Fatalf("AddParticipant p2: %v", err)
	}

	if err := st.SetDoneSuggesting("p1", true); err != nil {
		t.Fatalf("SetDoneSuggesting: %v", err)
	}

	got, err := st.ParticipantByToken("vote1", "tok1")
	if err != nil {
		t.Fatalf("ParticipantByToken: %v", err)
	}
	if !got.DoneSuggesting {
		t.Fatalf("expected p1 done_suggesting = true")
	}

	list, err := st.Participants("vote1")
	if err != nil {
		t.Fatalf("Participants: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 participants, got %d", len(list))
	}
	for _, p := range list {
		want := p.ID == "p1"
		if p.DoneSuggesting != want {
			t.Fatalf("participant %s DoneSuggesting = %v, want %v", p.ID, p.DoneSuggesting, want)
		}
	}

	// toggling back off works too
	if err := st.SetDoneSuggesting("p1", false); err != nil {
		t.Fatalf("SetDoneSuggesting off: %v", err)
	}
	got, err = st.ParticipantByToken("vote1", "tok1")
	if err != nil {
		t.Fatalf("ParticipantByToken: %v", err)
	}
	if got.DoneSuggesting {
		t.Fatalf("expected p1 done_suggesting = false after toggle off")
	}

	if err := st.SetDoneSuggesting("nosuch", true); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for unknown participant, got %v", err)
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

	// DeleteOptionAny ignores ownership but stays scoped to the vote.
	if err := st.DeleteOptionAny("other-vote", "o2"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("DeleteOptionAny wrong vote: %v, want ErrNotFound", err)
	}
	if err := st.DeleteOptionAny("vote1", "o2"); err != nil {
		t.Fatalf("DeleteOptionAny: %v", err)
	}
	if err := st.DeleteOptionAny("vote1", "o2"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("DeleteOptionAny missing: %v, want ErrNotFound", err)
	}
	if opts, _ = st.Options("vote1"); len(opts) != 0 {
		t.Fatalf("expected no options left, got %v", opts)
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

// TestMigrateAddsColumnsToExistingDB opens a database built with the
// pre-advanced-options schema (no done_suggesting/active_options columns)
// directly, seeds it with a row through those old columns only, then opens
// it through Store.Open and verifies the new columns were added additively
// and are usable (existing rows get the documented defaults, not an error).
func TestMigrateAddsColumnsToExistingDB(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")

	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open raw db: %v", err)
	}
	if _, err := raw.Exec(oldSchema); err != nil {
		t.Fatalf("apply old schema: %v", err)
	}
	if _, err := raw.Exec(
		`INSERT INTO votes (slug, title, phase, settings, creator_token, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		"legacy-vote", "Old vote", "suggesting", "{}", "ct", 1,
	); err != nil {
		t.Fatalf("seed legacy vote: %v", err)
	}
	if _, err := raw.Exec(
		`INSERT INTO participants (id, vote_slug, name, token, is_creator, wants_revote, joined_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"legacy-p1", "legacy-vote", "Alice", "tok1", 1, 0, 10,
	); err != nil {
		t.Fatalf("seed legacy participant: %v", err)
	}
	if err := raw.Close(); err != nil {
		t.Fatalf("close raw db: %v", err)
	}

	// Sanity check: the old columns really are absent before migration, so
	// this test would fail loudly (rather than silently passing) if the
	// fixture schema above ever drifted to already include them.
	verify, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("reopen raw db: %v", err)
	}
	hasColumn := func(table, column string) bool {
		rows, err := verify.Query(`PRAGMA table_info(` + table + `)`)
		if err != nil {
			t.Fatalf("PRAGMA table_info(%s): %v", table, err)
		}
		defer rows.Close()
		for rows.Next() {
			var cid int
			var name, ctype string
			var notNull, pk int
			var dflt any
			if err := rows.Scan(&cid, &name, &ctype, &notNull, &dflt, &pk); err != nil {
				t.Fatalf("scan table_info: %v", err)
			}
			if name == column {
				return true
			}
		}
		return false
	}
	if hasColumn("participants", "done_suggesting") {
		t.Fatalf("fixture already has done_suggesting; test no longer exercises the migration")
	}
	if hasColumn("votes", "active_options") {
		t.Fatalf("fixture already has active_options; test no longer exercises the migration")
	}
	if err := verify.Close(); err != nil {
		t.Fatalf("close verify db: %v", err)
	}

	st, err := Open(path)
	if err != nil {
		t.Fatalf("Open (should migrate additively): %v", err)
	}
	defer st.Close()

	v, err := st.GetVote("legacy-vote")
	if err != nil {
		t.Fatalf("GetVote after migration: %v", err)
	}
	if v.ActiveOptions != nil {
		t.Fatalf("expected migrated active_options to default to NULL, got %v", *v.ActiveOptions)
	}

	parts, err := st.Participants("legacy-vote")
	if err != nil {
		t.Fatalf("Participants after migration: %v", err)
	}
	if len(parts) != 1 {
		t.Fatalf("expected 1 participant, got %d", len(parts))
	}
	if parts[0].DoneSuggesting {
		t.Fatalf("expected migrated done_suggesting to default to false")
	}

	// The migrated column must be fully usable, not just present.
	if err := st.SetDoneSuggesting("legacy-p1", true); err != nil {
		t.Fatalf("SetDoneSuggesting on migrated column: %v", err)
	}
	got, err := st.ParticipantByToken("legacy-vote", "tok1")
	if err != nil {
		t.Fatalf("ParticipantByToken after migration: %v", err)
	}
	if !got.DoneSuggesting {
		t.Fatalf("expected done_suggesting = true after SetDoneSuggesting")
	}

	// Re-opening an already-migrated database must be a no-op, not an error
	// (ALTER TABLE ADD COLUMN would fail if attempted a second time).
	if err := st.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	st2, err := Open(path)
	if err != nil {
		t.Fatalf("second Open on already-migrated db: %v", err)
	}
	defer st2.Close()
}

func TestMigrationAddsPublishRoundColumns(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	// Pre-round schema: the July tables without the new columns.
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
CREATE TABLE votes (slug TEXT PRIMARY KEY, title TEXT NOT NULL, phase TEXT NOT NULL DEFAULT 'suggesting',
  settings TEXT NOT NULL, creator_token TEXT NOT NULL, phase_deadline INTEGER, results TEXT,
  created_at INTEGER NOT NULL, active_options TEXT);
CREATE TABLE participants (id TEXT PRIMARY KEY, vote_slug TEXT NOT NULL REFERENCES votes(slug),
  name TEXT NOT NULL, token TEXT NOT NULL UNIQUE, is_creator INTEGER NOT NULL DEFAULT 0,
  wants_revote INTEGER NOT NULL DEFAULT 0, joined_at INTEGER NOT NULL, done_suggesting INTEGER NOT NULL DEFAULT 0);
INSERT INTO votes (slug,title,settings,creator_token,created_at) VALUES ('old','T','{}','ct',500);
INSERT INTO participants (id,vote_slug,name,token,joined_at) VALUES ('p1','old','A','tok',500);`); err != nil {
		t.Fatal(err)
	}
	db.Close()

	st, err := Open(path)
	if err != nil {
		t.Fatalf("open migrated: %v", err)
	}
	defer st.Close()
	v, err := st.GetVote("old")
	if err != nil {
		t.Fatal(err)
	}
	if v.LastActivity != 500 {
		t.Fatalf("last_activity backfill = %d, want created_at 500", v.LastActivity)
	}
	if v.ClosedAt != nil || v.NextSlug != nil || v.NextCreatorToken != nil {
		t.Fatalf("new nullable columns should be nil: %+v", v)
	}
	ps, err := st.Participants("old")
	if err != nil || len(ps) != 1 || ps[0].NextToken != nil {
		t.Fatalf("participants after migration: %+v err=%v", ps, err)
	}
}

func TestCreateVoteDefaultsLastActivity(t *testing.T) {
	st := openTestStore(t)
	if err := st.CreateVote(VoteRow{Slug: "a", Title: "A", Phase: "suggesting", Settings: "{}", CreatorToken: "c", CreatedAt: 42}); err != nil {
		t.Fatal(err)
	}
	v, err := st.GetVote("a")
	if err != nil {
		t.Fatal(err)
	}
	if v.LastActivity != 42 {
		t.Fatalf("LastActivity = %d, want CreatedAt 42", v.LastActivity)
	}
}

func TestUpdateVotePublishRoundFields(t *testing.T) {
	st := openTestStore(t)
	if err := st.CreateVote(VoteRow{Slug: "a", Title: "A", Phase: "suggesting", Settings: "{}", CreatorToken: "c", CreatedAt: 10}); err != nil {
		t.Fatal(err)
	}
	if err := st.TouchVote("a", 100); err != nil {
		t.Fatal(err)
	}
	v, err := st.GetVote("a")
	if err != nil {
		t.Fatal(err)
	}
	closed := int64(90)
	next, nct := "b", "nct"
	v.ClosedAt, v.NextSlug, v.NextCreatorToken = &closed, &next, &nct
	v.LastActivity = 50 // stale in-memory copy must not roll activity back
	if err := st.UpdateVote(v); err != nil {
		t.Fatal(err)
	}
	got, err := st.GetVote("a")
	if err != nil {
		t.Fatal(err)
	}
	if got.ClosedAt == nil || *got.ClosedAt != 90 || got.NextSlug == nil || *got.NextSlug != "b" ||
		got.NextCreatorToken == nil || *got.NextCreatorToken != "nct" {
		t.Fatalf("fields not persisted: %+v", got)
	}
	if got.LastActivity != 100 {
		t.Fatalf("LastActivity = %d, want 100 (not regressed)", got.LastActivity)
	}
	got.ClosedAt = nil
	if err := st.UpdateVote(got); err != nil {
		t.Fatal(err)
	}
	if again, _ := st.GetVote("a"); again.ClosedAt != nil {
		t.Fatalf("ClosedAt not cleared: %v", *again.ClosedAt)
	}
}

func TestRemovedParticipantsExcluded(t *testing.T) {
	st := openTestStore(t)
	if err := st.CreateVote(VoteRow{Slug: "a", Title: "A", Phase: "suggesting", Settings: "{}", CreatorToken: "c", CreatedAt: 1}); err != nil {
		t.Fatal(err)
	}
	for _, p := range []ParticipantRow{
		{ID: "p1", VoteSlug: "a", Name: "A", Token: "t1", JoinedAt: 1},
		{ID: "p2", VoteSlug: "a", Name: "B", Token: "t2", JoinedAt: 2},
	} {
		if err := st.AddParticipant(p); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := st.db.Exec(`UPDATE participants SET removed_at = 5, next_token = 'nt' WHERE id = 'p2'`); err != nil {
		t.Fatal(err)
	}
	ps, err := st.Participants("a")
	if err != nil || len(ps) != 1 || ps[0].ID != "p1" {
		t.Fatalf("Participants = %+v err=%v, want only p1", ps, err)
	}
	if _, err := st.ParticipantByToken("a", "t2"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("removed participant resolved by token: err=%v", err)
	}
	if _, err := st.db.Exec(`UPDATE participants SET next_token = 'nt1' WHERE id = 'p1'`); err != nil {
		t.Fatal(err)
	}
	p, err := st.ParticipantByToken("a", "t1")
	if err != nil || p.NextToken == nil || *p.NextToken != "nt1" {
		t.Fatalf("NextToken not read: %+v err=%v", p, err)
	}
}

func TestTouchAndPruneByLastActivity(t *testing.T) {
	st := openTestStore(t)
	mk := func(slug string, created int64) {
		if err := st.CreateVote(VoteRow{Slug: slug, Title: slug, Phase: "suggesting", Settings: "{}", CreatorToken: "c" + slug, CreatedAt: created}); err != nil {
			t.Fatal(err)
		}
	}
	mk("stale", 100)
	mk("busy", 100)
	if err := st.TouchVote("busy", 900); err != nil {
		t.Fatal(err)
	}
	gone, err := st.DeleteVotesInactiveSince(500)
	if err != nil {
		t.Fatal(err)
	}
	if len(gone) != 1 || gone[0] != "stale" {
		t.Fatalf("pruned %v, want [stale]", gone)
	}
	if _, err := st.GetVote("busy"); err != nil {
		t.Fatalf("recently active vote was pruned: %v", err)
	}
	if _, err := st.GetVote("stale"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("stale vote survived: %v", err)
	}
}

func TestTouchVoteUnknownSlug(t *testing.T) {
	st := openTestStore(t)
	if err := st.TouchVote("nope", 1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("TouchVote unknown = %v, want ErrNotFound", err)
	}
}

func TestCheckpointEmptiesWAL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cp.db")
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.CreateVote(VoteRow{Slug: "w", Title: "W", Phase: "suggesting", Settings: "{}", CreatorToken: "c", CreatedAt: 1}); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(path + "-wal"); err != nil || fi.Size() == 0 {
		t.Fatalf("expected a non-empty -wal before checkpoint (err=%v)", err)
	}
	if err := st.Checkpoint(); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(path + "-wal"); err == nil && fi.Size() != 0 {
		t.Fatalf("-wal is %d bytes after Checkpoint, want empty", fi.Size())
	}
}

func TestCloseCheckpointsWAL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wal.db")
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.CreateVote(VoteRow{Slug: "w", Title: "W", Phase: "suggesting", Settings: "{}", CreatorToken: "c", CreatedAt: 1}); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(path + "-wal"); err == nil && fi.Size() != 0 {
		t.Fatalf("-wal is %d bytes after Close, want empty/absent", fi.Size())
	}
	// The main file alone must hold the data.
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM votes`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("rows in main file = %d err=%v, want 1", n, err)
	}
}

func TestQueryCountCountsReads(t *testing.T) {
	st := openTestStore(t)
	before := st.QueryCount()
	_, _ = st.GetVote("nope")
	_, _ = st.Participants("nope")
	_, _ = st.ParticipantByToken("nope", "t")
	_, _ = st.Options("nope")
	_, _ = st.Ballots("nope")
	if got := st.QueryCount() - before; got != 5 {
		t.Fatalf("QueryCount delta = %d, want 5", got)
	}
}

func TestRemoveParticipantStore(t *testing.T) {
	st := openTestStore(t)

	if err := st.CreateVote(VoteRow{Slug: "v", Title: "T", Phase: "suggesting", Settings: "{}", CreatorToken: "ct", CreatedAt: 1}); err != nil {
		t.Fatalf("CreateVote: %v", err)
	}
	for _, p := range []ParticipantRow{
		{ID: "p1", VoteSlug: "v", Name: "Alice", Token: "tok1", IsCreator: true, JoinedAt: 1},
		{ID: "p2", VoteSlug: "v", Name: "Bob", Token: "tok2", JoinedAt: 2},
	} {
		if err := st.AddParticipant(p); err != nil {
			t.Fatalf("AddParticipant: %v", err)
		}
	}
	if err := st.AddOption(OptionRow{ID: "o1", VoteSlug: "v", ParticipantID: "p2", Title: "Bob's", CreatedAt: 3}); err != nil {
		t.Fatalf("AddOption: %v", err)
	}
	if err := st.AddOption(OptionRow{ID: "o2", VoteSlug: "v", ParticipantID: "p1", Title: "Alice's", CreatedAt: 4}); err != nil {
		t.Fatalf("AddOption: %v", err)
	}
	if err := st.PutBallot("v", "p2", `{"o2":1}`); err != nil {
		t.Fatalf("PutBallot: %v", err)
	}
	if err := st.SetDoneSuggesting("p2", true); err != nil {
		t.Fatalf("SetDoneSuggesting: %v", err)
	}

	if err := st.RemoveParticipant("v", "p1", "fresh1", false, 10); !errors.Is(err, ErrIsCreator) {
		t.Fatalf("removing creator: got %v, want ErrIsCreator", err)
	}
	if err := st.RemoveParticipant("v", "nope", "fresh2", false, 10); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown participant: got %v, want ErrNotFound", err)
	}
	if err := st.RemoveParticipant("other", "p2", "fresh3", false, 10); !errors.Is(err, ErrNotFound) {
		t.Fatalf("participant of another vote: got %v, want ErrNotFound", err)
	}

	if err := st.RemoveParticipant("v", "p2", "fresh4", true, 10); err != nil {
		t.Fatalf("RemoveParticipant: %v", err)
	}
	if _, err := st.ParticipantByToken("v", "tok2"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("old token still resolves: %v", err)
	}
	if _, err := st.ParticipantByToken("v", "fresh4"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("removed participant's new token must not resolve either: %v", err)
	}
	parts, err := st.Participants("v")
	if err != nil || len(parts) != 1 || parts[0].ID != "p1" {
		t.Fatalf("Participants after remove = %+v, %v", parts, err)
	}
	ballots, err := st.Ballots("v")
	if err != nil {
		t.Fatalf("Ballots: %v", err)
	}
	if _, ok := ballots["p2"]; ok {
		t.Fatal("removed participant's ballot must be deleted")
	}
	opts, err := st.Options("v")
	if err != nil || len(opts) != 1 || opts[0].ID != "o2" {
		t.Fatalf("options after remove with deleteOptions = %+v, %v", opts, err)
	}

	if err := st.RemoveParticipant("v", "p2", "fresh5", true, 11); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second removal: got %v, want ErrNotFound", err)
	}
}

func TestRemoveParticipantKeepsOptionsWhenAsked(t *testing.T) {
	st := openTestStore(t)
	if err := st.CreateVote(VoteRow{Slug: "v", Title: "T", Phase: "voting", Settings: "{}", CreatorToken: "ct", CreatedAt: 1}); err != nil {
		t.Fatalf("CreateVote: %v", err)
	}
	if err := st.AddParticipant(ParticipantRow{ID: "p2", VoteSlug: "v", Name: "Bob", Token: "tok2", JoinedAt: 2}); err != nil {
		t.Fatalf("AddParticipant: %v", err)
	}
	if err := st.AddOption(OptionRow{ID: "o1", VoteSlug: "v", ParticipantID: "p2", Title: "Bob's", CreatedAt: 3}); err != nil {
		t.Fatalf("AddOption: %v", err)
	}
	if err := st.RemoveParticipant("v", "p2", "fresh", false, 10); err != nil {
		t.Fatalf("RemoveParticipant: %v", err)
	}
	opts, err := st.Options("v")
	if err != nil || len(opts) != 1 {
		t.Fatalf("options must be kept, got %+v, %v", opts, err)
	}
}
