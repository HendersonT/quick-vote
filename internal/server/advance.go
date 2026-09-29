package server

import (
	crand "crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"time"

	"github.com/HendersonT/quick-vote/internal/domain"
	"github.com/HendersonT/quick-vote/internal/store"
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
	// Legacy-decode normalization (F4/F5): see parseSettings/BuildRoomState.
	settings = settings.Normalized()

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
		// Every option is back in play leaving suggesting, even if a
		// previous round of this same vote somehow left active_options set
		// (defensive; normally nil already at this point).
		v.ActiveOptions = nil
		if settings.VoteTimerSecs > 0 {
			d := s.now().Add(time.Duration(settings.VoteTimerSecs) * time.Second).Unix()
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
		// F5: distinguish a tie resolved after an automatic runoff round tied
		// again (runoffFallback "creator") from a plain creator tiebreak, per
		// the spec's UI requirement (domain.Results.AfterRunoff).
		if res.AfterRunoff {
			res.TiebreakNote = "tie broken after runoff by the creator"
		} else {
			res.TiebreakNote = "tie broken by the creator"
		}
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

// enterResults scores the current ballots — restricted to the active option
// set when a runoff round is already in progress (F5) — and stores the
// results, moving the vote into the results phase and clearing any deadline.
// If tiebreaker "runoff" hits a fresh (not-already-in-runoff) multi-way tie,
// ComputeResults reports RunoffPending instead of a winner/TiePending; in
// that case this starts an automatic runoff round (enterRunoff) rather than
// entering results at all.
func (s *Server) enterResults(v store.VoteRow, settings domain.Settings) error {
	opts, err := s.store.Options(v.Slug)
	if err != nil {
		return err
	}
	ballots, err := s.store.Ballots(v.Slug)
	if err != nil {
		return err
	}
	active, err := decodeActiveOptions(v)
	if err != nil {
		return err
	}
	optionIDs := activeOptionIDs(opts, active)

	tb := domain.TiebreakConfig{
		Tiebreaker:     settings.Tiebreaker,
		RunoffFallback: settings.RunoffFallback,
		InRunoff:       v.ActiveOptions != nil,
	}
	res := domain.ComputeResults(optionIDs, ballots, settings.SurvivalThreshold, tb, newSeededRand())

	if res.RunoffPending {
		return s.enterRunoff(v, settings, res.TiedOptionIDs)
	}

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

// enterRunoff starts an automatic runoff round (F5) restricted to tiedIDs:
// all ballots are cleared, the vote stays in the voting phase with only
// tiedIDs active, and the vote timer is re-armed if configured. The caller
// (advancePhase's PhaseVoting case) is reached only from the voting phase, so
// the phase itself does not need to change.
func (s *Server) enterRunoff(v store.VoteRow, settings domain.Settings, tiedIDs []string) error {
	if err := s.store.DeleteBallots(v.Slug); err != nil {
		return err
	}
	buf, err := json.Marshal(tiedIDs)
	if err != nil {
		return err
	}
	active := string(buf)
	v.ActiveOptions = &active
	v.Phase = string(domain.PhaseVoting)
	v.Results = nil
	v.PhaseDeadline = nil
	if settings.VoteTimerSecs > 0 {
		d := s.now().Add(time.Duration(settings.VoteTimerSecs) * time.Second).Unix()
		v.PhaseDeadline = &d
	}
	if err := s.store.UpdateVote(v); err != nil {
		return err
	}
	s.armOrClear(v.Slug, v.PhaseDeadline)
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
