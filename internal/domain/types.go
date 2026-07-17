package domain

import "fmt"

type Phase string

const (
	PhaseSuggesting Phase = "suggesting"
	PhaseVoting     Phase = "voting"
	PhaseResults    Phase = "results"
)

type Tiebreaker string

const (
	TiebreakMostBackers Tiebreaker = "most-backers"
	TiebreakRandom      Tiebreaker = "random"
	TiebreakCreator     Tiebreaker = "creator"
)

type Settings struct {
	MaxSuggestionsPerUser int        `json:"maxSuggestionsPerUser"`
	CreditsPerOption      int        `json:"creditsPerOption"`
	SuggestAdvanceMode    string     `json:"suggestAdvanceMode"`
	SuggestAdvanceCount   int        `json:"suggestAdvanceCount"`
	VoteAdvanceMode       string     `json:"voteAdvanceMode"`
	SurvivalThreshold     int        `json:"survivalThreshold"`
	Tiebreaker            Tiebreaker `json:"tiebreaker"`
	RevoteThresholdPct    int        `json:"revoteThresholdPct"`
	SuggestTimerSecs      int        `json:"suggestTimerSecs"`
	VoteTimerSecs         int        `json:"voteTimerSecs"`
}

func DefaultSettings() Settings {
	return Settings{
		MaxSuggestionsPerUser: 3,
		CreditsPerOption:      3,
		SuggestAdvanceMode:    "manual",
		VoteAdvanceMode:       "all-voted",
		SurvivalThreshold:     1,
		Tiebreaker:            TiebreakMostBackers,
		RevoteThresholdPct:    33,
	}
}

// Validate clamps/errors: MaxSuggestionsPerUser 1..20, CreditsPerOption 1..100,
// SurvivalThreshold >= 0, RevoteThresholdPct 1..100, timers 0..86400,
// modes/tiebreaker must be one of the enumerated strings.
func (s Settings) Validate() error {
	if s.MaxSuggestionsPerUser < 1 || s.MaxSuggestionsPerUser > 20 {
		return fmt.Errorf("maxSuggestionsPerUser must be between 1 and 20")
	}
	if s.CreditsPerOption < 1 || s.CreditsPerOption > 100 {
		return fmt.Errorf("creditsPerOption must be between 1 and 100")
	}
	if s.SuggestAdvanceMode != "manual" && s.SuggestAdvanceMode != "count" {
		return fmt.Errorf("suggestAdvanceMode must be %q or %q", "manual", "count")
	}
	if s.VoteAdvanceMode != "manual" && s.VoteAdvanceMode != "all-voted" {
		return fmt.Errorf("voteAdvanceMode must be %q or %q", "manual", "all-voted")
	}
	if s.SurvivalThreshold < 0 {
		return fmt.Errorf("survivalThreshold must be >= 0")
	}
	switch s.Tiebreaker {
	case TiebreakMostBackers, TiebreakRandom, TiebreakCreator:
	default:
		return fmt.Errorf("tiebreaker must be one of %q, %q, %q", TiebreakMostBackers, TiebreakRandom, TiebreakCreator)
	}
	if s.RevoteThresholdPct < 1 || s.RevoteThresholdPct > 100 {
		return fmt.Errorf("revoteThresholdPct must be between 1 and 100")
	}
	if s.SuggestTimerSecs < 0 || s.SuggestTimerSecs > 86400 {
		return fmt.Errorf("suggestTimerSecs must be between 0 and 86400")
	}
	if s.VoteTimerSecs < 0 || s.VoteTimerSecs > 86400 {
		return fmt.Errorf("voteTimerSecs must be between 0 and 86400")
	}
	return nil
}
