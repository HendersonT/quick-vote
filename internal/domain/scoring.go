package domain

import (
	"math/rand"
	"sort"
)

type OptionResult struct {
	OptionID   string `json:"optionId"`
	Score      int    `json:"score"`
	Backers    int    `json:"backers"`
	Eliminated bool   `json:"eliminated"`
}

type Results struct {
	Scores        []OptionResult `json:"scores"`
	WinnerID      string         `json:"winnerOptionId"`
	TiePending    bool           `json:"tiePending"`
	TiedOptionIDs []string       `json:"tiedOptionIds"`
	TiebreakNote  string         `json:"tiebreakNote"`
	RevoteCalls   int            `json:"revoteCalls"`
	RevoteNeeded  int            `json:"revoteNeeded"`
}

// ComputeResults scores ballots. optionIDs preserves creation order.
// ballots: participantID -> optionID -> votes. rnd used only for tiebreaks.
func ComputeResults(optionIDs []string, ballots map[string]map[string]int,
	survivalThreshold int, tb Tiebreaker, rnd *rand.Rand) Results {

	score := map[string]int{}
	backers := map[string]int{}
	for _, b := range ballots {
		for id, v := range b {
			if v > 0 {
				score[id] += v
				backers[id]++
			}
		}
	}
	res := Results{TiedOptionIDs: []string{}}
	for _, id := range optionIDs {
		res.Scores = append(res.Scores, OptionResult{
			OptionID: id, Score: score[id], Backers: backers[id],
			Eliminated: score[id] < survivalThreshold,
		})
	}
	// stable sort by score desc (keeps creation order within equal scores)
	sortStableByScoreDesc(res.Scores)

	// candidates: surviving options with the max surviving score
	best := -1
	for _, r := range res.Scores {
		if !r.Eliminated && r.Score > best {
			best = r.Score
		}
	}
	if best < 0 {
		return res // everything eliminated: no winner
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
			res.WinnerID = byBackers[0]
			res.TiebreakNote = "tie broken by most distinct backers"
		} else {
			res.WinnerID = byBackers[rnd.Intn(len(byBackers))]
			res.TiebreakNote = "tie broken randomly"
		}
	case TiebreakRandom:
		res.WinnerID = cands[rnd.Intn(len(cands))]
		res.TiebreakNote = "tie broken randomly"
	case TiebreakCreator:
		res.TiePending = true
		res.TiedOptionIDs = cands
		res.TiebreakNote = "tied — waiting for the creator to pick"
	}
	return res
}

func sortStableByScoreDesc(s []OptionResult) {
	sort.SliceStable(s, func(i, j int) bool {
		return s[i].Score > s[j].Score
	})
}
