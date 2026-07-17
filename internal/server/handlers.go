package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/quickvote/quickvote/internal/domain"
	"github.com/quickvote/quickvote/internal/ids"
	"github.com/quickvote/quickvote/internal/store"
)

type errorResponse struct {
	Error string `json:"error"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, errorResponse{Error: msg})
}

// bearerToken extracts the session token from an "Authorization: Bearer
// <token>" header, or "" if absent/malformed.
func bearerToken(r *http.Request) string {
	const prefix = "Bearer "
	h := r.Header.Get("Authorization")
	if strings.HasPrefix(h, prefix) {
		return strings.TrimPrefix(h, prefix)
	}
	return ""
}

// settingsPatch mirrors domain.Settings with every field optional, so a
// creation request can override only the fields it cares about while the
// rest fall back to domain.DefaultSettings().
type settingsPatch struct {
	MaxSuggestionsPerUser *int    `json:"maxSuggestionsPerUser"`
	CreditsPerOption      *int    `json:"creditsPerOption"`
	SuggestAdvanceMode    *string `json:"suggestAdvanceMode"`
	SuggestAdvanceCount   *int    `json:"suggestAdvanceCount"`
	VoteAdvanceMode       *string `json:"voteAdvanceMode"`
	SurvivalThreshold     *int    `json:"survivalThreshold"`
	Tiebreaker            *string `json:"tiebreaker"`
	RevoteThresholdPct    *int    `json:"revoteThresholdPct"`
	SuggestTimerSecs      *int    `json:"suggestTimerSecs"`
	VoteTimerSecs         *int    `json:"voteTimerSecs"`
}

func (p *settingsPatch) applyTo(s domain.Settings) domain.Settings {
	if p == nil {
		return s
	}
	if p.MaxSuggestionsPerUser != nil {
		s.MaxSuggestionsPerUser = *p.MaxSuggestionsPerUser
	}
	if p.CreditsPerOption != nil {
		s.CreditsPerOption = *p.CreditsPerOption
	}
	if p.SuggestAdvanceMode != nil {
		s.SuggestAdvanceMode = *p.SuggestAdvanceMode
	}
	if p.SuggestAdvanceCount != nil {
		s.SuggestAdvanceCount = *p.SuggestAdvanceCount
	}
	if p.VoteAdvanceMode != nil {
		s.VoteAdvanceMode = *p.VoteAdvanceMode
	}
	if p.SurvivalThreshold != nil {
		s.SurvivalThreshold = *p.SurvivalThreshold
	}
	if p.Tiebreaker != nil {
		s.Tiebreaker = domain.Tiebreaker(*p.Tiebreaker)
	}
	if p.RevoteThresholdPct != nil {
		s.RevoteThresholdPct = *p.RevoteThresholdPct
	}
	if p.SuggestTimerSecs != nil {
		s.SuggestTimerSecs = *p.SuggestTimerSecs
	}
	if p.VoteTimerSecs != nil {
		s.VoteTimerSecs = *p.VoteTimerSecs
	}
	return s
}

type createVoteRequest struct {
	Title       string         `json:"title"`
	CreatorName string         `json:"creatorName"`
	Settings    *settingsPatch `json:"settings"`
}

type createVoteResponse struct {
	Slug         string         `json:"slug"`
	CreatorToken string         `json:"creatorToken"`
	SessionToken string         `json:"sessionToken"`
	State        map[string]any `json:"state"`
}

// handleCreateVote implements POST /api/votes.
func (s *Server) handleCreateVote(w http.ResponseWriter, r *http.Request) {
	var req createVoteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	title := strings.TrimSpace(req.Title)
	if title == "" {
		writeError(w, http.StatusBadRequest, "title is required")
		return
	}
	if len(title) > 200 {
		writeError(w, http.StatusBadRequest, "title must be at most 200 characters")
		return
	}

	creatorName := strings.TrimSpace(req.CreatorName)
	if creatorName == "" {
		writeError(w, http.StatusBadRequest, "creatorName is required")
		return
	}
	if len(creatorName) > 50 {
		writeError(w, http.StatusBadRequest, "creatorName must be at most 50 characters")
		return
	}

	settings := req.Settings.applyTo(domain.DefaultSettings())
	if err := settings.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	settingsJSON, err := json.Marshal(settings)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	now := time.Now()
	slug := ids.NewSlug()
	creatorToken := ids.NewToken()
	sessionToken := ids.NewToken()
	participantID := ids.NewToken()

	var deadline *int64
	if settings.SuggestTimerSecs > 0 {
		d := now.Add(time.Duration(settings.SuggestTimerSecs) * time.Second).Unix()
		deadline = &d
		// TODO(Task 7): register this deadline with the timer scheduler.
	}

	v := store.VoteRow{
		Slug:          slug,
		Title:         title,
		Phase:         string(domain.PhaseSuggesting),
		Settings:      string(settingsJSON),
		CreatorToken:  creatorToken,
		PhaseDeadline: deadline,
		Results:       nil,
		CreatedAt:     now.Unix(),
	}
	if err := s.store.CreateVote(v); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create vote")
		return
	}

	p := store.ParticipantRow{
		ID:        participantID,
		VoteSlug:  slug,
		Name:      creatorName,
		Token:     sessionToken,
		IsCreator: true,
		JoinedAt:  now.Unix(),
	}
	if err := s.store.AddParticipant(p); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to add creator")
		return
	}

	state := BuildRoomState(v, []store.ParticipantRow{p}, nil, map[string]map[string]int{}, &p)

	writeJSON(w, http.StatusCreated, createVoteResponse{
		Slug:         slug,
		CreatorToken: creatorToken,
		SessionToken: sessionToken,
		State:        state,
	})
}

type joinRequest struct {
	Name string `json:"name"`
}

type joinResponse struct {
	SessionToken string         `json:"sessionToken"`
	State        map[string]any `json:"state"`
}

// handleJoin implements POST /api/votes/{slug}/join. Joining is allowed in
// any phase.
func (s *Server) handleJoin(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	v, err := s.store.GetVote(slug)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "vote not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	var req joinRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if len(name) > 50 {
		writeError(w, http.StatusBadRequest, "name must be at most 50 characters")
		return
	}

	existing, err := s.store.Participants(slug)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	name = dedupeName(name, existing)

	sessionToken := ids.NewToken()
	p := store.ParticipantRow{
		ID:       ids.NewToken(),
		VoteSlug: slug,
		Name:     name,
		Token:    sessionToken,
		JoinedAt: time.Now().Unix(),
	}
	if err := s.store.AddParticipant(p); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to join")
		return
	}

	opts, err := s.store.Options(slug)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	ballots, err := s.store.Ballots(slug)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	parts := append(existing, p)

	state := BuildRoomState(v, parts, opts, ballots, &p)

	writeJSON(w, http.StatusOK, joinResponse{SessionToken: sessionToken, State: state})
}

// dedupeName appends " (2)", " (3)", ... until name is unique among existing
// participants' names.
func dedupeName(name string, existing []store.ParticipantRow) string {
	taken := make(map[string]bool, len(existing))
	for _, p := range existing {
		taken[p.Name] = true
	}
	if !taken[name] {
		return name
	}
	for i := 2; ; i++ {
		candidate := name + " (" + strconv.Itoa(i) + ")"
		if !taken[candidate] {
			return candidate
		}
	}
}

// handleGetVote implements GET /api/votes/{slug}. The Authorization Bearer
// token is optional; a missing or invalid token yields a spectator snapshot
// ("you": null) rather than an error.
func (s *Server) handleGetVote(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	v, err := s.store.GetVote(slug)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "vote not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	parts, err := s.store.Participants(slug)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	opts, err := s.store.Options(slug)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	ballots, err := s.store.Ballots(slug)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	var requester *store.ParticipantRow
	if token := bearerToken(r); token != "" {
		if p, err := s.store.ParticipantByToken(slug, token); err == nil {
			requester = &p
		}
	}

	state := BuildRoomState(v, parts, opts, ballots, requester)

	writeJSON(w, http.StatusOK, state)
}
