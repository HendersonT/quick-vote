package server_test

import (
	"database/sql"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/HendersonT/quick-vote/internal/clock"
	"github.com/HendersonT/quick-vote/internal/server"
	"github.com/HendersonT/quick-vote/internal/store"
)

func TestRemoveParticipantWipesAndInvalidates(t *testing.T) {
	s := newTestServer(t)
	slug, ct, aliceTok, _ := createVote(t, s, map[string]any{"suggestAdvanceMode": "manual"})
	bobTok := join(t, s, slug, "Bob")
	suggest(t, s, slug, bobTok, "Bob's idea")
	bobID := participantID(t, getState(t, s, slug, bobTok))

	rec, st := doHdr(t, s, http.MethodDelete, "/api/votes/"+slug+"/participants/"+bobID, nil, aliceTok, ct)
	if rec.Code != http.StatusOK {
		t.Fatalf("remove: %d %s", rec.Code, rec.Body.String())
	}
	if n := len(st["participants"].([]any)); n != 1 {
		t.Fatalf("participants after remove = %d, want 1", n)
	}
	if n := len(st["options"].([]any)); n != 0 {
		t.Fatalf("suggest-phase removal must delete their options, have %d", n)
	}
	if got := getState(t, s, slug, bobTok); got["you"] != nil {
		t.Fatal("removed participant's token must no longer authenticate")
	}
	if rec, _ := suggest(t, s, slug, bobTok, "again"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("removed participant write: %d, want 401", rec.Code)
	}
}

func TestRemoveParticipantDuringVotingKeepsOptions(t *testing.T) {
	s := newTestServer(t)
	slug, ct, aliceTok, _ := createVote(t, s, map[string]any{"suggestAdvanceMode": "manual", "voteAdvanceMode": "manual"})
	bobTok := join(t, s, slug, "Bob")
	suggest(t, s, slug, bobTok, "Bob's idea")
	suggest(t, s, slug, aliceTok, "Alice's idea")
	if rec, _ := doHdr(t, s, http.MethodPost, "/api/votes/"+slug+"/advance", map[string]any{}, aliceTok, ct); rec.Code != http.StatusOK {
		t.Fatalf("advance: %d %s", rec.Code, rec.Body.String())
	}
	ids := optionTitleToID(t, getState(t, s, slug, aliceTok))
	if rec, _ := putBallot(t, s, slug, aliceTok, map[string]int{ids["Bob's idea"]: 1}); rec.Code != http.StatusOK {
		t.Fatalf("alice ballot: %d %s", rec.Code, rec.Body.String())
	}
	if rec, _ := putBallot(t, s, slug, bobTok, map[string]int{ids["Alice's idea"]: 1}); rec.Code != http.StatusOK {
		t.Fatalf("bob ballot: %d %s", rec.Code, rec.Body.String())
	}
	bobID := participantID(t, getState(t, s, slug, bobTok))

	rec, st := doHdr(t, s, http.MethodDelete, "/api/votes/"+slug+"/participants/"+bobID, nil, aliceTok, ct)
	if rec.Code != http.StatusOK {
		t.Fatalf("remove: %d", rec.Code)
	}
	if n := len(st["options"].([]any)); n != 2 {
		t.Fatalf("voting-phase removal must keep options (others voted on them), have %d", n)
	}
	you := st["you"].(map[string]any)
	if b := you["ballot"].(map[string]any); b[ids["Bob's idea"]] != float64(1) {
		t.Fatalf("Alice's ballot changed: %v", b)
	}
}

func TestRemoveParticipantAuth(t *testing.T) {
	s := newTestServer(t)
	slug, ct, aliceTok, st := createVote(t, s, nil)
	bobTok := join(t, s, slug, "Bob")
	aliceID := st["you"].(map[string]any)["participantId"].(string)
	bobID := participantID(t, getState(t, s, slug, bobTok))
	path := "/api/votes/" + slug + "/participants/"

	if rec, _ := doHdr(t, s, http.MethodDelete, path+bobID, nil, "", ct); rec.Code != http.StatusUnauthorized {
		t.Fatalf("no session: %d, want 401", rec.Code)
	}
	if rec, _ := doHdr(t, s, http.MethodDelete, path+aliceID, nil, bobTok, ct); rec.Code != http.StatusForbidden {
		t.Fatalf("non-creator session with creator token: %d, want 403", rec.Code)
	}
	if rec, _ := doHdr(t, s, http.MethodDelete, path+bobID, nil, aliceTok, "wrong"); rec.Code != http.StatusForbidden {
		t.Fatalf("wrong creator token: %d, want 403", rec.Code)
	}
	if rec, _ := doHdr(t, s, http.MethodDelete, path+aliceID, nil, aliceTok, ct); rec.Code != http.StatusBadRequest {
		t.Fatalf("removing creator: %d, want 400", rec.Code)
	}
	if rec, _ := doHdr(t, s, http.MethodDelete, path+"nope", nil, aliceTok, ct); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown participant: %d, want 404", rec.Code)
	}
	if rec, _ := doHdr(t, s, http.MethodDelete, "/api/votes/nosuchvote/participants/"+bobID, nil, aliceTok, ct); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown vote: %d, want 404", rec.Code)
	}
	// Bob was never removed by any of the failed attempts above.
	if got := getState(t, s, slug, bobTok); got["you"] == nil {
		t.Fatal("failed removal attempts must not remove anyone")
	}
}

