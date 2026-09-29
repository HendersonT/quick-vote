package server

import (
	"crypto/subtle"

	"github.com/HendersonT/quick-vote/internal/store"
)

// roomData is everything BuildRoomState needs for one vote, loaded once.
// Loading it once per change and personalizing in memory keeps a broadcast's
// store cost constant no matter how many connections are watching the room.
type roomData struct {
	vote    store.VoteRow
	parts   []store.ParticipantRow
	opts    []store.OptionRow
	ballots map[string]map[string]int
}

// loadRoom reads the vote, its (non-removed) participants, options, and
// ballots. A missing vote surfaces as store.ErrNotFound from GetVote so
// callers can map it to 404.
func (s *Server) loadRoom(slug string) (roomData, error) {
	v, err := s.store.GetVote(slug)
	if err != nil {
		return roomData{}, err
	}
	parts, err := s.store.Participants(slug)
	if err != nil {
		return roomData{}, err
	}
	opts, err := s.store.Options(slug)
	if err != nil {
		return roomData{}, err
	}
	ballots, err := s.store.Ballots(slug)
	if err != nil {
		return roomData{}, err
	}
	return roomData{vote: v, parts: parts, opts: opts, ballots: ballots}, nil
}

// requester resolves token against the already-loaded participants with a
// constant-time compare; nil for "" or no match (spectator). Every
// participant is compared (no early exit) so timing doesn't reveal which
// position, if any, matched. The result is a copy, so callers can't mutate
// the participant list shared across connections.
func (d roomData) requester(token string) *store.ParticipantRow {
	if token == "" {
		return nil
	}
	var found *store.ParticipantRow
	for i := range d.parts {
		if subtle.ConstantTimeCompare([]byte(d.parts[i].Token), []byte(token)) == 1 && found == nil {
			p := d.parts[i]
			found = &p
		}
	}
	return found
}

// stateFor builds the room state personalized for the session token (a
// spectator view when the token is empty or unknown). creatorTokenOK: the
// caller also presented the vote's creator token (see BuildRoomState).
func (d roomData) stateFor(token string, creatorTokenOK bool) map[string]any {
	return d.stateForParticipant(d.requester(token), creatorTokenOK)
}

// stateForParticipant builds the room state personalized for p (nil =
// spectator).
func (d roomData) stateForParticipant(p *store.ParticipantRow, creatorTokenOK bool) map[string]any {
	return BuildRoomState(d.vote, d.parts, d.opts, d.ballots, p, creatorTokenOK)
}
