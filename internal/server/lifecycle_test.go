package server_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/HendersonT/quick-vote/internal/server"
)

// doHdr issues a request like doJSON but also sets an optional
// X-Creator-Token header, used to exercise creator-only routes.
func doHdr(t *testing.T, s *server.Server, method, path string, body any, token, creatorToken string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()

	var reader *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		reader = bytes.NewReader(b)
	} else {
		reader = bytes.NewReader(nil)
	}

	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if creatorToken != "" {
		req.Header.Set("X-Creator-Token", creatorToken)
	}

	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)

	var out map[string]any
	if rec.Body.Len() > 0 {
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode response %q: %v", rec.Body.String(), err)
		}
	}
	return rec, out
}

// --- small helpers over the JSON snapshot ---

func createVote(t *testing.T, s *server.Server, settings map[string]any) (slug, creatorToken, sessionToken string, state map[string]any) {
	t.Helper()
	body := map[string]any{"title": "Game night", "creatorName": "Alice"}
	if settings != nil {
		body["settings"] = settings
	}
	rec, out := doJSON(t, s, http.MethodPost, "/api/votes", body, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	return out["slug"].(string), out["creatorToken"].(string), out["sessionToken"].(string), out["state"].(map[string]any)
}

func join(t *testing.T, s *server.Server, slug, name string) string {
	t.Helper()
	rec, out := doJSON(t, s, http.MethodPost, "/api/votes/"+slug+"/join", map[string]any{"name": name}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("join %s: status = %d, body = %s", name, rec.Code, rec.Body.String())
	}
	return out["sessionToken"].(string)
}

func suggest(t *testing.T, s *server.Server, slug, token, title string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	return doJSON(t, s, http.MethodPost, "/api/votes/"+slug+"/suggestions", map[string]any{"title": title}, token)
}

func getState(t *testing.T, s *server.Server, slug, token string) map[string]any {
	t.Helper()
	rec, out := doJSON(t, s, http.MethodGet, "/api/votes/"+slug, nil, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("get state: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	return out
}

func optionTitleToID(t *testing.T, state map[string]any) map[string]string {
	t.Helper()
	m := map[string]string{}
	for _, raw := range state["options"].([]any) {
		o := raw.(map[string]any)
		m[o["title"].(string)] = o["id"].(string)
	}
	return m
}

func putBallot(t *testing.T, s *server.Server, slug, token string, votes map[string]int) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	return doJSON(t, s, http.MethodPut, "/api/votes/"+slug+"/ballot", map[string]any{"votes": votes}, token)
}

func TestFullLifecycle(t *testing.T) {
	s := newTestServer(t)

	slug, creatorTok, aliceTok, state := createVote(t, s, map[string]any{
		"maxSuggestionsPerUser": 2,
		"creditsPerOption":      3,
		"suggestAdvanceMode":    "manual",
		"voteAdvanceMode":       "all-voted",
		"survivalThreshold":     1,
		"tiebreaker":            "most-backers",
		"revoteThresholdPct":    34,
	})
	if state["phase"] != "suggesting" {
		t.Fatalf("phase = %v, want suggesting", state["phase"])
	}

	bobTok := join(t, s, slug, "Bob")
	carolTok := join(t, s, slug, "Carol")

	if rec, _ := suggest(t, s, slug, aliceTok, "Catan"); rec.Code != http.StatusOK {
		t.Fatalf("alice suggest: %d", rec.Code)
	}
	if rec, _ := suggest(t, s, slug, bobTok, "Wingspan"); rec.Code != http.StatusOK {
		t.Fatalf("bob suggest: %d", rec.Code)
	}
	if rec, _ := suggest(t, s, slug, carolTok, "Munchkin"); rec.Code != http.StatusOK {
		t.Fatalf("carol suggest: %d", rec.Code)
	}

	// manual advance to voting
	rec, adv := doHdr(t, s, http.MethodPost, "/api/votes/"+slug+"/advance", map[string]any{}, aliceTok, creatorTok)
	if rec.Code != http.StatusOK {
		t.Fatalf("advance: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if adv["phase"] != "voting" {
		t.Fatalf("phase = %v, want voting", adv["phase"])
	}
	if adv["budget"] != float64(9) {
		t.Fatalf("budget = %v, want 9", adv["budget"])
	}

	ids := optionTitleToID(t, adv)

	// Alice: Catan 2, Wingspan 1 -> cost 5
	if rec, _ := putBallot(t, s, slug, aliceTok, map[string]int{ids["Catan"]: 2, ids["Wingspan"]: 1}); rec.Code != http.StatusOK {
		t.Fatalf("alice ballot: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	// Bob: Wingspan 2 -> cost 4
	if rec, _ := putBallot(t, s, slug, bobTok, map[string]int{ids["Wingspan"]: 2}); rec.Code != http.StatusOK {
		t.Fatalf("bob ballot: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	// Carol: Catan 1, Wingspan 1, Munchkin 0 -> cost 2; 3rd ballot auto-advances
	rec, carolResp := putBallot(t, s, slug, carolTok, map[string]int{ids["Catan"]: 1, ids["Wingspan"]: 1, ids["Munchkin"]: 0})
	if rec.Code != http.StatusOK {
		t.Fatalf("carol ballot: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if carolResp["phase"] != "results" {
		t.Fatalf("phase = %v, want results after all voted", carolResp["phase"])
	}

	results := carolResp["results"].(map[string]any)
	scores := map[string][2]int{} // title -> {score, backers}
	idToTitle := map[string]string{}
	for title, id := range ids {
		idToTitle[id] = title
	}
	for _, raw := range results["scores"].([]any) {
		sc := raw.(map[string]any)
		title := idToTitle[sc["optionId"].(string)]
		scores[title] = [2]int{int(sc["score"].(float64)), int(sc["backers"].(float64))}
		if title == "Munchkin" && sc["eliminated"] != true {
			t.Fatalf("Munchkin should be eliminated: %v", sc)
		}
	}
	if scores["Wingspan"] != [2]int{4, 3} {
		t.Fatalf("Wingspan score/backers = %v, want {4 3}", scores["Wingspan"])
	}
	if scores["Catan"] != [2]int{3, 2} {
		t.Fatalf("Catan score/backers = %v, want {3 2}", scores["Catan"])
	}
	if scores["Munchkin"] != [2]int{0, 0} {
		t.Fatalf("Munchkin score/backers = %v, want {0 0}", scores["Munchkin"])
	}
	if results["winnerOptionId"] != ids["Wingspan"] {
		t.Fatalf("winner = %v, want Wingspan (%s)", results["winnerOptionId"], ids["Wingspan"])
	}
	if int(results["revoteNeeded"].(float64)) != 2 {
		t.Fatalf("revoteNeeded = %v, want 2", results["revoteNeeded"])
	}

	// Bob calls re-vote: 1 of 2, phase stays results.
	rec, bobRe := doJSON(t, s, http.MethodPost, "/api/votes/"+slug+"/revote", map[string]any{}, bobTok)
	if rec.Code != http.StatusOK {
		t.Fatalf("bob revote: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if bobRe["phase"] != "results" {
		t.Fatalf("phase = %v, want still results after 1 call", bobRe["phase"])
	}
	if int(bobRe["results"].(map[string]any)["revoteCalls"].(float64)) != 1 {
		t.Fatalf("revoteCalls = %v, want 1", bobRe["results"].(map[string]any)["revoteCalls"])
	}

	// Carol calls re-vote: 2 of 2 -> back to voting, ballots cleared.
	rec, carolRe := doJSON(t, s, http.MethodPost, "/api/votes/"+slug+"/revote", map[string]any{}, carolTok)
	if rec.Code != http.StatusOK {
		t.Fatalf("carol revote: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if carolRe["phase"] != "voting" {
		t.Fatalf("phase = %v, want voting after re-vote met", carolRe["phase"])
	}
	if carolRe["results"] != nil {
		t.Fatalf("results = %v, want nil after re-vote", carolRe["results"])
	}
	for _, raw := range carolRe["participants"].([]any) {
		p := raw.(map[string]any)
		if p["hasVoted"] != false {
			t.Fatalf("participant %v hasVoted should be false after re-vote", p["name"])
		}
		if p["wantsRevote"] != false {
			t.Fatalf("participant %v wantsRevote should be reset after re-vote", p["name"])
		}
	}
	if len(carolRe["options"].([]any)) != 3 {
		t.Fatalf("options count = %d, want 3 preserved across re-vote", len(carolRe["options"].([]any)))
	}
}

// setupVoting drives a fresh vote to the voting phase with two options
// ("A", "B") suggested by the creator, returning tokens and option ids.
func setupVoting(t *testing.T, s *server.Server, settings map[string]any) (slug, creatorTok, aliceTok string, ids map[string]string) {
	t.Helper()
	if settings == nil {
		settings = map[string]any{}
	}
	slug, creatorTok, aliceTok, _ = createVote(t, s, settings)
	if rec, _ := suggest(t, s, slug, aliceTok, "A"); rec.Code != http.StatusOK {
		t.Fatalf("suggest A: %d", rec.Code)
	}
	if rec, _ := suggest(t, s, slug, aliceTok, "B"); rec.Code != http.StatusOK {
		t.Fatalf("suggest B: %d", rec.Code)
	}
	rec, adv := doHdr(t, s, http.MethodPost, "/api/votes/"+slug+"/advance", map[string]any{}, aliceTok, creatorTok)
	if rec.Code != http.StatusOK {
		t.Fatalf("advance to voting: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	return slug, creatorTok, aliceTok, optionTitleToID(t, adv)
}

func TestBallotOverBudget(t *testing.T) {
	s := newTestServer(t)
	slug, _, aliceTok, ids := setupVoting(t, s, map[string]any{"creditsPerOption": 3})
	// budget = 3 * 2 = 6. {A:3} costs 9 -> over budget.
	rec, out := putBallot(t, s, slug, aliceTok, map[string]int{ids["A"]: 3})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body = %s", rec.Code, rec.Body.String())
	}
	if msg, _ := out["error"].(string); msg == "" {
		t.Fatalf("expected error message, got %v", out)
	}
}

func TestSuggestInVotingPhase(t *testing.T) {
	s := newTestServer(t)
	slug, _, aliceTok, _ := setupVoting(t, s, nil)
	rec, _ := suggest(t, s, slug, aliceTok, "Late")
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", rec.Code)
	}
}

func TestSuggestionCap(t *testing.T) {
	s := newTestServer(t)
	slug, _, aliceTok, _ := createVote(t, s, map[string]any{"maxSuggestionsPerUser": 2})
	if rec, _ := suggest(t, s, slug, aliceTok, "One"); rec.Code != http.StatusOK {
		t.Fatalf("first: %d", rec.Code)
	}
	if rec, _ := suggest(t, s, slug, aliceTok, "Two"); rec.Code != http.StatusOK {
		t.Fatalf("second: %d", rec.Code)
	}
	rec, _ := suggest(t, s, slug, aliceTok, "Three")
	if rec.Code != http.StatusConflict {
		t.Fatalf("third: status = %d, want 409", rec.Code)
	}
}

func TestSuggestionDuplicateTitle(t *testing.T) {
	s := newTestServer(t)
	slug, _, aliceTok, _ := createVote(t, s, nil)
	bobTok := join(t, s, slug, "Bob")
	if rec, _ := suggest(t, s, slug, aliceTok, "Catan"); rec.Code != http.StatusOK {
		t.Fatalf("alice: %d", rec.Code)
	}
	// case-insensitive duplicate, different participant
	rec, _ := suggest(t, s, slug, bobTok, "  catan ")
	if rec.Code != http.StatusConflict {
		t.Fatalf("dup: status = %d, want 409", rec.Code)
	}
}

func TestAdvanceWithoutCreatorToken(t *testing.T) {
	s := newTestServer(t)
	slug, _, aliceTok, _ := createVote(t, s, nil)
	if rec, _ := suggest(t, s, slug, aliceTok, "A"); rec.Code != http.StatusOK {
		t.Fatalf("suggest A: %d", rec.Code)
	}
	if rec, _ := suggest(t, s, slug, aliceTok, "B"); rec.Code != http.StatusOK {
		t.Fatalf("suggest B: %d", rec.Code)
	}
	// valid session bearer but no creator token header
	rec, _ := doHdr(t, s, http.MethodPost, "/api/votes/"+slug+"/advance", map[string]any{}, aliceTok, "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

func TestAdvanceWithOneOption(t *testing.T) {
	s := newTestServer(t)
	slug, creatorTok, aliceTok, _ := createVote(t, s, nil)
	if rec, _ := suggest(t, s, slug, aliceTok, "Only"); rec.Code != http.StatusOK {
		t.Fatalf("suggest: %d", rec.Code)
	}
	rec, _ := doHdr(t, s, http.MethodPost, "/api/votes/"+slug+"/advance", map[string]any{}, aliceTok, creatorTok)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", rec.Code)
	}
}

func TestCountBasedSuggestAutoAdvance(t *testing.T) {
	s := newTestServer(t)
	slug, _, aliceTok, _ := createVote(t, s, map[string]any{
		"suggestAdvanceMode":  "count",
		"suggestAdvanceCount": 2,
	})
	bobTok := join(t, s, slug, "Bob")

	// One distinct suggester so far: stays in suggesting.
	_, st1 := suggest(t, s, slug, aliceTok, "Catan")
	if st1["phase"] != "suggesting" {
		t.Fatalf("phase = %v, want suggesting after 1 suggester", st1["phase"])
	}
	// Second distinct suggester reaches the count -> auto-advance.
	_, st2 := suggest(t, s, slug, bobTok, "Wingspan")
	if st2["phase"] != "voting" {
		t.Fatalf("phase = %v, want voting after count reached", st2["phase"])
	}
}

func TestCreatorTiebreakFlow(t *testing.T) {
	s := newTestServer(t)
	slug, creatorTok, aliceTok, ids := setupVoting(t, s, map[string]any{
		"tiebreaker":      "creator",
		"voteAdvanceMode": "all-voted",
	})
	bobTok := join(t, s, slug, "Bob")

	// Alice backs A, Bob backs B -> equal score, both survive -> tie pending.
	if rec, _ := putBallot(t, s, slug, aliceTok, map[string]int{ids["A"]: 1}); rec.Code != http.StatusOK {
		t.Fatalf("alice ballot: %d", rec.Code)
	}
	rec, bobResp := putBallot(t, s, slug, bobTok, map[string]int{ids["B"]: 1})
	if rec.Code != http.StatusOK {
		t.Fatalf("bob ballot: %d", rec.Code)
	}
	if bobResp["phase"] != "results" {
		t.Fatalf("phase = %v, want results", bobResp["phase"])
	}
	res := bobResp["results"].(map[string]any)
	if res["tiePending"] != true {
		t.Fatalf("tiePending = %v, want true", res["tiePending"])
	}
	if res["winnerOptionId"] != "" {
		t.Fatalf("winnerOptionId = %v, want empty while tie pending", res["winnerOptionId"])
	}

	// Wrong (non-tied) winner id -> 400.
	rec, _ = doHdr(t, s, http.MethodPost, "/api/votes/"+slug+"/advance",
		map[string]any{"winnerOptionId": "bogus"}, aliceTok, creatorTok)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad tiebreak winner: status = %d, want 400", rec.Code)
	}

	// Creator picks A.
	rec, resolved := doHdr(t, s, http.MethodPost, "/api/votes/"+slug+"/advance",
		map[string]any{"winnerOptionId": ids["A"]}, aliceTok, creatorTok)
	if rec.Code != http.StatusOK {
		t.Fatalf("resolve: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	rres := resolved["results"].(map[string]any)
	if rres["tiePending"] != false {
		t.Fatalf("tiePending = %v, want false after resolution", rres["tiePending"])
	}
	if rres["winnerOptionId"] != ids["A"] {
		t.Fatalf("winner = %v, want A (%s)", rres["winnerOptionId"], ids["A"])
	}
}

func TestSuggestTimerAutoAdvance(t *testing.T) {
	s := newTestServer(t)
	slug, _, aliceTok, _ := createVote(t, s, map[string]any{"suggestTimerSecs": 1})
	if rec, _ := suggest(t, s, slug, aliceTok, "Catan"); rec.Code != http.StatusOK {
		t.Fatalf("suggest A: %d", rec.Code)
	}
	if rec, _ := suggest(t, s, slug, aliceTok, "Wingspan"); rec.Code != http.StatusOK {
		t.Fatalf("suggest B: %d", rec.Code)
	}

	// Wait for the 1s suggest timer to fire and advance the phase.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		st := getState(t, s, slug, aliceTok)
		if st["phase"] == "voting" {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("phase did not advance to voting after suggest timer expiry")
}

func TestSuggestTimerHoldsWithTooFewOptions(t *testing.T) {
	s := newTestServer(t)
	slug, _, aliceTok, _ := createVote(t, s, map[string]any{"suggestTimerSecs": 1})
	if rec, _ := suggest(t, s, slug, aliceTok, "Only"); rec.Code != http.StatusOK {
		t.Fatalf("suggest: %d", rec.Code)
	}

	time.Sleep(1500 * time.Millisecond)
	st := getState(t, s, slug, aliceTok)
	if st["phase"] != "suggesting" {
		t.Fatalf("phase = %v, want suggesting held with <2 options", st["phase"])
	}
	if st["phaseDeadline"] != nil {
		t.Fatalf("phaseDeadline = %v, want nil (dropped on timer expiry)", st["phaseDeadline"])
	}
}

func TestRevoteWrongPhase(t *testing.T) {
	s := newTestServer(t)
	slug, _, aliceTok, _ := createVote(t, s, nil)
	rec, _ := doJSON(t, s, http.MethodPost, "/api/votes/"+slug+"/revote", map[string]any{}, aliceTok)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (revote outside results)", rec.Code)
	}
}
