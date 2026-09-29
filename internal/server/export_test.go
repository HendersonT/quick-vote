package server_test

import (
	"encoding/csv"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func getCSV(t *testing.T, s http.Handler, slug string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/votes/"+slug+"/results.csv", nil)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	return rec
}

func TestResultsCSVNeutralizesFormulas(t *testing.T) {
	s := newTestServer(t)
	slug, ct, aliceTok, _ := createVote(t, s, map[string]any{"suggestAdvanceMode": "manual", "voteAdvanceMode": "manual", "maxSuggestionsPerUser": 6})
	titles := []string{`=HYPERLINK("x")`, "+1", "-cmd", "@SUM(A1)", `Plain, with "quotes"`, "multi\nline"}
	for _, ti := range titles {
		if rec, _ := suggest(t, s, slug, aliceTok, ti); rec.Code != http.StatusOK {
			t.Fatalf("suggest %q: %d %s", ti, rec.Code, rec.Body.String())
		}
	}
	doHdr(t, s, http.MethodPost, "/api/votes/"+slug+"/advance", map[string]any{}, aliceTok, ct)
	ids := optionTitleToID(t, getState(t, s, slug, aliceTok))
	if rec, _ := putBallot(t, s, slug, aliceTok, map[string]int{ids[`=HYPERLINK("x")`]: 2}); rec.Code != http.StatusOK {
		t.Fatalf("ballot: %d %s", rec.Code, rec.Body.String())
	}
	doHdr(t, s, http.MethodPost, "/api/votes/"+slug+"/advance", map[string]any{}, aliceTok, ct)

	rec := getCSV(t, s, slug)
	if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/csv") {
		t.Fatalf("csv: %d %q %s", rec.Code, rec.Header().Get("Content-Type"), rec.Body.String())
	}
	if cd := rec.Header().Get("Content-Disposition"); !strings.Contains(cd, slug+"-results.csv") || !strings.HasPrefix(cd, "attachment") {
		t.Fatalf("Content-Disposition = %q", cd)
	}
	body := rec.Body.String()
	rows, err := csv.NewReader(strings.NewReader(body)).ReadAll()
	if err != nil {
		t.Fatalf("parse csv: %v", err)
	}
	if strings.Join(rows[0], ",") != "rank,option,score,backers,vetoes,eliminated,winner" {
		t.Fatalf("header = %v", rows[0])
	}
	if len(rows) != 1+len(titles) {
		t.Fatalf("rows = %d, want %d", len(rows), 1+len(titles))
	}
	if rows[1][1] != `'=HYPERLINK("x")` || rows[1][6] != "yes" || rows[1][0] != "1" || rows[1][2] != "2" {
		t.Fatalf("top row = %v", rows[1])
	}
	got := map[string]bool{}
	for i, r := range rows[1:] {
		if c := r[1][0]; c == '=' || c == '+' || c == '-' || c == '@' {
			t.Fatalf("formula-leading cell survived: %q", r[1])
		}
		if i > 0 && r[6] != "no" {
			t.Fatalf("non-top row marked winner: %v", r)
		}
		got[r[1]] = true
	}
	for _, want := range []string{"'+1", "'-cmd", "'@SUM(A1)", `Plain, with "quotes"`, "multi\nline"} {
		if !got[want] {
			t.Fatalf("missing option cell %q in %v", want, rows)
		}
	}
	if strings.Contains(body, aliceTok) || strings.Contains(body, ct) || strings.Contains(body, "Alice") {
		t.Fatal("CSV must not include participant tokens or names")
	}
}

func TestResultsCSVBeforeResults(t *testing.T) {
	s := newTestServer(t)
	slug, _, _, _ := createVote(t, s, nil)
	if rec := getCSV(t, s, slug); rec.Code != http.StatusConflict {
		t.Fatalf("csv before results: %d, want 409", rec.Code)
	}
}

func TestResultsCSVUnknownVote(t *testing.T) {
	s := newTestServer(t)
	if rec := getCSV(t, s, "nope-nope-nope"); rec.Code != http.StatusNotFound {
		t.Fatalf("csv unknown vote: %d, want 404", rec.Code)
	}
}
