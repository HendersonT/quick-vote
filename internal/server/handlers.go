package server

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/HendersonT/quick-vote/internal/domain"
	"github.com/HendersonT/quick-vote/internal/ids"
	"github.com/HendersonT/quick-vote/internal/store"
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
	// VetoCost, VoteScalingExponent and RunoffFallback are the three
	// "advanced options" settings added after the original release (see
	// domain.Settings). Omitting them from the create request leaves
	// domain.DefaultSettings()'s explicit defaults in place.
	VetoCost            *int     `json:"vetoCost"`
	VoteScalingExponent *float64 `json:"voteScalingExponent"`
	RunoffFallback      *string  `json:"runoffFallback"`
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
	if p.VetoCost != nil {
		s.VetoCost = *p.VetoCost
	}
	if p.VoteScalingExponent != nil {
		s.VoteScalingExponent = *p.VoteScalingExponent
	}
	if p.RunoffFallback != nil {
		s.RunoffFallback = domain.Tiebreaker(*p.RunoffFallback)
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

	now := s.now()
	slug := ids.NewSlug()
	creatorToken := ids.NewToken()
	sessionToken := ids.NewToken()
	participantID := ids.NewToken()

	var deadline *int64
	if settings.SuggestTimerSecs > 0 {
		d := now.Add(time.Duration(settings.SuggestTimerSecs) * time.Second).Unix()
		deadline = &d
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

	s.armOrClear(slug, deadline)

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
	// Serialize joins per room so the participant cap below can't be raced.
	defer s.lockSlug(slug)()
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
	if len(existing) >= maxParticipantsPerVote {
		writeError(w, http.StatusConflict, "this vote is full")
		return
	}
	name = dedupeName(name, existing)

	sessionToken := ids.NewToken()
	p := store.ParticipantRow{
		ID:       ids.NewToken(),
		VoteSlug: slug,
		Name:     name,
		Token:    sessionToken,
		JoinedAt: s.now().Unix(),
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

// getVoteOr404 loads a vote, writing a 404/500 error response and returning
// ok=false when the lookup fails.
func (s *Server) getVoteOr404(w http.ResponseWriter, slug string) (store.VoteRow, bool) {
	v, err := s.store.GetVote(slug)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "vote not found")
		} else {
			writeError(w, http.StatusInternalServerError, "internal error")
		}
		return store.VoteRow{}, false
	}
	return v, true
}

// requireParticipant resolves the caller's session token to a participant of
// slug, writing a 401 error and returning ok=false when it is missing/invalid.
func (s *Server) requireParticipant(w http.ResponseWriter, r *http.Request, slug string) (store.ParticipantRow, bool) {
	token := bearerToken(r)
	if token == "" {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return store.ParticipantRow{}, false
	}
	p, err := s.store.ParticipantByToken(slug, token)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid session token")
		return store.ParticipantRow{}, false
	}
	return p, true
}

// parseSettings decodes the settings JSON stored on a vote row and applies
// Normalized() so a vote created before the "advanced options" round (whose
// stored JSON lacks voteScalingExponent/runoffFallback) still gets today's
// defaults for them instead of Go's zero value.
func parseSettings(v store.VoteRow) (domain.Settings, error) {
	var s domain.Settings
	if err := json.Unmarshal([]byte(v.Settings), &s); err != nil {
		return domain.Settings{}, err
	}
	return s.Normalized(), nil
}

// writeState reloads the full room state for slug and writes it as a 200
// response, personalized for requester (nil = spectator).
func (s *Server) writeState(w http.ResponseWriter, slug string, requester *store.ParticipantRow) {
	v, err := s.store.GetVote(slug)
	if err != nil {
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
	writeJSON(w, http.StatusOK, BuildRoomState(v, parts, opts, ballots, requester))
}

type suggestionRequest struct {
	Title string `json:"title"`
}

// handleCreateSuggestion implements POST /api/votes/{slug}/suggestions.
func (s *Server) handleCreateSuggestion(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	defer s.lockSlug(slug)()
	v, ok := s.getVoteOr404(w, slug)
	if !ok {
		return
	}
	p, ok := s.requireParticipant(w, r, slug)
	if !ok {
		return
	}
	if v.Phase != string(domain.PhaseSuggesting) {
		writeError(w, http.StatusConflict, "suggestions are closed")
		return
	}

	var req suggestionRequest
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

	settings, err := parseSettings(v)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	opts, err := s.store.Options(slug)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	mine := 0
	for _, o := range opts {
		if o.ParticipantID == p.ID {
			mine++
		}
		if strings.EqualFold(strings.TrimSpace(o.Title), title) {
			writeError(w, http.StatusConflict, "that suggestion already exists")
			return
		}
	}
	if mine >= settings.MaxSuggestionsPerUser {
		writeError(w, http.StatusConflict, "suggestion limit reached")
		return
	}

	o := store.OptionRow{
		ID:            ids.NewToken(),
		VoteSlug:      slug,
		ParticipantID: p.ID,
		Title:         title,
		CreatedAt:     s.now().Unix(),
	}
	if err := s.store.AddOption(o); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to add suggestion")
		return
	}

	s.maybeAutoAdvanceSuggest(slug, settings)
	s.changed(slug)
	s.writeState(w, slug, &p)
}

