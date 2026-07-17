package server

import (
	crand "crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"time"

	"github.com/quickvote/quickvote/internal/domain"
	"github.com/quickvote/quickvote/internal/store"
)

// Sentinel errors returned by advancePhase so callers can map them to HTTP
// status codes.
var (
	errNeedTwoSuggestions = errors.New("need at least 2 suggestions")
	errAlreadyResults     = errors.New("already at results")
	errNoTiePending       = errors.New("no tiebreak is pending")
	errBadTiebreakWinner  = errors.New("winnerOptionId must be one of the tied options")
)

// advancePhase is the single phase-transition code path used by manual creator
// advances, auto-triggers (suggest-count / all-voted), and timer expiry.
//
//   - suggesting -> voting: requires >=2 options (else errNeedTwoSuggestions);
//     arms the vote timer if configured.
//   - voting -> results: computes and stores results; clears any deadline.
//   - results: with tiebreakWinner set, resolves a pending creator tiebreak;
//     otherwise errAlreadyResults.
//
// byCreator currently only documents intent; the transition rules do not
// differ by trigger source (the timer's <2-options special case is handled by
// the caller inspecting errNeedTwoSuggestions).
func (s *Server) advancePhase(slug string, byCreator bool, tiebreakWinner string) error {
	v, err := s.store.GetVote(slug)
	if err != nil {
		return err
	}
	var settings domain.Settings
	if err := json.Unmarshal([]byte(v.Settings), &settings); err != nil {
		return fmt.Errorf("decode settings: %w", err)
	}

	switch domain.Phase(v.Phase) {
	case domain.PhaseSuggesting:
		opts, err := s.store.Options(slug)
		if err != nil {
			return err
		}
		if len(opts) < 2 {
			return errNeedTwoSuggestions
		}
		v.Phase = string(domain.PhaseVoting)
		v.Results = nil
		v.PhaseDeadline = nil
		if settings.VoteTimerSecs > 0 {
			d := time.Now().Add(time.Duration(settings.VoteTimerSecs) * time.Second).Unix()
			v.PhaseDeadline = &d
		}
		if err := s.store.UpdateVote(v); err != nil {
			return err
		}
		s.armOrClear(slug, v.PhaseDeadline)
		return nil

	case domain.PhaseVoting:
		return s.enterResults(v, settings)

	case domain.PhaseResults:
		if v.Results == nil || tiebreakWinner == "" {
			return errAlreadyResults
		}
		var res domain.Results
		if err := json.Unmarshal([]byte(*v.Results), &res); err != nil {
			return fmt.Errorf("decode results: %w", err)
		}
		if !res.TiePending {
			return errNoTiePending
		}
		valid := false
		for _, id := range res.TiedOptionIDs {
			if id == tiebreakWinner {
				valid = true
				break
			}
		}
		if !valid {
			return errBadTiebreakWinner
		}
		res.WinnerID = tiebreakWinner
		res.TiePending = false
		res.TiebreakNote = "tie broken by the creator"
		buf, err := json.Marshal(res)
		if err != nil {
			return err
		}
		rs := string(buf)
		v.Results = &rs
		return s.store.UpdateVote(v)

	default:
		return fmt.Errorf("unknown phase %q", v.Phase)
	}
}

// enterResults scores the current ballots and stores the results, moving the
// vote into the results phase and clearing any deadline.
func (s *Server) enterResults(v store.VoteRow, settings domain.Settings) error {
	opts, err := s.store.Options(v.Slug)
	if err != nil {
		return err
	}
	ballots, err := s.store.Ballots(v.Slug)
	if err != nil {
		return err
	}
	optionIDs := make([]string, 0, len(opts))
	for _, o := range opts {
		optionIDs = append(optionIDs, o.ID)
	}
	res := domain.ComputeResults(optionIDs, ballots, settings.SurvivalThreshold, settings.Tiebreaker, newSeededRand())
	buf, err := json.Marshal(res)
	if err != nil {
		return err
	}
	rs := string(buf)
	v.Phase = string(domain.PhaseResults)
	v.Results = &rs
	v.PhaseDeadline = nil
	if err := s.store.UpdateVote(v); err != nil {
		return err
	}
	s.armOrClear(v.Slug, nil)
	return nil
}

// newSeededRand returns a math/rand source seeded from crypto/rand, used only
// for tiebreak randomness.
func newSeededRand() *rand.Rand {
	var b [8]byte
	if _, err := crand.Read(b[:]); err != nil {
		return rand.New(rand.NewSource(time.Now().UnixNano()))
	}
	return rand.New(rand.NewSource(int64(binary.LittleEndian.Uint64(b[:]))))
}
