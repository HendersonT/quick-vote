package server

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/HendersonT/quick-vote/internal/domain"
	"github.com/HendersonT/quick-vote/internal/ids"
	"github.com/HendersonT/quick-vote/internal/store"
)

// requireCreator authenticates the caller as this vote's creator: a valid
// creator session AND the matching X-Creator-Token. Writes 401/403 and
// returns ok=false otherwise.
//
// Both are required so that a leaked creator token alone (e.g. copied from a
// shared device's storage) is not enough to moderate the room, and a
// non-creator session can't borrow the creator token either.
func (s *Server) requireCreator(w http.ResponseWriter, r *http.Request, v store.VoteRow) (store.ParticipantRow, bool) {
	p, ok := s.requireParticipant(w, r, v.Slug)
	if !ok {
		return store.ParticipantRow{}, false
	}
	if !p.IsCreator || !creatorTokenValid(r.Header.Get("X-Creator-Token"), v) {
		writeError(w, http.StatusForbidden, "creator token required")
		return store.ParticipantRow{}, false
	}
	return p, true
}

// creatorTokenValid reports, in constant time, whether presented is v's
// creator token. An empty token never matches.
func creatorTokenValid(presented string, v store.VoteRow) bool {
	return presented != "" && subtle.ConstantTimeCompare([]byte(presented), []byte(v.CreatorToken)) == 1
}

// handleRemoveParticipant implements DELETE /api/votes/{slug}/participants/{id}
// (spec B1): the creator soft-removes a participant, wiping their ballot and,
// during the suggest phase, their suggestions. Once voting has started their
// suggestions are kept because others may have spent credits on them. Their
// session token is invalidated, so their live WebSockets fall back to the
// spectator view on the broadcast below.
func (s *Server) handleRemoveParticipant(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	defer s.lockSlug(slug)()
	v, ok := s.getVoteOr404(w, slug)
	if !ok {
		return
	}
	if rejectIfClosed(w, v) {
		return
	}
	creator, ok := s.requireCreator(w, r, v)
	if !ok {
		return
	}

	id := chi.URLParam(r, "id")
	deleteOptions := v.Phase == string(domain.PhaseSuggesting)
	if err := s.store.RemoveParticipant(slug, id, ids.NewToken(), deleteOptions, s.now().Unix()); err != nil {
		switch {
		case errors.Is(err, store.ErrIsCreator):
			writeError(w, http.StatusBadRequest, "the creator can't be removed")
		case errors.Is(err, store.ErrNotFound):
			writeError(w, http.StatusNotFound, "participant not found")
		default:
			writeError(w, http.StatusInternalServerError, "internal error")
		}
		return
	}

	// The removed participant may have been the last holdout for an
	// all-done / all-voted auto-advance.
	if settings, err := parseSettings(v); err == nil {
		switch v.Phase {
		case string(domain.PhaseSuggesting):
			s.maybeAutoAdvanceSuggest(slug, settings)
		case string(domain.PhaseVoting):
			s.maybeAutoAdvanceVote(slug, settings)
		}
	}
	s.changed(slug)
	s.writeState(w, r, slug, &creator)
}

// rejectIfClosed writes 409 "this vote is closed" and returns true when the
// creator has closed v (spec B3). Every mutating handler calls it right after
// loading the vote and before auth, so a closed room is read-only for
// everyone, the creator included, until it is reopened.
func rejectIfClosed(w http.ResponseWriter, v store.VoteRow) bool {
	if v.ClosedAt != nil {
		writeError(w, http.StatusConflict, "this vote is closed")
		return true
	}
	return false
}

