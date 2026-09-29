package server_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/HendersonT/quick-vote/internal/clock"
	"github.com/HendersonT/quick-vote/internal/server"
	"github.com/HendersonT/quick-vote/internal/store"
)

func newTestServer(t *testing.T) *server.Server {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return server.New(st, nil)
}

// newFakeClockServer returns a server whose deadlines and timestamps run on
// a manually advanced clock, so timer behavior is tested without sleeping.
func newFakeClockServer(t *testing.T) (*server.Server, *clock.Fake) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	fc := clock.NewFake(time.Unix(1_700_000_000, 0))
	return server.NewWithConfig(st, nil, server.Config{Clock: fc}), fc
}

// doJSON issues an HTTP request against the server's handler directly
// (no network) and decodes the JSON response body into a map.
func doJSON(t *testing.T, s *server.Server, method, path string, body any, token string) (*httptest.ResponseRecorder, map[string]any) {
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

func TestCreateVoteDefaults(t *testing.T) {
	s := newTestServer(t)

	rec, out := doJSON(t, s, http.MethodPost, "/api/votes", map[string]any{
		"title":       "Friday game night",
		"creatorName": "Alice",
	}, "")

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if slug, _ := out["slug"].(string); slug == "" {
		t.Fatalf("missing slug in response: %v", out)
	}
	if tok, _ := out["creatorToken"].(string); tok == "" {
		t.Fatalf("missing creatorToken in response: %v", out)
	}
	if tok, _ := out["sessionToken"].(string); tok == "" {
		t.Fatalf("missing sessionToken in response: %v", out)
	}

	state, ok := out["state"].(map[string]any)
	if !ok {
		t.Fatalf("missing state in response: %v", out)
	}
	if state["phase"] != "suggesting" {
		t.Fatalf("state.phase = %v, want %q", state["phase"], "suggesting")
	}
	if state["phaseDeadline"] != nil {
		t.Fatalf("state.phaseDeadline = %v, want nil (no suggest timer configured)", state["phaseDeadline"])
	}

	settings, ok := state["settings"].(map[string]any)
	if !ok {
		t.Fatalf("missing settings in state: %v", state)
	}
	wantDefaults := map[string]any{
		"maxSuggestionsPerUser": float64(3),
		"creditsPerOption":      float64(3),
		"suggestAdvanceMode":    "manual",
		"voteAdvanceMode":       "all-voted",
		"survivalThreshold":     float64(0),
		"tiebreaker":            "most-backers",
		"revoteThresholdPct":    float64(33),
	}
	for k, want := range wantDefaults {
		if got := settings[k]; got != want {
			t.Errorf("settings[%q] = %v, want %v", k, got, want)
		}
	}

	you, ok := state["you"].(map[string]any)
	if !ok {
		t.Fatalf("missing you in state: %v", state)
	}
	if you["isCreator"] != true {
		t.Fatalf("you.isCreator = %v, want true", you["isCreator"])
	}
	if you["ballot"] != nil {
		t.Fatalf("you.ballot = %v, want nil", you["ballot"])
	}

	if state["results"] != nil {
		t.Fatalf("state.results = %v, want nil", state["results"])
	}
	if state["budget"] != float64(0) {
		t.Fatalf("state.budget = %v, want 0 (no options yet)", state["budget"])
	}
}

func TestCreateVotePartialSettingsOverride(t *testing.T) {
	s := newTestServer(t)

	_, out := doJSON(t, s, http.MethodPost, "/api/votes", map[string]any{
		"title":       "Movie night",
		"creatorName": "Alice",
		"settings":    map[string]any{"creditsPerOption": 5},
	}, "")

	state := out["state"].(map[string]any)
	settings := state["settings"].(map[string]any)

	if settings["creditsPerOption"] != float64(5) {
		t.Fatalf("settings.creditsPerOption = %v, want 5", settings["creditsPerOption"])
	}
	if settings["maxSuggestionsPerUser"] != float64(3) {
		t.Fatalf("settings.maxSuggestionsPerUser = %v, want default 3", settings["maxSuggestionsPerUser"])
	}
	if settings["tiebreaker"] != "most-backers" {
		t.Fatalf("settings.tiebreaker = %v, want default most-backers", settings["tiebreaker"])
	}
}

func TestCreateVoteInvalidSettings(t *testing.T) {
	s := newTestServer(t)

	rec, out := doJSON(t, s, http.MethodPost, "/api/votes", map[string]any{
		"title":       "Bad settings",
		"creatorName": "Alice",
		"settings":    map[string]any{"creditsPerOption": 0},
	}, "")

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if msg, _ := out["error"].(string); msg == "" {
		t.Fatalf("expected non-empty error message, got %v", out)
	}
}

func TestCreateVoteMissingTitle(t *testing.T) {
	s := newTestServer(t)

	rec, out := doJSON(t, s, http.MethodPost, "/api/votes", map[string]any{
		"title":       "",
		"creatorName": "Alice",
	}, "")

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if msg, _ := out["error"].(string); msg == "" {
		t.Fatalf("expected non-empty error message, got %v", out)
	}
}

func TestJoinDedupesName(t *testing.T) {
	s := newTestServer(t)

	_, created := doJSON(t, s, http.MethodPost, "/api/votes", map[string]any{
		"title":       "Trivia night",
		"creatorName": "Sam",
	}, "")
	slug := created["slug"].(string)

	rec, out := doJSON(t, s, http.MethodPost, "/api/votes/"+slug+"/join", map[string]any{
		"name": "Sam",
	}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	state := out["state"].(map[string]any)
	you := state["you"].(map[string]any)
	participants := state["participants"].([]any)

	var joinedName string
	for _, raw := range participants {
		p := raw.(map[string]any)
		if p["id"] == you["participantId"] {
			joinedName, _ = p["name"].(string)
		}
	}
	if joinedName != "Sam (2)" {
		t.Fatalf("joined participant name = %q, want %q", joinedName, "Sam (2)")
	}
	if you["isCreator"] != false {
		t.Fatalf("you.isCreator = %v, want false for joiner", you["isCreator"])
	}
}

func TestJoinUnknownSlug(t *testing.T) {
	s := newTestServer(t)

	rec, out := doJSON(t, s, http.MethodPost, "/api/votes/doesnotexist/join", map[string]any{
		"name": "Anyone",
	}, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if out["error"] != "vote not found" {
		t.Fatalf("error = %v, want %q", out["error"], "vote not found")
	}
}

func TestGetVoteWithCreatorToken(t *testing.T) {
	s := newTestServer(t)

	_, created := doJSON(t, s, http.MethodPost, "/api/votes", map[string]any{
		"title":       "Book club",
		"creatorName": "Alice",
	}, "")
	slug := created["slug"].(string)
	sessionToken := created["sessionToken"].(string)

	rec, out := doJSON(t, s, http.MethodGet, "/api/votes/"+slug, nil, sessionToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	you, ok := out["you"].(map[string]any)
	if !ok {
		t.Fatalf("missing you in state: %v", out)
	}
	if you["isCreator"] != true {
		t.Fatalf("you.isCreator = %v, want true", you["isCreator"])
	}
}

func TestGetVoteWithoutToken(t *testing.T) {
	s := newTestServer(t)

	_, created := doJSON(t, s, http.MethodPost, "/api/votes", map[string]any{
		"title":       "Book club",
		"creatorName": "Alice",
	}, "")
	slug := created["slug"].(string)

	rec, out := doJSON(t, s, http.MethodGet, "/api/votes/"+slug, nil, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if out["you"] != nil {
		t.Fatalf("you = %v, want nil for spectator", out["you"])
	}
}

func TestGetVoteWithInvalidToken(t *testing.T) {
	s := newTestServer(t)

	_, created := doJSON(t, s, http.MethodPost, "/api/votes", map[string]any{
		"title":       "Book club",
		"creatorName": "Alice",
	}, "")
	slug := created["slug"].(string)

	rec, out := doJSON(t, s, http.MethodGet, "/api/votes/"+slug, nil, "not-a-real-token")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if out["you"] != nil {
		t.Fatalf("you = %v, want nil for invalid token", out["you"])
	}
}

func TestGetVoteUnknownSlug(t *testing.T) {
	s := newTestServer(t)

	rec, out := doJSON(t, s, http.MethodGet, "/api/votes/doesnotexist", nil, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if out["error"] != "vote not found" {
		t.Fatalf("error = %v, want %q", out["error"], "vote not found")
	}
}

func TestCreateVoteRejectsOversizedBody(t *testing.T) {
	s := newTestServer(t)

	huge := make([]byte, 128*1024)
	for i := range huge {
		huge[i] = 'a'
	}
	body := map[string]any{"title": string(huge), "creatorName": "Alice"}

	rec, _ := doJSON(t, s, http.MethodPost, "/api/votes", body, "")
	if rec.Code < 400 || rec.Code >= 500 {
		t.Fatalf("expected a 4xx for oversized body, got %d", rec.Code)
	}
}

// TestLengthLimitsCountCharacters: the client caps titles at 200 and names
// at 50 characters, so the server must count characters too, not UTF-8
// bytes — or a title of 150 accented letters is rejected at 300 bytes.
func TestLengthLimitsCountCharacters(t *testing.T) {
	s := newTestServer(t)
	e := func(n int) string { return strings.Repeat("é", n) } // 2 bytes each

	create := func(title, name string) int {
		rec, _ := doJSON(t, s, http.MethodPost, "/api/votes", map[string]any{"title": title, "creatorName": name}, "")
		return rec.Code
	}
	if code := create(e(150), "Alice"); code != http.StatusCreated {
		t.Fatalf("150-character title: %d, want 201", code)
	}
	if code := create(e(201), "Alice"); code != http.StatusBadRequest {
		t.Fatalf("201-character title: %d, want 400", code)
	}
	if code := create("T", e(50)); code != http.StatusCreated {
		t.Fatalf("50-character creator name: %d, want 201", code)
	}
	if code := create("T", e(51)); code != http.StatusBadRequest {
		t.Fatalf("51-character creator name: %d, want 400", code)
	}

	slug, _, aliceTok, _ := createVote(t, s, map[string]any{"maxSuggestionsPerUser": 6})
	if rec, _ := doJSON(t, s, http.MethodPost, "/api/votes/"+slug+"/join", map[string]any{"name": e(50)}, ""); rec.Code != http.StatusOK {
		t.Fatalf("50-character join name: %d, want 200", rec.Code)
	}
	if rec, _ := doJSON(t, s, http.MethodPost, "/api/votes/"+slug+"/join", map[string]any{"name": e(51)}, ""); rec.Code != http.StatusBadRequest {
		t.Fatalf("51-character join name: %d, want 400", rec.Code)
	}
	if rec, _ := suggest(t, s, slug, aliceTok, e(150)); rec.Code != http.StatusOK {
		t.Fatalf("150-character suggestion: %d, want 200", rec.Code)
	}
	if rec, _ := suggest(t, s, slug, aliceTok, e(201)); rec.Code != http.StatusBadRequest {
		t.Fatalf("201-character suggestion: %d, want 400", rec.Code)
	}
}
