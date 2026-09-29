package server

import (
	"encoding/csv"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/HendersonT/quick-vote/internal/store"
)

func TestCSVSafe(t *testing.T) {
	cases := map[string]string{
		"":         "",
		"plain":    "plain",
		"=1+1":     "'=1+1",
		"+1":       "'+1",
		"-1":       "'-1",
		"@x":       "'@x",
		"\tx":      "'\tx",
		"\rx":      "'\rx",
		"a=b":      "a=b",
		"'already": "'already",
		"éclair":   "éclair",
	}
	for in, want := range cases {
		if got := csvSafe(in); got != want {
			t.Errorf("csvSafe(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestResultsCSVUnknownOptionFallsBackToID: a scored option with no matching
// row still gets a non-empty, identifiable cell rather than a blank one.
func TestResultsCSVUnknownOptionFallsBackToID(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	s := New(st, nil)
	res := `{"scores":[{"optionId":"o1","score":2,"backers":1},{"optionId":"ghost","score":1,"backers":1}],"winnerOptionId":"o1"}`
	if err := st.CreateVote(store.VoteRow{Slug: "r", Title: "R", Phase: "results", Settings: "{}", CreatorToken: "c",
		Results: &res, CreatedAt: 1}); err != nil {
		t.Fatal(err)
	}
	if err := st.AddParticipant(store.ParticipantRow{ID: "p1", VoteSlug: "r", Name: "Ann", Token: "t", IsCreator: true, JoinedAt: 1}); err != nil {
		t.Fatal(err)
	}
	if err := st.AddOption(store.OptionRow{ID: "o1", VoteSlug: "r", ParticipantID: "p1", Title: "Known", CreatedAt: 1}); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/votes/r/results.csv", nil))
	rows, err := csv.NewReader(strings.NewReader(rec.Body.String())).ReadAll()
	if err != nil || rec.Code != http.StatusOK || len(rows) != 3 {
		t.Fatalf("csv: %d rows=%v err=%v", rec.Code, rows, err)
	}
	if rows[1][1] != "Known" || rows[2][1] != "ghost" {
		t.Fatalf("option cells = %q, %q; want Known, ghost", rows[1][1], rows[2][1])
	}
}