func TestRemoveParticipantFromAnotherVoteIs404(t *testing.T) {
	s := newTestServer(t)
	slug1, ct1, aliceTok, _ := createVote(t, s, nil)
	slug2, _, _, _ := createVote(t, s, nil)
	carolTok := join(t, s, slug2, "Carol")
	carolID := participantID(t, getState(t, s, slug2, carolTok))

	if rec, _ := doHdr(t, s, http.MethodDelete, "/api/votes/"+slug1+"/participants/"+carolID, nil, aliceTok, ct1); rec.Code != http.StatusNotFound {
		t.Fatalf("participant of another vote: %d, want 404", rec.Code)
	}
	if got := getState(t, s, slug2, carolTok); got["you"] == nil {
		t.Fatal("a creator must not be able to remove someone from another vote")
	}
}

func TestAdvanceRejectsNonCreatorSessionWithCreatorToken(t *testing.T) {
	s := newTestServer(t)
	slug, ct, aliceTok, _ := createVote(t, s, nil)
	bobTok := join(t, s, slug, "Bob")
	suggest(t, s, slug, aliceTok, "A")
	suggest(t, s, slug, aliceTok, "B")
	if rec, _ := doHdr(t, s, http.MethodPost, "/api/votes/"+slug+"/advance", map[string]any{}, bobTok, ct); rec.Code != http.StatusForbidden {
		t.Fatalf("non-creator session advancing: %d, want 403", rec.Code)
	}
}

func TestRemoveLastHoldoutTriggersAllVotedAdvance(t *testing.T) {
	s := newTestServer(t)
	slug, ct, aliceTok, _ := createVote(t, s, map[string]any{"suggestAdvanceMode": "manual", "voteAdvanceMode": "all-voted"})
	bobTok := join(t, s, slug, "Bob")
	suggest(t, s, slug, aliceTok, "A")
	suggest(t, s, slug, aliceTok, "B")
	doHdr(t, s, http.MethodPost, "/api/votes/"+slug+"/advance", map[string]any{}, aliceTok, ct)
	ids := optionTitleToID(t, getState(t, s, slug, aliceTok))
	putBallot(t, s, slug, aliceTok, map[string]int{ids["A"]: 1})
	bobID := participantID(t, getState(t, s, slug, bobTok))

	_, st := doHdr(t, s, http.MethodDelete, "/api/votes/"+slug+"/participants/"+bobID, nil, aliceTok, ct)
	if st["phase"] != "results" {
		t.Fatalf("phase = %v, want results once the only non-voter is removed", st["phase"])
	}
}

func TestRemoveLastNotDoneTriggersAllDoneAdvance(t *testing.T) {
	s := newTestServer(t)
	slug, ct, aliceTok, _ := createVote(t, s, map[string]any{"suggestAdvanceMode": "all-done"})
	bobTok := join(t, s, slug, "Bob")
	suggest(t, s, slug, aliceTok, "A")
	suggest(t, s, slug, aliceTok, "B")
	if rec, _ := doJSON(t, s, http.MethodPost, "/api/votes/"+slug+"/done-suggesting", map[string]any{"done": true}, aliceTok); rec.Code != http.StatusOK {
		t.Fatalf("done-suggesting: %d %s", rec.Code, rec.Body.String())
	}
	bobID := participantID(t, getState(t, s, slug, bobTok))

	_, st := doHdr(t, s, http.MethodDelete, "/api/votes/"+slug+"/participants/"+bobID, nil, aliceTok, ct)
	if st["phase"] != "voting" {
		t.Fatalf("phase = %v, want voting once the only not-done participant is removed", st["phase"])
	}
}

func TestRemovedParticipantWSDowngradesToSpectator(t *testing.T) {
	s, ts := newWSTestServer(t)
	slug, ct, aliceTok, _ := createVote(t, s, nil)
	bobTok := join(t, s, slug, "Bob")
	bobConn := dialWS(t, ts, slug, bobTok)
	if snap := readSnapshot(t, bobConn); snap["you"] == nil {
		t.Fatal("Bob's initial snapshot must be personalized")
	}
	bobID := participantID(t, getState(t, s, slug, bobTok))

	if rec, _ := doHdr(t, s, http.MethodDelete, "/api/votes/"+slug+"/participants/"+bobID, nil, aliceTok, ct); rec.Code != http.StatusOK {
		t.Fatalf("remove: %d %s", rec.Code, rec.Body.String())
	}
	if snap := readSnapshot(t, bobConn); snap["you"] != nil {
		t.Fatalf("removed participant's live connection still personalized: %v", snap["you"])
	}
}

