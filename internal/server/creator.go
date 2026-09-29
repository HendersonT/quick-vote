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
