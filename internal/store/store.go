// Package store implements SQLite-backed persistence for quick-vote.
package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	_ "modernc.org/sqlite"
)

// ErrNotFound is returned when a lookup does not match any row.
var ErrNotFound = errors.New("not found")

// ErrIsCreator is returned by RemoveParticipant when asked to remove the
// vote's creator: the creator is the only one who can moderate the room, so
// removing them would leave it unmanageable.
var ErrIsCreator = errors.New("cannot remove the creator")

// ErrConflict signals a state conflict (e.g. a successor vote already exists).
var ErrConflict = errors.New("conflict")

const schema = `
CREATE TABLE IF NOT EXISTS votes (
  slug TEXT PRIMARY KEY, title TEXT NOT NULL,
  phase TEXT NOT NULL DEFAULT 'suggesting',
  settings TEXT NOT NULL, creator_token TEXT NOT NULL,
  phase_deadline INTEGER, results TEXT, created_at INTEGER NOT NULL,
  active_options TEXT, last_activity INTEGER, closed_at INTEGER,
  next_slug TEXT, next_creator_token TEXT);
CREATE TABLE IF NOT EXISTS participants (
  id TEXT PRIMARY KEY, vote_slug TEXT NOT NULL REFERENCES votes(slug),
  name TEXT NOT NULL, token TEXT NOT NULL UNIQUE,
  is_creator INTEGER NOT NULL DEFAULT 0,
  wants_revote INTEGER NOT NULL DEFAULT 0, joined_at INTEGER NOT NULL,
  done_suggesting INTEGER NOT NULL DEFAULT 0,
  removed_at INTEGER, next_token TEXT);
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

// migrationColumns lists columns added after the initial release that must
// be additively migrated onto pre-existing databases (CREATE TABLE IF NOT
// EXISTS does not alter tables that already exist). Each entry is also
// present in the base schema string above so fresh databases get it from
// CREATE TABLE directly; this list is what makes it idempotent for old ones.
var migrationColumns = []struct {
	table, column, ddl string
}{
	{"participants", "done_suggesting", "ALTER TABLE participants ADD COLUMN done_suggesting INTEGER NOT NULL DEFAULT 0"},
	{"votes", "active_options", "ALTER TABLE votes ADD COLUMN active_options TEXT"},
	{"votes", "last_activity", "ALTER TABLE votes ADD COLUMN last_activity INTEGER"},
	{"votes", "closed_at", "ALTER TABLE votes ADD COLUMN closed_at INTEGER"},
	{"votes", "next_slug", "ALTER TABLE votes ADD COLUMN next_slug TEXT"},
	{"votes", "next_creator_token", "ALTER TABLE votes ADD COLUMN next_creator_token TEXT"},
	{"participants", "removed_at", "ALTER TABLE participants ADD COLUMN removed_at INTEGER"},
	{"participants", "next_token", "ALTER TABLE participants ADD COLUMN next_token TEXT"},
}

// migrate adds any columns from migrationColumns missing on tables that
// already existed before this version, using PRAGMA table_info to detect
// them (ALTER TABLE ADD COLUMN has no "IF NOT EXISTS" form in SQLite).
func migrate(db *sql.DB) error {
	for _, m := range migrationColumns {
		rows, err := db.Query(fmt.Sprintf(`PRAGMA table_info(%s)`, m.table))
		if err != nil {
			return fmt.Errorf("inspect %s columns: %w", m.table, err)
		}
		found := false
		for rows.Next() {
			var cid int
			var name, ctype string
			var notNull, pk int
			var dflt any
			if err := rows.Scan(&cid, &name, &ctype, &notNull, &dflt, &pk); err != nil {
				rows.Close()
				return fmt.Errorf("scan %s column info: %w", m.table, err)
			}
			if name == m.column {
				found = true
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return fmt.Errorf("list %s columns: %w", m.table, err)
		}
		rows.Close()
		if found {
			continue
		}
		if _, err := db.Exec(m.ddl); err != nil {
			return fmt.Errorf("add column %s.%s: %w", m.table, m.column, err)
		}
	}
	// Votes that predate last-activity tracking count as last active when
	// they were created, so retention treats them exactly as it did before
	// (created_at-based) until something touches them.
	if _, err := db.Exec(`UPDATE votes SET last_activity = created_at WHERE last_activity IS NULL`); err != nil {
		return fmt.Errorf("backfill last_activity: %w", err)
	}
	return nil
}

// VoteRow mirrors the votes table.
type VoteRow struct {
	Slug          string
	Title         string
	Phase         string
	Settings      string
	CreatorToken  string
	PhaseDeadline *int64
	Results       *string
	CreatedAt     int64
	// ActiveOptions is a JSON array of option IDs restricting voting/scoring
	// to those options (used during a runoff round). nil means all options
	// are active.
	ActiveOptions *string
	// LastActivity (unix seconds) is when the vote last changed; retention
	// prunes by it so a long-running but active vote is never deleted.
	LastActivity int64
	// ClosedAt is set while the creator has closed the vote; nil means open.
	ClosedAt *int64
	// NextSlug is the successor vote started "with this group", if any.
	NextSlug *string
	// NextCreatorToken is the successor's creator token, kept so it can be
	// handed only to this vote's creator.
	NextCreatorToken *string
	// NextTitle is the successor's title, read-only: GetVote joins it in so
	// the room state can label the handoff link without another query. nil
	// when there is no successor (or it has since been pruned).
	NextTitle *string
}

// HasNext reports whether v links to a successor that still exists. A
// next_slug whose row is gone (NextTitle nil) counts as no successor: pruning
// unlinks it, but a database pruned by an older build may still dangle.
func (v VoteRow) HasNext() bool {
	return v.NextSlug != nil && v.NextTitle != nil
}

// ParticipantRow mirrors the participants table.
type ParticipantRow struct {
	ID          string
	VoteSlug    string
	Name        string
	Token       string
	IsCreator   bool
	WantsRevote bool
	JoinedAt    int64
	// DoneSuggesting is the participant's self-reported "I'm done
	// suggesting" flag, toggled explicitly and independent of how many
	// suggestions (if any) they've made.
	DoneSuggesting bool
	// NextToken is this participant's session token in the successor vote,
	// sent only to them so they can follow the group without re-joining.
	NextToken *string
}

// OptionRow mirrors the options table.
type OptionRow struct {
	ID            string
	VoteSlug      string
	ParticipantID string
	Title         string
	CreatedAt     int64
}

// Store is a SQLite-backed persistence layer. Writes are serialized with a
// mutex since the underlying DB connection pool is limited to one.
type Store struct {
	db *sql.DB
	mu sync.Mutex
	// queries counts room-data reads (see QueryCount).
	queries atomic.Int64
}

// QueryCount returns how many room-data reads (GetVote, Participants,
// ParticipantByToken, Options, Ballots) this store has issued. It exists so
// tests can assert that broadcast fan-out loads room data once rather than
// once per connection; it has no production use.
func (s *Store) QueryCount() int64 {
	return s.queries.Load()
}

// Open opens (creating if necessary) the SQLite database at path and
// applies the schema.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	db.SetMaxOpenConns(1)

	if _, err := db.Exec(`PRAGMA journal_mode=WAL;`); err != nil {
		db.Close()
		return nil, fmt.Errorf("set journal_mode: %w", err)
	}
	if _, err := db.Exec(`PRAGMA foreign_keys=ON;`); err != nil {
		db.Close()
		return nil, fmt.Errorf("set foreign_keys: %w", err)
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	if err := migrate(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate schema: %w", err)
	}

	return &Store{db: db}, nil
}

// Close checkpoints the WAL and closes the underlying database connection.
// The checkpoint's error is ignored: closing must still happen, and SQLite
// also checkpoints on last-connection close when it can.
func (s *Store) Close() error {
	_ = s.Checkpoint()
	return s.db.Close()
}

// Checkpoint copies the WAL into the main database file and truncates the
// WAL. SQLite only auto-checkpoints once the WAL reaches 1000 pages, so a
// small, quiet database can otherwise keep all recent data in -wal
// indefinitely, where a copy of the main file alone (a naive backup) misses it.
func (s *Store) Checkpoint() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, err := s.db.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		return fmt.Errorf("wal checkpoint: %w", err)
	}
	return nil
}

// TouchVote records activity on a vote at unix time at, which postpones its
// retention pruning. Returns ErrNotFound if the vote does not exist.
func (s *Store) TouchVote(slug string, at int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	res, err := s.db.Exec(`UPDATE votes SET last_activity = ? WHERE slug = ?`, at, slug)
	if err != nil {
		return fmt.Errorf("touch vote: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("touch vote rows affected: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// SetClosed sets (closedAt non-nil) or clears (nil) a vote's closed_at
// marker without touching any other column, so reopening can't clobber
// fields another code path just wrote. Returns ErrNotFound if the vote does
// not exist.
func (s *Store) SetClosed(slug string, closedAt *int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	res, err := s.db.Exec(`UPDATE votes SET closed_at = ? WHERE slug = ?`, closedAt, slug)
	if err != nil {
		return fmt.Errorf("set closed: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("set closed rows affected: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// CreateVote inserts a new vote row. A zero LastActivity defaults to
// CreatedAt: a brand-new vote was last active when it was created.
func (s *Store) CreateVote(v VoteRow) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if v.LastActivity == 0 {
		v.LastActivity = v.CreatedAt
	}
	_, err := s.db.Exec(
		`INSERT INTO votes (slug, title, phase, settings, creator_token, phase_deadline, results, created_at, active_options,
		   last_activity, closed_at, next_slug, next_creator_token)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		v.Slug, v.Title, v.Phase, v.Settings, v.CreatorToken, v.PhaseDeadline, v.Results, v.CreatedAt, v.ActiveOptions,
		v.LastActivity, v.ClosedAt, v.NextSlug, v.NextCreatorToken,
	)
	if err != nil {
		return fmt.Errorf("create vote: %w", err)
	}
	return nil
}