func TestCreatorDeletesAnySuggestion(t *testing.T) {
	s := newTestServer(t)
	slug, ct, aliceTok, _ := createVote(t, s, nil)
	bobTok := join(t, s, slug, "Bob")
	_, st := suggest(t, s, slug, bobTok, "spam")
	id := optionTitleToID(t, st)["spam"]
	path := "/api/votes/" + slug + "/suggestions/" + id

	if rec, _ := doHdr(t, s, http.MethodDelete, path, nil, aliceTok, ""); rec.Code != http.StatusNotFound {
		t.Fatalf("creator without creator token deletes others': %d, want 404 (owner-only path)", rec.Code)
	}
	if rec, _ := doHdr(t, s, http.MethodDelete, path, nil, aliceTok, "wrong"); rec.Code != http.StatusForbidden {
		t.Fatalf("wrong creator token: %d, want 403", rec.Code)
	}
	// A non-creator session can't borrow the real creator token.
	if rec, _ := doHdr(t, s, http.MethodDelete, path, nil, bobTok, ct); rec.Code != http.StatusForbidden {
		t.Fatalf("non-creator with creator token: %d, want 403", rec.Code)
	}
	rec, st := doHdr(t, s, http.MethodDelete, path, nil, aliceTok, ct)
	if rec.Code != http.StatusOK || len(st["options"].([]any)) != 0 {
		t.Fatalf("creator delete: %d options=%v", rec.Code, st["options"])
	}
	if rec, _ := doHdr(t, s, http.MethodDelete, path, nil, aliceTok, ct); rec.Code != http.StatusNotFound {
		t.Fatalf("creator delete of missing suggestion: %d, want 404", rec.Code)
	}
}

func TestCreatorDeleteSuggestionOnlyWhileSuggesting(t *testing.T) {
	s := newTestServer(t)
	slug, ct, aliceTok, _ := createVote(t, s, map[string]any{"suggestAdvanceMode": "manual"})
	bobTok := join(t, s, slug, "Bob")
	suggest(t, s, slug, aliceTok, "other")
	_, st := suggest(t, s, slug, bobTok, "keep")
	id := optionTitleToID(t, st)["keep"]
	if rec, _ := doHdr(t, s, http.MethodPost, "/api/votes/"+slug+"/advance", map[string]any{}, aliceTok, ct); rec.Code != http.StatusOK {
		t.Fatalf("advance: %d %s", rec.Code, rec.Body.String())
	}
	rec, _ := doHdr(t, s, http.MethodDelete, "/api/votes/"+slug+"/suggestions/"+id, nil, aliceTok, ct)
	if rec.Code != http.StatusConflict {
		t.Fatalf("creator delete during voting: %d, want 409", rec.Code)
	}
}

func TestCloseBlocksWritesAndReopenRestores(t *testing.T) {
	s := newTestServer(t)
	slug, ct, aliceTok, _ := createVote(t, s, nil)
	bobTok := join(t, s, slug, "Bob")
	base := "/api/votes/" + slug

	rec, st := doHdr(t, s, http.MethodPost, base+"/close", nil, aliceTok, ct)
	if rec.Code != http.StatusOK || st["closed"] != true {
		t.Fatalf("close: %d closed=%v", rec.Code, st["closed"])
	}
	if rec, _ := doHdr(t, s, http.MethodPost, base+"/close", nil, aliceTok, ct); rec.Code != http.StatusOK {
		t.Fatalf("close is idempotent: %d", rec.Code)
	}
	blocked := []struct {
		method, path string
		body         any
		tok, ct      string
	}{
		{http.MethodPost, base + "/join", map[string]any{"name": "Carol"}, "", ""},
		{http.MethodPost, base + "/suggestions", map[string]any{"title": "x"}, bobTok, ""},
		{http.MethodPost, base + "/done-suggesting", map[string]any{}, bobTok, ""},
		{http.MethodPost, base + "/advance", map[string]any{}, aliceTok, ct},
	}
	for _, b := range blocked {
		if rec, _ := doHdr(t, s, b.method, b.path, b.body, b.tok, b.ct); rec.Code != http.StatusConflict {
			t.Errorf("%s %s while closed: %d, want 409", b.method, b.path, rec.Code)
		}
	}
	if rec, _ := doJSON(t, s, http.MethodGet, base, nil, ""); rec.Code != http.StatusOK {
		t.Fatalf("GET while closed: %d", rec.Code)
	}
	if rec, _ := doHdr(t, s, http.MethodPost, base+"/close", nil, bobTok, ct); rec.Code != http.StatusForbidden {
		t.Fatalf("non-creator close: %d, want 403", rec.Code)
	}
	if rec, _ := doHdr(t, s, http.MethodPost, base+"/reopen", nil, bobTok, ct); rec.Code != http.StatusForbidden {
		t.Fatalf("non-creator reopen: %d, want 403", rec.Code)
	}

	rec, st = doHdr(t, s, http.MethodPost, base+"/reopen", nil, aliceTok, ct)
	if rec.Code != http.StatusOK || st["closed"] != false {
		t.Fatalf("reopen: %d closed=%v", rec.Code, st["closed"])
	}
	if rec, _ := doHdr(t, s, http.MethodPost, base+"/reopen", nil, aliceTok, ct); rec.Code != http.StatusOK {
		t.Fatalf("reopen of an open vote is a no-op 200: %d", rec.Code)
	}
	if rec, _ := suggest(t, s, slug, bobTok, "after reopen"); rec.Code != http.StatusOK {
		t.Fatalf("suggest after reopen: %d", rec.Code)
	}
}

