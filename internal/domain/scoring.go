package domain

import (
	"math/rand"
	"sort"
)

type OptionResult struct {
	OptionID string `json:"optionId"`
	Score    int    `json:"score"`
	Backers  int    `json:"backers"`
	// VetoCount is the number of participants who explicitly vetoed this
	// option (ballot value -1). Any vetoCount > 0 eliminates the option
	// regardless of its score.
	VetoCount  int  `json:"vetoCount"`
	Eliminated bool `json:"eliminated"`
}

type Results struct {
	Scores        []OptionResult `json:"scores"`
	WinnerID      string         `json:"winnerOptionId"`
	TiePending    bool           `json:"tiePending"`
	TiedOptionIDs []string       `json:"tiedOptionIds"`
	TiebreakNote  string         `json:"tiebreakNote"`
	RevoteCalls   int            `json:"revoteCalls"`
	RevoteNeeded  int            `json:"revoteNeeded"`
	// RunoffPending is true when tiebreaker "runoff" hit a fresh (not
	// already-in-runoff) multi-way tie: the caller must start a runoff
	// round (see TiebreakConfig.InRunoff) instead of treating this as final
	// results. TiedOptionIDs holds the tied options in that case.
	RunoffPending bool `json:"runoffPending"`
	// AfterRunoff is true when a pending (TiePending) tie was reached after an
	// automatic runoff round tied again (tiebreaker "runoff", runoffFallback
	// "creator"). advancePhase uses it to pick a TiebreakNote distinguishable
	// from a plain (non-runoff) creator tiebreak once the creator resolves
	// the pending tie — see F5.
	AfterRunoff bool `json:"afterRunoff"`
}

// TiebreakConfig carries the runoff context ComputeResults needs on top of
// the plain Tiebreaker: which tiebreaker to fall back to if a runoff round
// ties again, and whether this call is already scoring a runoff round.
type TiebreakConfig struct {
	Tiebreaker     Tiebreaker
	RunoffFallback Tiebreaker
	InRunoff       bool
}

// ComputeResults scores ballots. optionIDs preserves creation order.
// ballots: participantID -> optionID -> votes, where -1 means an explicit
// veto of that option by that participant. rnd is used only for tiebreaks.
func ComputeResults(optionIDs []string, ballots map[string]map[string]int,
	survivalThreshold int, tb TiebreakConfig, rnd *rand.Rand) Results {

	score := map[string]int{}
	backers := map[string]int{}
	vetoes := map[string]int{}
	for _, b := range ballots {
		for id, v := range b {
			switch {
			case v > 0:
				score[id] += v
				backers[id]++
			case v == -1:
				vetoes[id]++
			}
		}
	}
	res := Results{TiedOptionIDs: []string{}}
	for _, id := range optionIDs {
		res.Scores = append(res.Scores, OptionResult{
			OptionID:  id,
			Score:     score[id],
			Backers:   backers[id],
			VetoCount: vetoes[id],
			// A veto is fatal regardless of score: a vetoed option can
			// never win even if its remaining backers clear the threshold.
			Eliminated: vetoes[id] > 0 || score[id] < survivalThreshold,
		})
	}
	// stable sort by score desc (keeps creation order within equal scores)
	sortStableByScoreDesc(res.Scores)

	// candidates: surviving (non-eliminated, non-vetoed) options with the
	// max surviving score
	best := -1
	for _, r := range res.Scores {
		if !r.Eliminated && r.Score > best {
			best = r.Score
		}
	}
	if best < 0 {
		return res // everything eliminated or vetoed: no winner
	}
	var cands []string
	for _, r := range res.Scores {
		if !r.Eliminated && r.Score == best {
			cands = append(cands, r.OptionID)
		}
	}
	if len(cands) == 1 {
		res.WinnerID = cands[0]
		return res
	}

	// Multi-way tie among survivors. "runoff" gets special handling: a
	// fresh tie (not already scoring a runoff round) pauses here instead of
	// picking a winner, so the caller can re-open voting on just the tied
	// options. A tie that recurs within the runoff itself falls through to
	// the configured fallback tiebreaker, guaranteeing termination.
	if tb.Tiebreaker == TiebreakRunoff && !tb.InRunoff {
		res.RunoffPending = true
		res.TiedOptionIDs = cands
		res.TiebreakNote = "tie triggers a runoff vote among the tied options"
		return res
	}
	effective := tb.Tiebreaker
	afterRunoff := false
	if tb.Tiebreaker == TiebreakRunoff {
		effective = tb.RunoffFallback
		afterRunoff = true
	}

	winner, note, tiePending := resolveTie(cands, backers, effective, rnd, afterRunoff)
	if tiePending {
		res.TiePending = true
		res.TiedOptionIDs = cands
		res.TiebreakNote = note
		res.AfterRunoff = afterRunoff
		return res
	}
	res.WinnerID = winner
	res.TiebreakNote = note
	return res
}

// resolveTie picks a winner among tied candidates (or reports the tie is
// still pending, for the "creator" tiebreaker) per tb. afterRunoff produces a
// distinguishable TiebreakNote when this resolution followed an automatic
// runoff round that tied again, versus resolving directly.
func resolveTie(cands []string, backers map[string]int, tb Tiebreaker, rnd *rand.Rand, afterRunoff bool) (winner, note string, tiePending bool) {
	switch tb {
	case TiebreakMostBackers:
		maxB := -1
		for _, id := range cands {
			if backers[id] > maxB {
				maxB = backers[id]
			}
		}
		var byBackers []string
		for _, id := range cands {
			if backers[id] == maxB {
				byBackers = append(byBackers, id)
			}
		}
		if len(byBackers) == 1 {
			if afterRunoff {
				return byBackers[0], "tie broken after runoff by most distinct backers", false
			}
			return byBackers[0], "tie broken by most distinct backers", false
		}
		w := byBackers[rnd.Intn(len(byBackers))]
		if afterRunoff {
			return w, "tie broken after runoff, randomly", false
		}
		return w, "tie broken randomly", false
	case TiebreakRandom:
		w := cands[rnd.Intn(len(cands))]
		if afterRunoff {
			return w, "tie broken after runoff, randomly", false
		}
		return w, "tie broken randomly", false
	case TiebreakEarliest:
		// cands is derived from res.Scores, itself stably sorted by score
		// desc from the creation-ordered optionIDs input, so among equal
		// scores cands[0] is the earliest-suggested option.
		if afterRunoff {
			return cands[0], "tie broken after runoff by earliest suggestion", false
		}
		return cands[0], "tie broken by earliest suggestion", false
	case TiebreakCreator:
		if afterRunoff {
			return "", "tied after runoff — waiting for the creator to pick", true
		}
		return "", "tied — waiting for the creator to pick", true
	default:
		// Unreachable for a Validate-passing Settings (unknown tiebreakers
		// and runoffFallback == "runoff" are both rejected there), but
		// resolve safely rather than leaving no winner.
		return cands[rnd.Intn(len(cands))], "tie broken randomly", false
	}
}

func sortStableByScoreDesc(s []OptionResult) {
	sort.SliceStable(s, func(i, j int) bool {
		return s[i].Score > s[j].Score
	})
}
