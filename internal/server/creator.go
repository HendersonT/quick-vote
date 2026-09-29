package server

import (
	"crypto/subtle"
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
	tokenOK := subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Creator-Token")), []byte(v.CreatorToken)) == 1
	if !p.IsCreator || !tokenOK {
		writeError(w, http.StatusForbidden, "creator token required")
		return store.ParticipantRow{}, false
	}
	return p, true
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
	s.writeState(w, slug, &creator)
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
	s.writeState(w, slug, &creator)
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
	s.writeState(w, slug, &creator)
}