// GetVote fetches a vote by slug. Returns ErrNotFound if it does not exist.
func (s *Store) GetVote(slug string) (VoteRow, error) {
	s.queries.Add(1)
	s.mu.Lock()
	defer s.mu.Unlock()

	row := s.db.QueryRow(
		`SELECT v.slug, v.title, v.phase, v.settings, v.creator_token, v.phase_deadline, v.results, v.created_at,
		   v.active_options, COALESCE(v.last_activity, v.created_at), v.closed_at, v.next_slug, v.next_creator_token,
		   n.title
		 FROM votes v LEFT JOIN votes n ON n.slug = v.next_slug
		 WHERE v.slug = ?`, slug,
	)
	var v VoteRow
	if err := row.Scan(&v.Slug, &v.Title, &v.Phase, &v.Settings, &v.CreatorToken,
		&v.PhaseDeadline, &v.Results, &v.CreatedAt, &v.ActiveOptions,
		&v.LastActivity, &v.ClosedAt, &v.NextSlug, &v.NextCreatorToken, &v.NextTitle); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return VoteRow{}, ErrNotFound
		}
		return VoteRow{}, fmt.Errorf("get vote: %w", err)
	}
	return v, nil
}

// UpdateVote updates the mutable fields of a vote: phase, deadline, results,
// active_options, closed_at, next_slug, next_creator_token and
// last_activity. last_activity only ever moves forward: a handler holding a
// VoteRow loaded before a TouchVote must not roll the activity time back and
// make a live vote look prunable.
func (s *Store) UpdateVote(v VoteRow) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	res, err := s.db.Exec(
		`UPDATE votes SET phase = ?, phase_deadline = ?, results = ?, active_options = ?,
		   closed_at = ?, next_slug = ?, next_creator_token = ?,
		   last_activity = MAX(COALESCE(last_activity, created_at), ?)
		 WHERE slug = ?`,
		v.Phase, v.PhaseDeadline, v.Results, v.ActiveOptions,
		v.ClosedAt, v.NextSlug, v.NextCreatorToken, v.LastActivity, v.Slug,
	)
	if err != nil {
		return fmt.Errorf("update vote: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("update vote rows affected: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// CreateNextVote atomically creates next (with its participants) and links
// it from srcSlug: sets votes.next_slug and next_creator_token on the source
// and participants.next_token for each carried-over source participant.
// carried maps source participant ID -> new participant row. Returns
// ErrConflict if the source already has a successor that still exists,
// ErrNotFound if the source vote does not exist.
//
// One transaction, so a crash can't leave a successor the old room never
// links to, or a link to participants that were never created.
func (s *Store) CreateNextVote(srcSlug string, next VoteRow, carried map[string]ParticipantRow) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin next vote: %w", err)
	}
	defer tx.Rollback()

	// Only a successor that still exists counts (see VoteRow.HasNext); a
	// dangling link is replaced below.
	var existing *string
	if err := tx.QueryRow(
		`SELECT n.slug FROM votes v LEFT JOIN votes n ON n.slug = v.next_slug WHERE v.slug = ?`, srcSlug,
	).Scan(&existing); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return fmt.Errorf("lookup source vote: %w", err)
	}
	if existing != nil {
		return ErrConflict
	}
	// Drop any handoff tokens left by a dangling link, so only the
	// participants carried this time get one.
	if _, err := tx.Exec(`UPDATE participants SET next_token = NULL WHERE vote_slug = ?`, srcSlug); err != nil {
		return fmt.Errorf("clear stale next tokens: %w", err)
	}

	if next.LastActivity == 0 {
		next.LastActivity = next.CreatedAt
	}
	if _, err := tx.Exec(
		`INSERT INTO votes (slug, title, phase, settings, creator_token, phase_deadline, results, created_at, active_options,
		   last_activity, closed_at, next_slug, next_creator_token)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		next.Slug, next.Title, next.Phase, next.Settings, next.CreatorToken, next.PhaseDeadline, next.Results,
		next.CreatedAt, next.ActiveOptions, next.LastActivity, next.ClosedAt, next.NextSlug, next.NextCreatorToken,
	); err != nil {
		return fmt.Errorf("create next vote: %w", err)
	}

	// Insert in source join order so the new room lists the group the same
	// way (Participants orders by joined_at, then rowid).
	rows, err := tx.Query(
		`SELECT id FROM participants WHERE vote_slug = ? ORDER BY joined_at ASC, rowid ASC`, srcSlug,
	)
	if err != nil {
		return fmt.Errorf("list source participants: %w", err)
	}
	var order []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return fmt.Errorf("scan source participant: %w", err)
		}
		if _, ok := carried[id]; ok {
			order = append(order, id)
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("list source participants: %w", err)
	}
	rows.Close()
	if len(order) != len(carried) {
		return fmt.Errorf("create next vote: carried participant not in source vote %s", srcSlug)
	}

	for _, srcID := range order {
		p := carried[srcID]
		if _, err := tx.Exec(
			`INSERT INTO participants (id, vote_slug, name, token, is_creator, wants_revote, joined_at, done_suggesting)
			 VALUES (?, ?, ?, ?, ?, 0, ?, 0)`,
			p.ID, next.Slug, p.Name, p.Token, boolToInt(p.IsCreator), p.JoinedAt,
		); err != nil {
			return fmt.Errorf("carry participant: %w", err)
		}
		if _, err := tx.Exec(
			`UPDATE participants SET next_token = ? WHERE id = ? AND vote_slug = ?`, p.Token, srcID, srcSlug,
		); err != nil {
			return fmt.Errorf("set next token: %w", err)
		}
	}

	if _, err := tx.Exec(
		`UPDATE votes SET next_slug = ?, next_creator_token = ? WHERE slug = ?`,
		next.Slug, next.CreatorToken, srcSlug,
	); err != nil {
		return fmt.Errorf("link next vote: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit next vote: %w", err)
	}
	return nil
}

// ActiveDeadlines returns the phase deadline (unix seconds) for every vote
// that currently has one set, keyed by slug. Used on server startup to re-arm
// in-flight phase timers that would otherwise be lost across a restart.
func (s *Store) ActiveDeadlines() (map[string]int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := s.db.Query(
		`SELECT slug, phase_deadline FROM votes WHERE phase_deadline IS NOT NULL`,
	)
	if err != nil {
		return nil, fmt.Errorf("list active deadlines: %w", err)
	}
	defer rows.Close()

	out := make(map[string]int64)
	for rows.Next() {
		var slug string
		var deadline int64
		if err := rows.Scan(&slug, &deadline); err != nil {
			return nil, fmt.Errorf("scan active deadline: %w", err)
		}
		out[slug] = deadline
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list active deadlines: %w", err)
	}
	return out, nil
}

// AddParticipant inserts a new participant row.
func (s *Store) AddParticipant(p ParticipantRow) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(
		`INSERT INTO participants (id, vote_slug, name, token, is_creator, wants_revote, joined_at, done_suggesting)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		p.ID, p.VoteSlug, p.Name, p.Token, boolToInt(p.IsCreator), boolToInt(p.WantsRevote), p.JoinedAt, boolToInt(p.DoneSuggesting),
	)
	if err != nil {
		return fmt.Errorf("add participant: %w", err)
	}
	return nil
}