func TestClosedVoteTimerDoesNotAdvance(t *testing.T) {
	s, fc := newFakeClockServer(t)
	slug, ct, aliceTok, _ := createVote(t, s, map[string]any{"suggestTimerSecs": 60})
	suggest(t, s, slug, aliceTok, "A")
	suggest(t, s, slug, aliceTok, "B")
	doHdr(t, s, http.MethodPost, "/api/votes/"+slug+"/close", nil, aliceTok, ct)

	fc.Advance(2 * time.Minute)
	st := getState(t, s, slug, aliceTok)
	if st["phase"] != "suggesting" || st["phaseDeadline"] != nil {
		t.Fatalf("closed vote changed on timer: phase=%v deadline=%v", st["phase"], st["phaseDeadline"])
	}
	doHdr(t, s, http.MethodPost, "/api/votes/"+slug+"/reopen", nil, aliceTok, ct)
	fc.Advance(2 * time.Minute)
	if st := getState(t, s, slug, aliceTok); st["phase"] != "suggesting" {
		t.Fatalf("reopen must not re-arm the timer: phase=%v", st["phase"])
	}
}

func TestCloseBlocksVotingAndResultsWrites(t *testing.T) {
	s := newTestServer(t)
	slug, ct, aliceTok, _ := createVote(t, s, map[string]any{"suggestAdvanceMode": "manual", "voteAdvanceMode": "manual"})
	bobTok := join(t, s, slug, "Bob")
	_, st := suggest(t, s, slug, aliceTok, "A")
	suggest(t, s, slug, aliceTok, "B")
	optA := optionTitleToID(t, st)["A"]
	base := "/api/votes/" + slug
	bobID := participantID(t, getState(t, s, slug, bobTok))

	// Suggest phase: deleting a suggestion is blocked while closed.
	doHdr(t, s, http.MethodPost, base+"/close", nil, aliceTok, ct)
	if rec, _ := doHdr(t, s, http.MethodDelete, base+"/suggestions/"+optA, nil, aliceTok, ""); rec.Code != http.StatusConflict {
		t.Fatalf("delete suggestion while closed: %d, want 409", rec.Code)
	}
	if rec, _ := doHdr(t, s, http.MethodDelete, base+"/participants/"+bobID, nil, aliceTok, ct); rec.Code != http.StatusConflict {
		t.Fatalf("remove participant while closed: %d, want 409", rec.Code)
	}
	doHdr(t, s, http.MethodPost, base+"/reopen", nil, aliceTok, ct)

	// Voting phase: ballots blocked.
	doHdr(t, s, http.MethodPost, base+"/advance", map[string]any{}, aliceTok, ct)
	doHdr(t, s, http.MethodPost, base+"/close", nil, aliceTok, ct)
	if rec, _ := putBallot(t, s, slug, bobTok, map[string]int{optA: 1}); rec.Code != http.StatusConflict {
		t.Fatalf("ballot while closed: %d, want 409", rec.Code)
	}
	doHdr(t, s, http.MethodPost, base+"/reopen", nil, aliceTok, ct)

	// Results phase: re-vote calls blocked.
	doHdr(t, s, http.MethodPost, base+"/advance", map[string]any{}, aliceTok, ct)
	if st := getState(t, s, slug, aliceTok); st["phase"] != "results" {
		t.Fatalf("setup: phase=%v, want results", st["phase"])
	}
	doHdr(t, s, http.MethodPost, base+"/close", nil, aliceTok, ct)
	if rec, _ := doHdr(t, s, http.MethodPost, base+"/revote", map[string]any{}, bobTok, ""); rec.Code != http.StatusConflict {
		t.Fatalf("revote while closed: %d, want 409", rec.Code)
	}
}