// handleClose implements POST /api/votes/{slug}/close (spec B3). Closing is
// idempotent. It also drops any armed phase deadline, both stored and in the
// scheduler: a closed room must not move on its own, and reopening
// deliberately does not bring the timer back.
func (s *Server) handleClose(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	defer s.lockSlug(slug)()
	v, ok := s.getVoteOr404(w, slug)
	if !ok {
		return
	}
	creator, ok := s.requireCreator(w, r, v)
	if !ok {
		return
	}
	if v.ClosedAt == nil {
		// One UpdateVote writes closed_at and the cleared deadline together,
		// so a crash can't leave a closed vote with a deadline RearmTimers
		// would pick back up.
		now := s.now().Unix()
		v.ClosedAt = &now
		v.PhaseDeadline = nil
		if err := s.store.UpdateVote(v); err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		if s.scheduler != nil {
			s.scheduler.Clear(slug)
		}
	}
	s.changed(slug)
	s.writeState(w, r, slug, &creator)
}

// handleReopen implements POST /api/votes/{slug}/reopen (spec B3). Reopening
// an open vote is a no-op 200. No timer is re-armed; the phase continues
// under the creator's manual control.
func (s *Server) handleReopen(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	defer s.lockSlug(slug)()
	v, ok := s.getVoteOr404(w, slug)
	if !ok {
		return
	}
	creator, ok := s.requireCreator(w, r, v)
	if !ok {
		return
	}
	if v.ClosedAt != nil {
		if err := s.store.SetClosed(slug, nil); err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
	}
	s.changed(slug)
	s.writeState(w, r, slug, &creator)
}

type nextVoteRequest struct {
	Title    string         `json:"title"`
	Settings *settingsPatch `json:"settings"`
}

// handleNextVote implements POST /api/votes/{slug}/next (spec B4): the
// creator starts a follow-up vote with the same group. Every current
// (non-removed) participant is carried over with a fresh participant ID and
// session token; the old room then advertises the successor and hands each
// participant their own new token, so the group moves on without re-joining
// and without any old token working in the new vote. Settings default to
// this vote's, with an optional patch applied over them.
func (s *Server) handleNextVote(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	defer s.lockSlug(slug)()
	v, ok := s.getVoteOr404(w, slug)
	if !ok {
		return
	}
	if rejectIfClosed(w, v) {
		return
	}
	caller, ok := s.requireCreator(w, r, v)
	if !ok {
		return
	}
	// A link to a successor that no longer exists doesn't count; the new
	// follow-up replaces it.
	if v.HasNext() {
		writeError(w, http.StatusConflict, "this vote already has a follow-up")
		return
	}

	var req nextVoteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	title, msg := validateTitle(req.Title)
	if msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	base, err := parseSettings(v)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	settings := req.Settings.applyTo(base)
	if err := settings.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	next, err := s.newVoteRow(title, settings)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	// Participants excludes removed members, and names are already unique
	// within the source vote, so the carried group is valid as-is and within
	// the participant cap.
	parts, err := s.store.Participants(slug)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	carried := make(map[string]store.ParticipantRow, len(parts))
	for _, old := range parts {
		carried[old.ID] = store.ParticipantRow{
			ID:        ids.NewToken(),
			VoteSlug:  next.Slug,
			Name:      old.Name,
			Token:     ids.NewToken(),
			IsCreator: old.IsCreator,
			JoinedAt:  next.CreatedAt,
		}
	}
	if err := s.store.CreateNextVote(slug, next, carried); err != nil {
		if errors.Is(err, store.ErrConflict) {
			writeError(w, http.StatusConflict, "this vote already has a follow-up")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	s.armOrClear(next.Slug, next.PhaseDeadline)
	// The old room broadcasts the handoff to everyone still watching it.
	s.changed(slug)

	me := carried[caller.ID]
	d, err := s.loadRoom(next.Slug)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	// The caller just proved creator credentials and receives the new
	// creator token in this same response.
	writeJSON(w, http.StatusCreated, createVoteResponse{
		Slug:         next.Slug,
		CreatorToken: next.CreatorToken,
		SessionToken: me.Token,
		State:        d.stateFor(me.Token, true),
	})
}