// RemoveParticipant soft-deletes a participant of slug in one transaction:
// their ballot is deleted, their suggestions too when deleteOptions is set
// (the caller decides by phase — once voting has started, others may have
// spent credits on those options), and the row is marked removed with its
// session token replaced by newToken. newToken is never handed to anyone, so
// the old token stops authenticating immediately. The row itself stays so
// options kept after removal still have a valid participant_id.
//
// Returns ErrNotFound if the participant is not a current (non-removed)
// member of slug, and ErrIsCreator for the creator.
func (s *Store) RemoveParticipant(slug, participantID, newToken string, deleteOptions bool, at int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin remove participant: %w", err)
	}
	defer tx.Rollback()

	var isCreator int
	err = tx.QueryRow(
		`SELECT is_creator FROM participants WHERE id = ? AND vote_slug = ? AND removed_at IS NULL`,
		participantID, slug,
	).Scan(&isCreator)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return fmt.Errorf("lookup participant: %w", err)
	}
	if isCreator != 0 {
		return ErrIsCreator
	}

	if _, err := tx.Exec(`DELETE FROM ballots WHERE vote_slug = ? AND participant_id = ?`, slug, participantID); err != nil {
		return fmt.Errorf("delete removed participant's ballot: %w", err)
	}
	if deleteOptions {
		if _, err := tx.Exec(`DELETE FROM options WHERE vote_slug = ? AND participant_id = ?`, slug, participantID); err != nil {
			return fmt.Errorf("delete removed participant's options: %w", err)
		}
	}
	if _, err := tx.Exec(
		`UPDATE participants SET removed_at = ?, token = ?, wants_revote = 0, done_suggesting = 0, next_token = NULL
		 WHERE id = ?`,
		at, newToken, participantID,
	); err != nil {
		return fmt.Errorf("mark participant removed: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit remove participant: %w", err)
	}
	return nil
}