func TestNextVoteCarriesGroupWithFreshTokens(t *testing.T) {
	s := newTestServer(t)
	slug, ct, aliceTok, _ := createVote(t, s, map[string]any{"creditsPerOption": 7})
	bobTok := join(t, s, slug, "Bob")

	rec, out := doHdr(t, s, http.MethodPost, "/api/votes/"+slug+"/next", map[string]any{"title": "Round two"}, aliceTok, ct)
	if rec.Code != http.StatusCreated {
		t.Fatalf("next: %d %s", rec.Code, rec.Body.String())
	}
	next := out["slug"].(string)
	newState := out["state"].(map[string]any)
	if newState["title"] != "Round two" || newState["settings"].(map[string]any)["creditsPerOption"] != float64(7) {
		t.Fatalf("next vote title/settings not carried: %v", newState)
	}
	if n := len(newState["participants"].([]any)); n != 2 {
		t.Fatalf("carried participants = %d, want 2", n)
	}
	// The caller's own response carries their new creator credentials.
	if out["creatorToken"] == "" || out["creatorToken"] == ct || out["sessionToken"] == "" || out["sessionToken"] == aliceTok {
		t.Fatalf("caller needs fresh creator/session tokens: %v", out)
	}
	if you := newState["you"].(map[string]any); you["isCreator"] != true {
		t.Fatalf("caller must be creator of the next vote: %v", you)
	}

	bobOld := getState(t, s, slug, bobTok)
	if bobOld["next"].(map[string]any)["slug"] != next || bobOld["next"].(map[string]any)["title"] != "Round two" {
		t.Fatalf("old vote next = %v", bobOld["next"])
	}
	bobYou := bobOld["you"].(map[string]any)
	bobNewTok, _ := bobYou["nextSessionToken"].(string)
	if bobNewTok == "" || bobNewTok == bobTok {
		t.Fatalf("Bob needs a fresh token, got %q", bobNewTok)
	}
	if _, ok := bobYou["nextCreatorToken"]; ok {
		t.Fatal("non-creator must not receive the next creator token")
	}
	if got := getState(t, s, next, bobNewTok); got["you"] == nil {
		t.Fatal("Bob's next token doesn't authenticate in the new vote")
	}
	if got := getState(t, s, next, bobTok); got["you"] != nil {
		t.Fatal("old token must not authenticate in the new vote")
	}
	_, aliceOld := doHdr(t, s, http.MethodGet, "/api/votes/"+slug, nil, aliceTok, ct)
	aliceYou := aliceOld["you"].(map[string]any)
	if aliceYou["nextCreatorToken"] != out["creatorToken"] || aliceYou["nextSessionToken"] != out["sessionToken"] {
		t.Fatalf("creator's old-vote handoff = %v, want response tokens", aliceYou)
	}
	// Spectators see the link but no tokens.
	spec := getState(t, s, slug, "")
	if spec["next"] == nil || spec["you"] != nil {
		t.Fatalf("spectator view: next=%v you=%v", spec["next"], spec["you"])
	}
	if rec, _ := doHdr(t, s, http.MethodPost, "/api/votes/"+slug+"/next", map[string]any{"title": "again"}, aliceTok, ct); rec.Code != http.StatusConflict {
		t.Fatalf("second successor: %d, want 409", rec.Code)
	}
	// The new vote itself has no successor yet.
	if got := getState(t, s, next, bobNewTok); got["next"] != nil {
		t.Fatalf("new vote next = %v, want null", got["next"])
	}
}

func TestNextVoteSkipsRemovedParticipants(t *testing.T) {
	s := newTestServer(t)
	slug, ct, aliceTok, _ := createVote(t, s, nil)
	bobTok := join(t, s, slug, "Bob")
	join(t, s, slug, "Carol")
	bobID := participantID(t, getState(t, s, slug, bobTok))
	doHdr(t, s, http.MethodDelete, "/api/votes/"+slug+"/participants/"+bobID, nil, aliceTok, ct)

	_, out := doHdr(t, s, http.MethodPost, "/api/votes/"+slug+"/next", map[string]any{"title": "R2"}, aliceTok, ct)
	names := []string{}
	for _, p := range out["state"].(map[string]any)["participants"].([]any) {
		names = append(names, p.(map[string]any)["name"].(string))
	}
	if len(names) != 2 || names[0] != "Alice" || names[1] != "Carol" {
		t.Fatalf("carried %v, want [Alice Carol]", names)
	}
	// The removed participant's old token gets neither the handoff nor a
	// seat in the new vote.
	if got := getState(t, s, slug, bobTok); got["you"] != nil {
		t.Fatalf("removed participant still authenticates: %v", got["you"])
	}
}

func TestNextVoteSettingsPatchAndAuth(t *testing.T) {
	s := newTestServer(t)
	slug, ct, aliceTok, _ := createVote(t, s, map[string]any{"creditsPerOption": 7, "maxSuggestionsPerUser": 2})
	bobTok := join(t, s, slug, "Bob")
	path := "/api/votes/" + slug + "/next"

	if rec, _ := doHdr(t, s, http.MethodPost, path, map[string]any{"title": "x"}, "", ct); rec.Code != http.StatusUnauthorized {
		t.Fatalf("no session: %d, want 401", rec.Code)
	}
	if rec, _ := doHdr(t, s, http.MethodPost, path, map[string]any{"title": "x"}, bobTok, ct); rec.Code != http.StatusForbidden {
		t.Fatalf("non-creator: %d, want 403", rec.Code)
	}
	if rec, _ := doHdr(t, s, http.MethodPost, path, map[string]any{"title": "x"}, aliceTok, "wrong"); rec.Code != http.StatusForbidden {
		t.Fatalf("wrong creator token: %d, want 403", rec.Code)
	}

	rec, out := doHdr(t, s, http.MethodPost, path, map[string]any{"title": "  R2  ", "settings": map[string]any{"creditsPerOption": 3}}, aliceTok, ct)
	if rec.Code != http.StatusCreated {
		t.Fatalf("next: %d %s", rec.Code, rec.Body.String())
	}
	st := out["state"].(map[string]any)
	settings := st["settings"].(map[string]any)
	if st["title"] != "R2" || settings["creditsPerOption"] != float64(3) || settings["maxSuggestionsPerUser"] != float64(2) {
		t.Fatalf("patch over source settings: title=%v settings=%v", st["title"], settings)
	}
	if st["phase"] != "suggesting" {
		t.Fatalf("next vote phase = %v, want suggesting", st["phase"])
	}
}

