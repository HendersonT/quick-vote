package server_test

import (
	"net/http"
	"testing"
	"time"
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
