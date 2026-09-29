package server

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/HendersonT/quick-vote/internal/domain"
	"github.com/HendersonT/quick-vote/internal/store"
)

// handleResultsCSV implements GET /api/votes/{slug}/results.csv. It exports
// per-option totals only — never ballots or participant names — so, like the
// spectator snapshot, it needs no session: anyone with the link can already
// see these numbers in the room.
func (s *Server) handleResultsCSV(w http.ResponseWriter, r *http.Request) {
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
	// Mirror the room snapshot: results only count while the vote is in the
	// results phase (a revote clears them and returns to voting).
	if v.Phase != string(domain.PhaseResults) || v.Results == nil {
		writeError(w, http.StatusConflict, "results aren't in yet")
		return
	}
	var res domain.Results
	if err := json.Unmarshal([]byte(*v.Results), &res); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	opts, err := s.store.Options(slug)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	titles := make(map[string]string, len(opts))
	for _, o := range opts {
		titles[o.ID] = o.Title
	}

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	// slug came back from the store, so it is a server-generated slug from
	// the ids alphabet and safe to embed in the header.
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", slug+"-results.csv"))
	w.WriteHeader(http.StatusOK)

	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"rank", "option", "score", "backers", "vetoes", "eliminated", "winner"})
	for i, sc := range res.Scores {
		title, ok := titles[sc.OptionID]
		if !ok {
			// Options aren't deleted once scored, but if one ever were,
			// its ID still identifies the row better than a blank cell.
			title = sc.OptionID
		}
		_ = cw.Write([]string{
			strconv.Itoa(i + 1),
			csvSafe(title),
			strconv.Itoa(sc.Score),
			strconv.Itoa(sc.Backers),
			strconv.Itoa(sc.VetoCount),
			yesNo(sc.Eliminated),
			// WinnerID is empty while a creator tiebreak is pending; the
			// TiePending check is a guard so a pending tie can never be
			// exported with a winner even if that changes.
			yesNo(sc.OptionID == res.WinnerID && !res.TiePending),
		})
	}
	// The status line is already sent, so a write error (client gone) has
	// nowhere to be reported; the client just sees a truncated download.
	cw.Flush()
}

// csvSafe neutralizes spreadsheet formula injection: a cell starting with
// = + - @ (or a tab/CR, which some spreadsheets strip before evaluating) is
// prefixed with a single quote so it is shown as literal text. Quoting of
// commas, quotes and newlines is left to encoding/csv.
func csvSafe(s string) string {
	if s == "" {
		return s
	}
	switch s[0] {
	case '=', '+', '-', '@', '\t', '\r':
		return "'" + s
	}
	return s
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}
