// Package store implements SQLite-backed persistence for quick-vote.
package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// ErrNotFound is returned when a lookup does not match any row.
var ErrNotFound = errors.New("not found")

const schema = `
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

	return &Store{db: db}, nil
}

// Close closes the underlying database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

// CreateVote inserts a new vote row.
func (s *Store) CreateVote(v VoteRow) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(
		`INSERT INTO votes (slug, title, phase, settings, creator_token, phase_deadline, results, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		v.Slug, v.Title, v.Phase, v.Settings, v.CreatorToken, v.PhaseDeadline, v.Results, v.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("create vote: %w", err)
	}
	return nil
}

// GetVote fetches a vote by slug. Returns ErrNotFound if it does not exist.
func (s *Store) GetVote(slug string) (VoteRow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	row := s.db.QueryRow(
		`SELECT slug, title, phase, settings, creator_token, phase_deadline, results, created_at
		 FROM votes WHERE slug = ?`, slug,
	)
	var v VoteRow
	if err := row.Scan(&v.Slug, &v.Title, &v.Phase, &v.Settings, &v.CreatorToken,
		&v.PhaseDeadline, &v.Results, &v.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return VoteRow{}, ErrNotFound
		}
		return VoteRow{}, fmt.Errorf("get vote: %w", err)
	}
	return v, nil
}

// UpdateVote updates the mutable fields of a vote: phase, deadline, results.
func (s *Store) UpdateVote(v VoteRow) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	res, err := s.db.Exec(
		`UPDATE votes SET phase = ?, phase_deadline = ?, results = ? WHERE slug = ?`,
		v.Phase, v.PhaseDeadline, v.Results, v.Slug,
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
		`INSERT INTO participants (id, vote_slug, name, token, is_creator, wants_revote, joined_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		p.ID, p.VoteSlug, p.Name, p.Token, boolToInt(p.IsCreator), boolToInt(p.WantsRevote), p.JoinedAt,
	)
	if err != nil {
		return fmt.Errorf("add participant: %w", err)
	}
	return nil
}

// Participants returns all participants of a vote in joined order.
func (s *Store) Participants(slug string) ([]ParticipantRow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := s.db.Query(
		`SELECT id, vote_slug, name, token, is_creator, wants_revote, joined_at
		 FROM participants WHERE vote_slug = ? ORDER BY joined_at ASC, rowid ASC`, slug,
	)
	if err != nil {
		return nil, fmt.Errorf("list participants: %w", err)
	}
	defer rows.Close()

	var out []ParticipantRow
	for rows.Next() {
		var p ParticipantRow
		var isCreator, wantsRevote int
		if err := rows.Scan(&p.ID, &p.VoteSlug, &p.Name, &p.Token, &isCreator, &wantsRevote, &p.JoinedAt); err != nil {
			return nil, fmt.Errorf("scan participant: %w", err)
		}
		p.IsCreator = isCreator != 0
		p.WantsRevote = wantsRevote != 0
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list participants: %w", err)
	}
	return out, nil
}

// ParticipantByToken looks up a participant of a vote by their session token.
func (s *Store) ParticipantByToken(slug, token string) (ParticipantRow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	row := s.db.QueryRow(
		`SELECT id, vote_slug, name, token, is_creator, wants_revote, joined_at
		 FROM participants WHERE vote_slug = ? AND token = ?`, slug, token,
	)
	var p ParticipantRow
	var isCreator, wantsRevote int
	if err := row.Scan(&p.ID, &p.VoteSlug, &p.Name, &p.Token, &isCreator, &wantsRevote, &p.JoinedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ParticipantRow{}, ErrNotFound
		}
		return ParticipantRow{}, fmt.Errorf("get participant by token: %w", err)
	}
	p.IsCreator = isCreator != 0
	p.WantsRevote = wantsRevote != 0
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

// Options returns all options of a vote in creation order.
func (s *Store) Options(slug string) ([]OptionRow, error) {
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