// handleDeleteSuggestion implements DELETE
// /api/votes/{slug}/suggestions/{id}. A participant may only delete their own
// suggestions, and only during the suggesting phase.
func (s *Server) handleDeleteSuggestion(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	defer s.lockSlug(slug)()
	v, ok := s.getVoteOr404(w, slug)
	if !ok {
		return
	}
	p, ok := s.requireParticipant(w, r, slug)
	if !ok {
		return
	}
	if v.Phase != string(domain.PhaseSuggesting) {
		writeError(w, http.StatusConflict, "suggestions are closed")
		return
	}
	id := chi.URLParam(r, "id")
	if err := s.store.DeleteOption(slug, id, p.ID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "suggestion not found or not yours")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	if settings, err := parseSettings(v); err == nil {
		// A deletion can only ever reduce a suggestion/suggester count, so
		// this re-check can't newly satisfy "count"/"suggestion-count" — but
		// it's cheap and keeps every mutation that could plausibly affect
		// auto-advance flowing through the same recheck path (see F2).
		s.maybeAutoAdvanceSuggest(slug, settings)
	}
	s.changed(slug)
	s.writeState(w, slug, &p)
}

type ballotRequest struct {
	Votes map[string]int `json:"votes"`
}

// handlePutBallot implements PUT /api/votes/{slug}/ballot.
func (s *Server) handlePutBallot(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	defer s.lockSlug(slug)()
	v, ok := s.getVoteOr404(w, slug)
	if !ok {
		return
	}
	p, ok := s.requireParticipant(w, r, slug)
	if !ok {
		return
	}
	if v.Phase != string(domain.PhaseVoting) {
		writeError(w, http.StatusConflict, "voting is not open")
		return
	}

	var req ballotRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Votes == nil {
		req.Votes = map[string]int{}
	}

	settings, err := parseSettings(v)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	opts, err := s.store.Options(slug)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	active, err := decodeActiveOptions(v)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	// During a runoff round (active_options set), only the tied options are
	// votable and the budget shrinks to match — everything else is
	// unreachable via optionIDs, so ValidateBallot rejects it as unknown.
	optionIDs := activeOptionIDs(opts, active)
	budget := settings.CreditsPerOption * len(optionIDs)
	if err := domain.ValidateBallot(req.Votes, optionIDs, budget, settings.VoteScalingExponent, settings.VetoCost); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	buf, err := json.Marshal(req.Votes)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := s.store.PutBallot(slug, p.ID, string(buf)); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save ballot")
		return
	}

	s.maybeAutoAdvanceVote(slug, settings)
	s.changed(slug)
	s.writeState(w, slug, &p)
}

type advanceRequest struct {
	WinnerOptionID string `json:"winnerOptionId"`
}

// handleAdvance implements POST /api/votes/{slug}/advance. It requires both a
// valid session token and a matching X-Creator-Token header.
func (s *Server) handleAdvance(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	defer s.lockSlug(slug)()
	v, ok := s.getVoteOr404(w, slug)
	if !ok {
		return
	}
	p, ok := s.requireParticipant(w, r, slug)
	if !ok {
		return
	}
	if subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Creator-Token")), []byte(v.CreatorToken)) != 1 {
		writeError(w, http.StatusForbidden, "creator token required")
		return
	}

	var req advanceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := s.advancePhase(slug, true, strings.TrimSpace(req.WinnerOptionID)); err != nil {
		switch {
		case errors.Is(err, errNeedTwoSuggestions):
			writeError(w, http.StatusConflict, "need at least 2 suggestions")
		case errors.Is(err, errAlreadyResults):
			writeError(w, http.StatusConflict, "already at results")
		case errors.Is(err, errNoTiePending):
			writeError(w, http.StatusConflict, "no tiebreak is pending")
		case errors.Is(err, errBadTiebreakWinner):
			writeError(w, http.StatusBadRequest, "winnerOptionId must be one of the tied options")
		default:
			writeError(w, http.StatusInternalServerError, "internal error")
		}
		return
	}

	s.changed(slug)
	s.writeState(w, slug, &p)
}

