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
	TiebreakEarliest    Tiebreaker = "earliest"
	TiebreakRunoff      Tiebreaker = "runoff"
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
	// VetoCost is the credit price of vetoing an option (ballot value -1).
	// 0 disables veto entirely (default; preserves current behavior).
	VetoCost int `json:"vetoCost"`
	// VoteScalingExponent controls how steeply the cost of stacking credits
	// on one option grows: cost(v) = ceil(v^exponent - epsilon). 2.0 (the
	// default) reproduces the original quadratic cost.
	VoteScalingExponent float64 `json:"voteScalingExponent"`
	// RunoffFallback is the tiebreaker used to resolve a runoff round that
	// ties again. Never "runoff" itself (Validate rejects that), guaranteeing
	// a runoff terminates after one extra round.
	RunoffFallback Tiebreaker `json:"runoffFallback"`
}

func DefaultSettings() Settings {
	return Settings{
		MaxSuggestionsPerUser: 3,
		CreditsPerOption:      3,
		SuggestAdvanceMode:    "manual",
		VoteAdvanceMode:       "all-voted",
		SurvivalThreshold:     0,
		Tiebreaker:            TiebreakMostBackers,
		RevoteThresholdPct:    33,
		VetoCost:              0,
		VoteScalingExponent:   2.0,
		RunoffFallback:        TiebreakRandom,
	}
}

// Normalized fills in defaults for the fields added in the "advanced options"
// round that a legacy-decoded Settings (JSON stored on a vote created before
// the field existed) leaves at Go's zero value. Without this, an old vote's
// stored settings would silently switch to a linear vote cost (exponent 0)
// or an invalid runoff fallback ("") the moment it's re-decoded. Every
// settings decode site (parseSettings, BuildRoomState, advancePhase, ...)
// must call this before using the settings. VetoCost is not included here:
// its zero value already is the "disabled" default, so no legacy value needs
// remapping.
func (s Settings) Normalized() Settings {
	if s.VoteScalingExponent == 0 {
		s.VoteScalingExponent = 2.0
	}
	if s.RunoffFallback == "" {
		s.RunoffFallback = TiebreakRandom
	}
	return s
}

// Validate clamps/errors: MaxSuggestionsPerUser 1..20, CreditsPerOption 1..100,
// SurvivalThreshold >= 0, RevoteThresholdPct 1..100, timers 0..86400,
// VetoCost 0..100, VoteScalingExponent 1.0..4.0, SuggestAdvanceCount 1..1000
// (when the mode needs a count), modes/tiebreaker/runoffFallback must be one
// of the enumerated strings (runoffFallback may not be "runoff").
func (s Settings) Validate() error {
	if s.MaxSuggestionsPerUser < 1 || s.MaxSuggestionsPerUser > 20 {
		return fmt.Errorf("maxSuggestionsPerUser must be between 1 and 20")
	}
	if s.CreditsPerOption < 1 || s.CreditsPerOption > 100 {
		return fmt.Errorf("creditsPerOption must be between 1 and 100")
	}
	switch s.SuggestAdvanceMode {
	case "manual", "count", "suggestion-count", "all-done":
	default:
		return fmt.Errorf("suggestAdvanceMode must be one of %q, %q, %q, %q",
			"manual", "count", "suggestion-count", "all-done")
	}
	if s.SuggestAdvanceMode == "count" || s.SuggestAdvanceMode == "suggestion-count" {
		if s.SuggestAdvanceCount < 1 || s.SuggestAdvanceCount > 1000 {
			return fmt.Errorf("suggestAdvanceCount must be between 1 and 1000")
		}
	}
	if s.VoteAdvanceMode != "manual" && s.VoteAdvanceMode != "all-voted" {
		return fmt.Errorf("voteAdvanceMode must be %q or %q", "manual", "all-voted")
	}
	if s.SurvivalThreshold < 0 {
		return fmt.Errorf("survivalThreshold must be >= 0")
	}
	switch s.Tiebreaker {
	case TiebreakMostBackers, TiebreakRandom, TiebreakCreator, TiebreakEarliest, TiebreakRunoff:
	default:
		return fmt.Errorf("tiebreaker must be one of %q, %q, %q, %q, %q",
			TiebreakMostBackers, TiebreakRandom, TiebreakCreator, TiebreakEarliest, TiebreakRunoff)
	}
	switch s.RunoffFallback {
	case TiebreakMostBackers, TiebreakRandom, TiebreakCreator, TiebreakEarliest:
	default:
		return fmt.Errorf("runoffFallback must be one of %q, %q, %q, %q",
			TiebreakMostBackers, TiebreakRandom, TiebreakCreator, TiebreakEarliest)
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
	if s.VetoCost < 0 || s.VetoCost > 100 {
		return fmt.Errorf("vetoCost must be between 0 and 100")
	}
	if s.VoteScalingExponent < 1.0 || s.VoteScalingExponent > 4.0 {
		return fmt.Errorf("voteScalingExponent must be between 1.0 and 4.0")
	}
	return nil
}