func TestNextVoteValidationAndClosed(t *testing.T) {
	s := newTestServer(t)
	slug, ct, aliceTok, _ := createVote(t, s, nil)
	path := "/api/votes/" + slug + "/next"
	if rec, _ := doHdr(t, s, http.MethodPost, path, map[string]any{"title": "  "}, aliceTok, ct); rec.Code != http.StatusBadRequest {
		t.Fatalf("blank title: %d, want 400", rec.Code)
	}
	if rec, _ := doHdr(t, s, http.MethodPost, path, map[string]any{"title": "x", "settings": map[string]any{"creditsPerOption": 0}}, aliceTok, ct); rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid settings: %d, want 400", rec.Code)
	}
	doHdr(t, s, http.MethodPost, "/api/votes/"+slug+"/close", nil, aliceTok, ct)
	if rec, _ := doHdr(t, s, http.MethodPost, path, map[string]any{"title": "x"}, aliceTok, ct); rec.Code != http.StatusConflict {
		t.Fatalf("next on closed vote: %d, want 409", rec.Code)
	}
}

// TestNextVoteAfterSuccessorPruned: when a follow-up vote is pruned while its
// source is still in use, the source drops the link, so the group sees no
// dead "next" and the creator can start another follow-up.
func TestNextVoteAfterSuccessorPruned(t *testing.T) {
	s, fc := newFakeClockServer(t)
	slug, ct, aliceTok, _ := createVote(t, s, nil)
	bobTok := join(t, s, slug, "Bob")
	rec, out := doHdr(t, s, http.MethodPost, "/api/votes/"+slug+"/next", map[string]any{"title": "R2"}, aliceTok, ct)
	if rec.Code != http.StatusCreated {
		t.Fatalf("next: %d %s", rec.Code, rec.Body.String())
	}
	first := out["slug"].(string)

	fc.Advance(100 * 24 * time.Hour)
	suggest(t, s, slug, aliceTok, "still here") // activity on the source only
	if n, err := s.PruneExpired(90 * 24 * time.Hour); err != nil || n != 1 {
		t.Fatalf("pruned n=%d err=%v, want 1 (the successor)", n, err)
	}
	if rec, _ := doJSON(t, s, http.MethodGet, "/api/votes/"+first, nil, ""); rec.Code != http.StatusNotFound {
		t.Fatalf("successor survived prune: %d", rec.Code)
	}
	st := getState(t, s, slug, bobTok)
	if st["next"] != nil {
		t.Fatalf("source still advertises a pruned successor: %v", st["next"])
	}
	if _, ok := st["you"].(map[string]any)["nextSessionToken"]; ok {
		t.Fatal("handoff token for a pruned successor must not be sent")
	}

	rec, out = doHdr(t, s, http.MethodPost, "/api/votes/"+slug+"/next", map[string]any{"title": "R3"}, aliceTok, ct)
	if rec.Code != http.StatusCreated {
		t.Fatalf("next after successor pruned: %d %s", rec.Code, rec.Body.String())
	}
	second := out["slug"].(string)
	st = getState(t, s, slug, bobTok)
	if next, _ := st["next"].(map[string]any); next == nil || next["slug"] != second || next["title"] != "R3" {
		t.Fatalf("source next = %v, want %s/R3", st["next"], second)
	}
	bobNew, _ := st["you"].(map[string]any)["nextSessionToken"].(string)
	if got := getState(t, s, second, bobNew); got["you"] == nil {
		t.Fatal("Bob's new handoff token doesn't authenticate in the new successor")
	}
}

// TestNextVoteOverDanglingLink covers a source whose next_slug points at a
// row that is gone without having been unlinked (a database pruned by an
// earlier build): the room must show no successor or handoff tokens (the
// hasNext guard in BuildRoomState), and starting a new follow-up must work.
func TestNextVoteOverDanglingLink(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	st, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	s := server.NewWithConfig(st, nil, server.Config{Clock: clock.NewFake(time.Unix(1_700_000_000, 0))})

	slug, ct, aliceTok, _ := createVote(t, s, nil)
	bobTok := join(t, s, slug, "Bob")
	_, out := doHdr(t, s, http.MethodPost, "/api/votes/"+slug+"/next", map[string]any{"title": "R2"}, aliceTok, ct)
	first := out["slug"].(string)

	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	for _, q := range []string{`DELETE FROM participants WHERE vote_slug = ?`, `DELETE FROM votes WHERE slug = ?`} {
		if _, err := raw.Exec(q, first); err != nil {
			t.Fatal(err)
		}
	}

	for _, who := range []struct{ tok, ct string }{{bobTok, ""}, {aliceTok, ct}} {
		_, got := doHdr(t, s, http.MethodGet, "/api/votes/"+slug, nil, who.tok, who.ct)
		you := got["you"].(map[string]any)
		_, hasSession := you["nextSessionToken"]
		_, hasCreator := you["nextCreatorToken"]
		if got["next"] != nil || hasSession || hasCreator {
			t.Fatalf("dangling successor leaked: next=%v you=%v", got["next"], you)
		}
	}

	rec, out := doHdr(t, s, http.MethodPost, "/api/votes/"+slug+"/next", map[string]any{"title": "R3"}, aliceTok, ct)
	if rec.Code != http.StatusCreated {
		t.Fatalf("next over a dangling link: %d %s", rec.Code, rec.Body.String())
	}
	if next, _ := getState(t, s, slug, bobTok)["next"].(map[string]any); next == nil || next["slug"] != out["slug"] {
		t.Fatalf("source next = %v, want %v", next, out["slug"])
	}
}