// Participants returns the current (non-removed) participants of a vote in
// joined order. Removed participants are soft-deleted so their options and
// ballots keep valid foreign keys, but they no longer count for anything.
func (s *Store) Participants(slug string) ([]ParticipantRow, error) {
	s.queries.Add(1)
	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := s.db.Query(
		`SELECT id, vote_slug, name, token, is_creator, wants_revote, joined_at, done_suggesting, next_token
		 FROM participants WHERE vote_slug = ? AND removed_at IS NULL ORDER BY joined_at ASC, rowid ASC`, slug,
	)
	if err != nil {
		return nil, fmt.Errorf("list participants: %w", err)
	}
	defer rows.Close()

	var out []ParticipantRow
	for rows.Next() {
		var p ParticipantRow
		var isCreator, wantsRevote, doneSuggesting int
		if err := rows.Scan(&p.ID, &p.VoteSlug, &p.Name, &p.Token, &isCreator, &wantsRevote, &p.JoinedAt, &doneSuggesting, &p.NextToken); err != nil {
			return nil, fmt.Errorf("scan participant: %w", err)
		}
		p.IsCreator = isCreator != 0
		p.WantsRevote = wantsRevote != 0
		p.DoneSuggesting = doneSuggesting != 0
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list participants: %w", err)
	}
	return out, nil
}