// handleRevote implements POST /api/votes/{slug}/revote. It toggles the
// caller's re-vote call; if the threshold is met the vote returns to voting
// with cleared ballots.
func (s *Server) handleRevote(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	defer s.lockSlug(slug)()
	v, ok := s.getVoteOr404(w, slug)
	if !ok {
		return
	}
	p, ok := s.requireParticipant(w, r, slug)
	if !ok {
		return
	}
	if v.Phase != string(domain.PhaseResults) {
		writeError(w, http.StatusConflict, "re-vote can only be called in the results phase")
		return
	}

	if err := s.store.SetWantsRevote(p.ID, !p.WantsRevote); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	settings, err := parseSettings(v)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	parts, err := s.store.Participants(slug)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	calls := 0
	for _, pp := range parts {
		if pp.WantsRevote {
			calls++
		}
	}
	if domain.RevoteMet(calls, len(parts), settings.RevoteThresholdPct) {
		if err := s.store.DeleteBallots(slug); err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		if err := s.store.ResetRevotes(slug); err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		v.Phase = string(domain.PhaseVoting)
		v.Results = nil
		v.PhaseDeadline = nil
		// A threshold re-vote always resets to a clean slate: every option
		// (not just whatever was active during a prior runoff) is back in
		// play, per F5.
		v.ActiveOptions = nil
		if settings.VoteTimerSecs > 0 {
			d := s.now().Add(time.Duration(settings.VoteTimerSecs) * time.Second).Unix()
			v.PhaseDeadline = &d
		}
		if err := s.store.UpdateVote(v); err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		s.armOrClear(slug, v.PhaseDeadline)
	}

	s.changed(slug)
	s.writeState(w, slug, &p)
}

// handleDoneSuggesting implements POST /api/votes/{slug}/done-suggesting: it
// toggles the caller's "I'm done suggesting" flag (F1). Valid only during the
// suggesting phase. Toggling does not itself add/remove suggestions and is
// not cleared by later suggestion activity — it's a purely explicit signal
// that maybeAutoAdvanceSuggest's "all-done" mode reads.
func (s *Server) handleDoneSuggesting(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	defer s.lockSlug(slug)()
	v, ok := s.getVoteOr404(w, slug)
	if !ok {
		return
	}
	p, ok := s.requireParticipant(w, r, slug)
	if !ok {
		return
	}
	if v.Phase != string(domain.PhaseSuggesting) {
		writeError(w, http.StatusConflict, "done-suggesting only applies during the suggesting phase")
		return
	}

	if err := s.store.SetDoneSuggesting(p.ID, !p.DoneSuggesting); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	if settings, err := parseSettings(v); err == nil {
		s.maybeAutoAdvanceSuggest(slug, settings)
	}
	s.changed(slug)
	s.writeState(w, slug, &p)
}

// maybeAutoAdvanceSuggest advances suggesting -> voting when the configured
// suggest-advance rule is satisfied (F2). All modes still require at least 2
// options total; advancePhase itself enforces that (errNeedTwoSuggestions),
// this is just a cheap short-circuit to avoid a pointless attempt.
func (s *Server) maybeAutoAdvanceSuggest(slug string, settings domain.Settings) {
	opts, err := s.store.Options(slug)
	if err != nil || len(opts) < 2 {
		return
	}

	switch settings.SuggestAdvanceMode {
	case "count":
		// N distinct participants have suggested (unchanged legacy semantics).
		if settings.SuggestAdvanceCount < 1 {
			return
		}
		distinct := make(map[string]bool, len(opts))
		for _, o := range opts {
			distinct[o.ParticipantID] = true
		}
		if len(distinct) < settings.SuggestAdvanceCount {
			return
		}
	case "suggestion-count":
		// The total number of suggestions (not distinct suggesters) has
		// reached the configured count.
		if settings.SuggestAdvanceCount < 1 || len(opts) < settings.SuggestAdvanceCount {
			return
		}
	case "all-done":
		// Every current participant has explicitly marked themselves done,
		// regardless of how many suggestions (if any) they made.
		parts, err := s.store.Participants(slug)
		if err != nil || len(parts) == 0 {
			return
		}
		for _, pp := range parts {
			if !pp.DoneSuggesting {
				return
			}
		}
	default:
		return // "manual": never auto-advances.
	}
	_ = s.advancePhase(slug, false, "")
}

// maybeAutoAdvanceVote advances voting -> results when the all-voted rule is
// configured and every current participant has submitted a ballot.
func (s *Server) maybeAutoAdvanceVote(slug string, settings domain.Settings) {
	if settings.VoteAdvanceMode != "all-voted" {
		return
	}
	parts, err := s.store.Participants(slug)
	if err != nil || len(parts) == 0 {
		return
	}
	ballots, err := s.store.Ballots(slug)
	if err != nil {
		return
	}
	for _, p := range parts {
		if _, ok := ballots[p.ID]; !ok {
			return
		}
	}
	_ = s.advancePhase(slug, false, "")
}