// startNext creates a vote (Alice creator, Bob) and a follow-up of it,
// returning the source slug, its creator and session tokens, and the
// successor's creator token.
func startNext(t *testing.T, s *server.Server) (slug, ct, aliceTok, nextCT string) {
	t.Helper()
	slug, ct, aliceTok, _ = createVote(t, s, nil)
	join(t, s, slug, "Bob")
	rec, out := doHdr(t, s, http.MethodPost, "/api/votes/"+slug+"/next", map[string]any{"title": "R2"}, aliceTok, ct)
	if rec.Code != http.StatusCreated {
		t.Fatalf("next: %d %s", rec.Code, rec.Body.String())
	}
	return slug, ct, aliceTok, out["creatorToken"].(string)
}

// TestNextCreatorTokenNeedsCreatorToken: the successor's creator token is a
// creator power, so like every other creator power it needs the creator
// token as well as the creator's session; a leaked session alone must not
// hand it out. The session-only handoff token is unaffected.
func TestNextCreatorTokenNeedsCreatorToken(t *testing.T) {
	s := newTestServer(t)
	slug, ct, aliceTok, nextCT := startNext(t, s)
	get := func(creatorTok string) map[string]any {
		t.Helper()
		rec, st := doHdr(t, s, http.MethodGet, "/api/votes/"+slug, nil, aliceTok, creatorTok)
		if rec.Code != http.StatusOK {
			t.Fatalf("get: %d", rec.Code)
		}
		return st["you"].(map[string]any)
	}

	for _, bad := range []string{"", "wrong"} {
		you := get(bad)
		if _, ok := you["nextCreatorToken"]; ok {
			t.Fatalf("X-Creator-Token %q: nextCreatorToken sent on the session alone", bad)
		}
		if you["nextSessionToken"] == nil {
			t.Fatalf("X-Creator-Token %q: nextSessionToken must stay session-only", bad)
		}
	}
	if you := get(ct); you["nextCreatorToken"] != nextCT {
		t.Fatalf("with creator token: nextCreatorToken = %v, want %s", you["nextCreatorToken"], nextCT)
	}

	// Mutation responses (writeState) follow the same rule.
	done := "/api/votes/" + slug + "/done-suggesting"
	if _, st := doHdr(t, s, http.MethodPost, done, map[string]any{}, aliceTok, ""); st["you"].(map[string]any)["nextCreatorToken"] != nil {
		t.Fatal("mutation response without creator token carried nextCreatorToken")
	}
	if _, st := doHdr(t, s, http.MethodPost, done, map[string]any{}, aliceTok, ct); st["you"].(map[string]any)["nextCreatorToken"] != nextCT {
		t.Fatalf("mutation response with creator token: you = %v", st["you"])
	}
}

func TestWebSocketNextCreatorTokenNeedsCreatorToken(t *testing.T) {
	s, ts := newWSTestServer(t)
	slug, ct, aliceTok, nextCT := startNext(t, s)

	for _, c := range []struct {
		creatorTok string
		want       any
	}{{"", nil}, {"wrong", nil}, {ct, nextCT}} {
		conn := dialRaw(t, ts, slug)
		if err := conn.WriteJSON(map[string]string{"type": "auth", "token": aliceTok, "creatorToken": c.creatorTok}); err != nil {
			t.Fatal(err)
		}
		you := readSnapshot(t, conn)["you"].(map[string]any)
		if you["nextCreatorToken"] != c.want {
			t.Fatalf("creatorToken %q: nextCreatorToken = %v, want %v", c.creatorTok, you["nextCreatorToken"], c.want)
		}
		if you["nextSessionToken"] == nil {
			t.Fatalf("creatorToken %q: nextSessionToken missing", c.creatorTok)
		}
		// Broadcasts keep the connection's verified status.
		s.Broadcast(slug)
		if you := readSnapshot(t, conn)["you"].(map[string]any); you["nextCreatorToken"] != c.want {
			t.Fatalf("creatorToken %q on broadcast: nextCreatorToken = %v", c.creatorTok, you["nextCreatorToken"])
		}
	}
}

// resultsWithThreeVoters drives a manual vote (Alice creator, Bob, Carol;
// re-vote threshold 50%) to the results phase with Alice's ballot cast.
func resultsWithThreeVoters(t *testing.T, s *server.Server) (slug, ct, aliceTok, bobTok, carolTok string) {
	t.Helper()
	slug, ct, aliceTok, _ = createVote(t, s, map[string]any{
		"suggestAdvanceMode": "manual", "voteAdvanceMode": "manual", "revoteThresholdPct": 50,
	})
	bobTok = join(t, s, slug, "Bob")
	carolTok = join(t, s, slug, "Carol")
	suggest(t, s, slug, aliceTok, "A")
	suggest(t, s, slug, aliceTok, "B")
	adv := "/api/votes/" + slug + "/advance"
	doHdr(t, s, http.MethodPost, adv, map[string]any{}, aliceTok, ct)
	ids := optionTitleToID(t, getState(t, s, slug, aliceTok))
	if rec, _ := putBallot(t, s, slug, aliceTok, map[string]int{ids["A"]: 1}); rec.Code != http.StatusOK {
		t.Fatalf("ballot: %d %s", rec.Code, rec.Body.String())
	}
	if rec, st := doHdr(t, s, http.MethodPost, adv, map[string]any{}, aliceTok, ct); rec.Code != http.StatusOK || st["phase"] != "results" {
		t.Fatalf("advance to results: %d phase=%v", rec.Code, st["phase"])
	}
	return slug, ct, aliceTok, bobTok, carolTok
}