// ParticipantByToken looks up a current (non-removed) participant of a vote
// by their session token; a removed participant's token no longer resolves.
func (s *Store) ParticipantByToken(slug, token string) (ParticipantRow, error) {
	s.queries.Add(1)
	s.mu.Lock()
	defer s.mu.Unlock()

	row := s.db.QueryRow(
		`SELECT id, vote_slug, name, token, is_creator, wants_revote, joined_at, done_suggesting, next_token
		 FROM participants WHERE vote_slug = ? AND token = ? AND removed_at IS NULL`, slug, token,
	)
	var p ParticipantRow
	var isCreator, wantsRevote, doneSuggesting int
	if err := row.Scan(&p.ID, &p.VoteSlug, &p.Name, &p.Token, &isCreator, &wantsRevote, &p.JoinedAt, &doneSuggesting, &p.NextToken); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ParticipantRow{}, ErrNotFound
		}
		return ParticipantRow{}, fmt.Errorf("get participant by token: %w", err)
	}
	p.IsCreator = isCreator != 0
	p.WantsRevote = wantsRevote != 0
	p.DoneSuggesting = doneSuggesting != 0
	return p, nil
}

// SetWantsRevote sets a participant's wants_revote flag.
func (s *Store) SetWantsRevote(participantID string, want bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	res, err := s.db.Exec(
		`UPDATE participants SET wants_revote = ? WHERE id = ?`, boolToInt(want), participantID,
	)
	if err != nil {
		return fmt.Errorf("set wants_revote: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("set wants_revote rows affected: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// SetDoneSuggesting sets a participant's "I'm done suggesting" flag.
func (s *Store) SetDoneSuggesting(participantID string, done bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	res, err := s.db.Exec(
		`UPDATE participants SET done_suggesting = ? WHERE id = ?`, boolToInt(done), participantID,
	)
	if err != nil {
		return fmt.Errorf("set done_suggesting: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("set done_suggesting rows affected: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// ResetRevotes clears wants_revote for every participant of a vote.
func (s *Store) ResetRevotes(slug string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(`UPDATE participants SET wants_revote = 0 WHERE vote_slug = ?`, slug)
	if err != nil {
		return fmt.Errorf("reset revotes: %w", err)
	}
	return nil
}

// AddOption inserts a new option row.
func (s *Store) AddOption(o OptionRow) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(
		`INSERT INTO options (id, vote_slug, participant_id, title, created_at)
		 VALUES (?, ?, ?, ?, ?)`,
		o.ID, o.VoteSlug, o.ParticipantID, o.Title, o.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("add option: %w", err)
	}
	return nil
}

// DeleteOption deletes an option only if it belongs to participantID.
// If the option does not exist or is owned by someone else, it is a no-op
// and an error is returned.
func (s *Store) DeleteOption(slug, optionID, participantID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	res, err := s.db.Exec(
		`DELETE FROM options WHERE id = ? AND vote_slug = ? AND participant_id = ?`,
		optionID, slug, participantID,
	)
	if err != nil {
		return fmt.Errorf("delete option: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete option rows affected: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteOptionAny deletes an option regardless of who suggested it. Callers
// must already have authenticated the vote's creator: this is the moderation
// path (spec B2), so unlike DeleteOption it has no owner filter. Returns
// ErrNotFound if the option doesn't exist in this vote.
func (s *Store) DeleteOptionAny(slug, optionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	res, err := s.db.Exec(
		`DELETE FROM options WHERE id = ? AND vote_slug = ?`,
		optionID, slug,
	)
	if err != nil {
		return fmt.Errorf("delete option: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete option rows affected: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// Options returns all options of a vote in creation order.
func (s *Store) Options(slug string) ([]OptionRow, error) {
	s.queries.Add(1)
	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := s.db.Query(
		`SELECT id, vote_slug, participant_id, title, created_at
		 FROM options WHERE vote_slug = ? ORDER BY created_at ASC, rowid ASC`, slug,
	)
	if err != nil {
		return nil, fmt.Errorf("list options: %w", err)
	}
	defer rows.Close()

	var out []OptionRow
	for rows.Next() {
		var o OptionRow
		if err := rows.Scan(&o.ID, &o.VoteSlug, &o.ParticipantID, &o.Title, &o.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan option: %w", err)
		}
		out = append(out, o)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list options: %w", err)
	}
	return out, nil
}

// PutBallot upserts a participant's ballot (votes as a JSON-encoded
// map[optionID]int).
func (s *Store) PutBallot(slug, participantID, votesJSON string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(
		`INSERT INTO ballots (vote_slug, participant_id, votes, updated_at)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT (vote_slug, participant_id)
		 DO UPDATE SET votes = excluded.votes, updated_at = excluded.updated_at`,
		slug, participantID, votesJSON, time.Now().Unix(),
	)
	if err != nil {
		return fmt.Errorf("put ballot: %w", err)
	}
	return nil
}

// Ballots returns all ballots of a vote, decoded from JSON, keyed by
// participant ID then option ID.
func (s *Store) Ballots(slug string) (map[string]map[string]int, error) {
	s.queries.Add(1)
	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := s.db.Query(
		`SELECT participant_id, votes FROM ballots WHERE vote_slug = ?`, slug,
	)
	if err != nil {
		return nil, fmt.Errorf("list ballots: %w", err)
	}
	defer rows.Close()

	out := make(map[string]map[string]int)
	for rows.Next() {
		var pid, votesJSON string
		if err := rows.Scan(&pid, &votesJSON); err != nil {
			return nil, fmt.Errorf("scan ballot: %w", err)
		}
		var votes map[string]int
		if err := json.Unmarshal([]byte(votesJSON), &votes); err != nil {
			return nil, fmt.Errorf("decode ballot votes: %w", err)
		}
		out[pid] = votes
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list ballots: %w", err)
	}
	return out, nil
}

// DeleteBallots removes all ballots for a vote (used when a re-vote fires).
func (s *Store) DeleteBallots(slug string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(`DELETE FROM ballots WHERE vote_slug = ?`, slug)
	if err != nil {
		return fmt.Errorf("delete ballots: %w", err)
	}
	return nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// DeleteVotesInactiveSince permanently removes every vote whose last
// activity is before cutoff (unix seconds), along with its participants,
// options and ballots, and returns the deleted slugs. Used for
// data-retention pruning: keyed on activity rather than creation so a vote
// still in use is never deleted out from under its group. Surviving votes
// that linked to a deleted successor are unlinked in the same transaction.
func (s *Store) DeleteVotesInactiveSince(cutoff int64) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("begin prune: %w", err)
	}
	defer tx.Rollback()

	rows, err := tx.Query(`SELECT slug FROM votes WHERE last_activity < ?`, cutoff)
	if err != nil {
		return nil, fmt.Errorf("list expired votes: %w", err)
	}
	var slugs []string
	for rows.Next() {
		var slug string
		if err := rows.Scan(&slug); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan expired vote: %w", err)
		}
		slugs = append(slugs, slug)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("list expired votes: %w", err)
	}
	rows.Close()
	if len(slugs) == 0 {
		return nil, nil
	}

	sub := `SELECT slug FROM votes WHERE last_activity < ?`
	for _, q := range []string{
		// A surviving vote whose successor is being pruned drops the link
		// and its participants' handoff tokens, so it never points the
		// group at a vote that is gone and can start another follow-up.
		`UPDATE participants SET next_token = NULL
		 WHERE vote_slug IN (SELECT slug FROM votes WHERE next_slug IN (` + sub + `))`,
		`UPDATE votes SET next_slug = NULL, next_creator_token = NULL WHERE next_slug IN (` + sub + `)`,
		// Children first: foreign keys are enforced and declared without
		// ON DELETE CASCADE.
		`DELETE FROM ballots WHERE vote_slug IN (` + sub + `)`,
		`DELETE FROM options WHERE vote_slug IN (` + sub + `)`,
		`DELETE FROM participants WHERE vote_slug IN (` + sub + `)`,
		`DELETE FROM votes WHERE last_activity < ?`,
	} {
		if _, err := tx.Exec(q, cutoff); err != nil {
			return nil, fmt.Errorf("prune votes: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit prune: %w", err)
	}
	return slugs, nil
}
