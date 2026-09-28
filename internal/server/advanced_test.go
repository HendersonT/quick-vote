package server_test

import (
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/HendersonT/quick-vote/internal/server"
	"github.com/HendersonT/quick-vote/internal/store"
)

// doneSuggestingFor reads a participant's doneSuggesting flag out of a room
// state's participants array.
func doneSuggestingFor(t *testing.T, state map[string]any, participantID string) bool {
	t.Helper()
	for _, raw := range state["participants"].([]any) {
		p := raw.(map[string]any)
		if p["id"] == participantID {
			return p["doneSuggesting"].(bool)
		}
	}
	t.Fatalf("participant %q not found in state: %v", participantID, state)
	return false
}

func TestDoneSuggestingToggle(t *testing.T) {
	s := newTestServer(t)
	slug, _, aliceTok, state := createVote(t, s, nil)
	aliceID := state["you"].(map[string]any)["participantId"].(string)

	rec, out := doJSON(t, s, http.MethodPost, "/api/votes/"+slug+"/done-suggesting", map[string]any{}, aliceTok)
	if rec.Code != http.StatusOK {
		t.Fatalf("toggle on: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !doneSuggestingFor(t, out, aliceID) {
		t.Fatalf("doneSuggesting = false after toggle, want true")
	}

	// Toggling again resumes suggesting.
	rec, out = doJSON(t, s, http.MethodPost, "/api/votes/"+slug+"/done-suggesting", map[string]any{}, aliceTok)
	if rec.Code != http.StatusOK {
		t.Fatalf("toggle off: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if doneSuggestingFor(t, out, aliceID) {
		t.Fatalf("doneSuggesting = true after second toggle, want false")
	}

	// Adding a suggestion afterwards does not clear the flag, and marking
	// done doesn't block further suggesting.
	rec, _ = doJSON(t, s, http.MethodPost, "/api/votes/"+slug+"/done-suggesting", map[string]any{}, aliceTok)
	if rec.Code != http.StatusOK {
		t.Fatalf("toggle on again: %d", rec.Code)
	}
	rec, out = suggest(t, s, slug, aliceTok, "Catan")
	if rec.Code != http.StatusOK {
		t.Fatalf("suggest after done: %d", rec.Code)
	}
	if !doneSuggestingFor(t, out, aliceID) {
		t.Fatalf("doneSuggesting should survive adding a suggestion")
	}

	// Only valid during the suggesting phase.
	slug2, _, aliceTok2, _ := setupVoting(t, s, nil)
	rec, _ = doJSON(t, s, http.MethodPost, "/api/votes/"+slug2+"/done-suggesting", map[string]any{}, aliceTok2)
	if rec.Code != http.StatusConflict {
		t.Fatalf("done-suggesting in voting phase: status = %d, want 409", rec.Code)
	}
}

func TestAllDoneAutoAdvance(t *testing.T) {
	s := newTestServer(t)
	slug, _, aliceTok, _ := createVote(t, s, map[string]any{"suggestAdvanceMode": "all-done"})
	bobTok := join(t, s, slug, "Bob")

	if rec, _ := suggest(t, s, slug, aliceTok, "Catan"); rec.Code != http.StatusOK {
		t.Fatalf("suggest A: %d", rec.Code)
	}
	if rec, _ := suggest(t, s, slug, aliceTok, "Wingspan"); rec.Code != http.StatusOK {
		t.Fatalf("suggest B: %d", rec.Code)
	}

	// Alice marks done; Bob hasn't -> stays suggesting even with >=2 options.
	rec, out := doJSON(t, s, http.MethodPost, "/api/votes/"+slug+"/done-suggesting", map[string]any{}, aliceTok)
	if rec.Code != http.StatusOK {
		t.Fatalf("alice done: %d", rec.Code)
	}
	if out["phase"] != "suggesting" {
		t.Fatalf("phase = %v, want suggesting (Bob not done yet)", out["phase"])
	}

	// Bob marks done too -> every participant done -> auto-advance.
	rec, out = doJSON(t, s, http.MethodPost, "/api/votes/"+slug+"/done-suggesting", map[string]any{}, bobTok)
	if rec.Code != http.StatusOK {
		t.Fatalf("bob done: %d", rec.Code)
	}
	if out["phase"] != "voting" {
		t.Fatalf("phase = %v, want voting once every participant is done", out["phase"])
	}
}

func TestSuggestionCountAutoAdvance(t *testing.T) {
	s := newTestServer(t)
	slug, _, aliceTok, _ := createVote(t, s, map[string]any{
		"suggestAdvanceMode":  "suggestion-count",
		"suggestAdvanceCount": 3,
	})

	if rec, st := suggest(t, s, slug, aliceTok, "One"); rec.Code != http.StatusOK || st["phase"] != "suggesting" {
		t.Fatalf("first suggestion: status = %d, phase = %v", rec.Code, st["phase"])
	}
	if rec, st := suggest(t, s, slug, aliceTok, "Two"); rec.Code != http.StatusOK || st["phase"] != "suggesting" {
		t.Fatalf("second suggestion: status = %d, phase = %v", rec.Code, st["phase"])
	}
	// Third suggestion (still a single suggester — "suggestion-count" counts
	// total suggestions, unlike "count"'s distinct-suggesters rule) reaches
	// the configured total and auto-advances.
	rec, st := suggest(t, s, slug, aliceTok, "Three")
	if rec.Code != http.StatusOK {
		t.Fatalf("third suggestion: %d", rec.Code)
	}
	if st["phase"] != "voting" {
		t.Fatalf("phase = %v, want voting once total suggestions hit the count", st["phase"])
	}
}

func TestVetoBallotEndToEnd(t *testing.T) {
	s := newTestServer(t)
	slug, _, aliceTok, ids := setupVoting(t, s, map[string]any{
		"vetoCost":         2,
		"creditsPerOption": 3,
	})
	bobTok := join(t, s, slug, "Bob")

	// Alice: 2 credits on A, vetoes B. Cost = cost(2) + vetoCost = 4 + 2 = 6,
	// exactly the budget (3 * 2 options).
	rec, _ := putBallot(t, s, slug, aliceTok, map[string]int{ids["A"]: 2, ids["B"]: -1})
	if rec.Code != http.StatusOK {
		t.Fatalf("alice veto ballot: status = %d, body = %s", rec.Code, rec.Body.String())
	}

	// Bob backs B, unaware it's been vetoed.
	rec, out := putBallot(t, s, slug, bobTok, map[string]int{ids["B"]: 2})
	if rec.Code != http.StatusOK {
		t.Fatalf("bob ballot: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if out["phase"] != "results" {
		t.Fatalf("phase = %v, want results once both have voted", out["phase"])
	}

	results := out["results"].(map[string]any)
	byID := map[string]map[string]any{}
	for _, raw := range results["scores"].([]any) {
		sc := raw.(map[string]any)
		byID[sc["optionId"].(string)] = sc
	}
	b := byID[ids["B"]]
	if int(b["vetoCount"].(float64)) != 1 {
		t.Fatalf("B vetoCount = %v, want 1", b["vetoCount"])
	}
	if b["eliminated"] != true {
		t.Fatalf("B eliminated = %v, want true — a veto is fatal regardless of score", b["eliminated"])
	}
	if int(b["score"].(float64)) != 2 {
		t.Fatalf("B score = %v, want 2 (Bob's positive votes still display)", b["score"])
	}
	if results["winnerOptionId"] != ids["A"] {
		t.Fatalf("winner = %v, want A (%s); B is vetoed despite Bob's votes", results["winnerOptionId"], ids["A"])
	}
}

func TestVetoRejectedWhenDisabled(t *testing.T) {
	s := newTestServer(t)
	slug, _, aliceTok, ids := setupVoting(t, s, nil) // vetoCost defaults to 0 (disabled)
	rec, out := putBallot(t, s, slug, aliceTok, map[string]int{ids["A"]: -1})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (veto disabled by default)", rec.Code)
	}
	if msg, _ := out["error"].(string); msg == "" {
		t.Fatalf("expected a non-empty error message, got %v", out)
	}
}

func TestVetoRejectsBelowNegativeOne(t *testing.T) {
	s := newTestServer(t)
	slug, _, aliceTok, ids := setupVoting(t, s, map[string]any{"vetoCost": 1})
	rec, _ := putBallot(t, s, slug, aliceTok, map[string]int{ids["A"]: -2})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (values below -1 are always rejected)", rec.Code)
	}
}

// TestRunoffEndToEnd drives tiebreaker "runoff" through a full round trip: a
// tie triggers an automatic runoff restricted to the tied options (with
// ballots cleared and the budget shrunk), and a second tie within the runoff
// itself resolves deterministically via the "earliest" fallback.
func TestRunoffEndToEnd(t *testing.T) {
	s := newTestServer(t)
	slug, creatorTok, aliceTok, _ := createVote(t, s, map[string]any{
		"tiebreaker":     "runoff",
		"runoffFallback": "earliest",
	})
	bobTok := join(t, s, slug, "Bob")

	for _, title := range []string{"A", "B", "C"} {
		if rec, _ := suggest(t, s, slug, aliceTok, title); rec.Code != http.StatusOK {
			t.Fatalf("suggest %s: %d", title, rec.Code)
		}
	}
	rec, adv := doHdr(t, s, http.MethodPost, "/api/votes/"+slug+"/advance", map[string]any{}, aliceTok, creatorTok)
	if rec.Code != http.StatusOK {
		t.Fatalf("advance to voting: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	ids := optionTitleToID(t, adv)

	// Round 1: Alice backs A, Bob backs B -> tie at score 1 each; C (score 0)
	// is eliminated by the default survival threshold and out of the tie.
	if rec, _ := putBallot(t, s, slug, aliceTok, map[string]int{ids["A"]: 1}); rec.Code != http.StatusOK {
		t.Fatalf("alice round 1: %d", rec.Code)
	}
	rec, out := putBallot(t, s, slug, bobTok, map[string]int{ids["B"]: 1})
	if rec.Code != http.StatusOK {
		t.Fatalf("bob round 1: status = %d, body = %s", rec.Code, rec.Body.String())
	}

	// A fresh tie under tiebreaker "runoff" stays in voting, not results.
	if out["phase"] != "voting" {
		t.Fatalf("phase = %v, want voting (runoff round pending), body = %v", out["phase"], out)
	}
	if out["runoff"] != true {
		t.Fatalf("runoff = %v, want true", out["runoff"])
	}
	if wantBudget := float64(3 * 2); out["budget"] != wantBudget {
		t.Fatalf("budget = %v, want %v (shrunk to the 2 active options)", out["budget"], wantBudget)
	}
	activeFlags := map[string]bool{}
	for _, raw := range out["options"].([]any) {
		o := raw.(map[string]any)
		activeFlags[o["title"].(string)] = o["active"].(bool)
	}
	if !activeFlags["A"] || !activeFlags["B"] || activeFlags["C"] {
		t.Fatalf("active flags = %v, want only A and B active", activeFlags)
	}
	for _, raw := range out["participants"].([]any) {
		p := raw.(map[string]any)
		if p["hasVoted"] != false {
			t.Fatalf("participant %v hasVoted should be false — ballots are cleared for the runoff", p["name"])
		}
	}

	// C is no longer votable once the runoff has restricted the active set.
	rec, _ = putBallot(t, s, slug, aliceTok, map[string]int{ids["C"]: 1})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("voting for an inactive (non-runoff) option: status = %d, want 400", rec.Code)
	}

	// Round 2 (the runoff itself): tie again -> resolved by the "earliest"
	// fallback, deterministically picking A (suggested before B).
	if rec, _ := putBallot(t, s, slug, aliceTok, map[string]int{ids["A"]: 1}); rec.Code != http.StatusOK {
		t.Fatalf("alice round 2: %d", rec.Code)
	}
	rec, out = putBallot(t, s, slug, bobTok, map[string]int{ids["B"]: 1})
	if rec.Code != http.StatusOK {
		t.Fatalf("bob round 2: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if out["phase"] != "results" {
		t.Fatalf("phase = %v, want results after the runoff resolves", out["phase"])
	}
	results := out["results"].(map[string]any)
	if results["winnerOptionId"] != ids["A"] {
		t.Fatalf("winner = %v, want A (%s) via the earliest fallback", results["winnerOptionId"], ids["A"])
	}
	if note, _ := results["tiebreakNote"].(string); note == "" {
		t.Fatalf("expected a non-empty tiebreakNote, got %v", results["tiebreakNote"])
	}
}

// TestRunoffEndToEndCreatorFallback drives tiebreaker "runoff" with
// runoffFallback "creator": a tie triggers a runoff, and a second tie within
// the runoff itself pends for the creator to resolve (rather than resolving
// automatically). Once the creator resolves it, the final tiebreakNote must
// be distinguishable from a plain (non-runoff) creator tiebreak.
func TestRunoffEndToEndCreatorFallback(t *testing.T) {
	s := newTestServer(t)
	slug, creatorTok, aliceTok, _ := createVote(t, s, map[string]any{
		"tiebreaker":     "runoff",
		"runoffFallback": "creator",
	})
	bobTok := join(t, s, slug, "Bob")

	for _, title := range []string{"A", "B", "C"} {
		if rec, _ := suggest(t, s, slug, aliceTok, title); rec.Code != http.StatusOK {
			t.Fatalf("suggest %s: %d", title, rec.Code)
		}
	}
	rec, adv := doHdr(t, s, http.MethodPost, "/api/votes/"+slug+"/advance", map[string]any{}, aliceTok, creatorTok)
	if rec.Code != http.StatusOK {
		t.Fatalf("advance to voting: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	ids := optionTitleToID(t, adv)

	// Round 1: Alice backs A, Bob backs B -> tie at score 1 each; C (score 0)
	// is eliminated by the default survival threshold and out of the tie.
	if rec, _ := putBallot(t, s, slug, aliceTok, map[string]int{ids["A"]: 1}); rec.Code != http.StatusOK {
		t.Fatalf("alice round 1: %d", rec.Code)
	}
	rec, out := putBallot(t, s, slug, bobTok, map[string]int{ids["B"]: 1})
	if rec.Code != http.StatusOK {
		t.Fatalf("bob round 1: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if out["phase"] != "voting" {
		t.Fatalf("phase = %v, want voting (runoff round pending), body = %v", out["phase"], out)
	}

	// Round 2 (the runoff itself): tie again -> "creator" fallback pends
	// rather than resolving automatically.
	if rec, _ := putBallot(t, s, slug, aliceTok, map[string]int{ids["A"]: 1}); rec.Code != http.StatusOK {
		t.Fatalf("alice round 2: %d", rec.Code)
	}
	rec, out = putBallot(t, s, slug, bobTok, map[string]int{ids["B"]: 1})
	if rec.Code != http.StatusOK {
		t.Fatalf("bob round 2: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if out["phase"] != "results" {
		t.Fatalf("phase = %v, want results after the runoff ties again", out["phase"])
	}
	results := out["results"].(map[string]any)
	if results["tiePending"] != true {
		t.Fatalf("tiePending = %v, want true (creator fallback pending)", results["tiePending"])
	}

	// The creator resolves the pending tie.
	rec, out = doHdr(t, s, http.MethodPost, "/api/votes/"+slug+"/advance",
		map[string]any{"winnerOptionId": ids["A"]}, aliceTok, creatorTok)
	if rec.Code != http.StatusOK {
		t.Fatalf("creator resolves tie: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	results = out["results"].(map[string]any)
	if results["winnerOptionId"] != ids["A"] {
		t.Fatalf("winner = %v, want A (%s)", results["winnerOptionId"], ids["A"])
	}
	const wantNote = "tie broken after runoff by the creator"
	if note, _ := results["tiebreakNote"].(string); note != wantNote {
		t.Fatalf("tiebreakNote = %q, want %q (distinguishable from a plain creator tiebreak)", note, wantNote)
	}
}

// TestLegacySettingsNormalization seeds a vote row directly with the settings
// JSON shape from before the "advanced options" round (no vetoCost,
// voteScalingExponent or runoffFallback keys at all) and checks that both the
// room-state snapshot and ballot validation apply today's defaults instead of
// silently decoding to Go's zero value.
func TestLegacySettingsNormalization(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "legacy.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	s := server.New(st, nil)

	const legacySettings = `{"maxSuggestionsPerUser":3,"creditsPerOption":3,` +
		`"suggestAdvanceMode":"manual","voteAdvanceMode":"all-voted",` +
		`"survivalThreshold":1,"tiebreaker":"most-backers","revoteThresholdPct":33}`
	const slug = "legacy-vote"
	const sessionToken = "legacy-session-token"
	const participantID = "legacy-participant"
	now := time.Now().Unix()

	if err := st.CreateVote(store.VoteRow{
		Slug:         slug,
		Title:        "Legacy vote",
		Phase:        "suggesting",
		Settings:     legacySettings,
		CreatorToken: "legacy-creator-token",
		CreatedAt:    now,
	}); err != nil {
		t.Fatalf("create legacy vote: %v", err)
	}
	if err := st.AddParticipant(store.ParticipantRow{
		ID: participantID, VoteSlug: slug, Name: "Alice",
		Token: sessionToken, IsCreator: true, JoinedAt: now,
	}); err != nil {
		t.Fatalf("add participant: %v", err)
	}
	if err := st.AddOption(store.OptionRow{ID: "optA", VoteSlug: slug, ParticipantID: participantID, Title: "A", CreatedAt: now}); err != nil {
		t.Fatalf("add option A: %v", err)
	}
	if err := st.AddOption(store.OptionRow{ID: "optB", VoteSlug: slug, ParticipantID: participantID, Title: "B", CreatedAt: now + 1}); err != nil {
		t.Fatalf("add option B: %v", err)
	}

	rec, out := doJSON(t, s, http.MethodGet, "/api/votes/"+slug, nil, sessionToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("get vote: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	settings := out["settings"].(map[string]any)
	if settings["voteScalingExponent"] != float64(2) {
		t.Fatalf("voteScalingExponent = %v, want 2 (normalized default)", settings["voteScalingExponent"])
	}
	if settings["runoffFallback"] != "random" {
		t.Fatalf("runoffFallback = %v, want %q (normalized default)", settings["runoffFallback"], "random")
	}

	// Move the legacy vote into voting directly via the store (its own
	// UpdateVote path is layer-tested elsewhere; here we just need a phase
	// that accepts a ballot).
	v, err := st.GetVote(slug)
	if err != nil {
		t.Fatalf("get vote: %v", err)
	}
	v.Phase = "voting"
	if err := st.UpdateVote(v); err != nil {
		t.Fatalf("move legacy vote to voting: %v", err)
	}

	// budget = creditsPerOption(3) * 2 options = 6. Putting 5 credits on one
	// option costs ceil(5^2 - 1e-9) = 25 under the normalized quadratic
	// exponent, and must be rejected as over budget. If Normalized() were
	// NOT applied, the legacy zero-valued exponent would instead price it at
	// ceil(5^0 - 1e-9) = 1, well within budget — so this failing as expected
	// is exactly what proves normalization ran.
	rec, out = putBallot(t, s, slug, sessionToken, map[string]int{"optA": 5})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (over budget under the normalized quadratic cost), body = %v", rec.Code, out)
	}
}