// TestRemoveHoldoutTriggersRevote: removing a participant in the results
// phase shrinks the re-vote threshold, so the removed person may have been
// the holdout. The same threshold check as a re-vote call must run.
func TestRemoveHoldoutTriggersRevote(t *testing.T) {
	s := newTestServer(t)
	slug, ct, aliceTok, _, carolTok := resultsWithThreeVoters(t, s)
	// 1 call of the 2 needed (50% of 3, rounded up).
	if rec, st := doHdr(t, s, http.MethodPost, "/api/votes/"+slug+"/revote", map[string]any{}, aliceTok, ""); rec.Code != http.StatusOK || st["phase"] != "results" {
		t.Fatalf("revote call: %d phase=%v", rec.Code, st["phase"])
	}
	carolID := participantID(t, getState(t, s, slug, carolTok))

	rec, st := doHdr(t, s, http.MethodDelete, "/api/votes/"+slug+"/participants/"+carolID, nil, aliceTok, ct)
	if rec.Code != http.StatusOK {
		t.Fatalf("remove: %d %s", rec.Code, rec.Body.String())
	}
	if st["phase"] != "voting" || st["results"] != nil {
		t.Fatalf("phase=%v results=%v, want a fresh voting round (1 of 1 needed)", st["phase"], st["results"])
	}
	if b := st["you"].(map[string]any)["ballot"]; b != nil {
		t.Fatalf("ballots must be cleared by the re-vote, Alice has %v", b)
	}
	for _, p := range st["participants"].([]any) {
		if p.(map[string]any)["wantsRevote"] != false {
			t.Fatalf("re-vote calls must reset: %v", p)
		}
	}
}

// TestRemoveRevoteCallerDoesNotTriggerRevote: the removed participant's own
// call no longer counts, so removing a caller must not start a re-vote.
func TestRemoveRevoteCallerDoesNotTriggerRevote(t *testing.T) {
	s := newTestServer(t)
	slug, ct, aliceTok, bobTok, _ := resultsWithThreeVoters(t, s)
	doHdr(t, s, http.MethodPost, "/api/votes/"+slug+"/revote", map[string]any{}, bobTok, "")
	bobID := participantID(t, getState(t, s, slug, bobTok))

	_, st := doHdr(t, s, http.MethodDelete, "/api/votes/"+slug+"/participants/"+bobID, nil, aliceTok, ct)
	if st["phase"] != "results" {
		t.Fatalf("phase=%v, want results: the only caller was removed", st["phase"])
	}
}

// TestCloseReopenAuthMatrix: close and reopen need the creator's session and
// the creator token, and a rejected attempt changes nothing.
func TestCloseReopenAuthMatrix(t *testing.T) {
	s := newTestServer(t)
	slug, ct, aliceTok, _ := createVote(t, s, nil)
	bobTok := join(t, s, slug, "Bob")
	base := "/api/votes/" + slug
	rejected := []struct {
		name, tok, ct string
		want          int
	}{
		{"no session", "", ct, http.StatusUnauthorized},
		{"creator session, no creator token", aliceTok, "", http.StatusForbidden},
		{"creator session, wrong creator token", aliceTok, "wrong", http.StatusForbidden},
		{"non-creator session with the creator token", bobTok, ct, http.StatusForbidden},
	}
	for _, action := range []struct {
		path   string
		closed bool // state before (and after) every rejected attempt
	}{{"/close", false}, {"/reopen", true}} {
		if action.closed {
			if rec, _ := doHdr(t, s, http.MethodPost, base+"/close", nil, aliceTok, ct); rec.Code != http.StatusOK {
				t.Fatalf("setup close: %d", rec.Code)
			}
		}
		for _, c := range rejected {
			if rec, _ := doHdr(t, s, http.MethodPost, base+action.path, nil, c.tok, c.ct); rec.Code != c.want {
				t.Errorf("%s by %s: %d, want %d", action.path, c.name, rec.Code, c.want)
			}
			if got := getState(t, s, slug, "")["closed"]; got != action.closed {
				t.Fatalf("%s by %s changed closed to %v", action.path, c.name, got)
			}
		}
	}
}

// TestNextVoteCountsAgainstCreateLimit: a follow-up creates a vote, so it
// shares the per-IP creation throttle with POST /api/votes.
func TestNextVoteCountsAgainstCreateLimit(t *testing.T) {
	s := newTestServer(t)
	slug, ct, aliceTok, _ := createVote(t, s, nil)
	for i := 1; i < 10; i++ { // the create limit is a burst of 10
		createVote(t, s, nil)
	}
	rec, _ := doHdr(t, s, http.MethodPost, "/api/votes/"+slug+"/next", map[string]any{"title": "R2"}, aliceTok, ct)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("next after exhausting creates: %d, want 429", rec.Code)
	}
}
