package server

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/HendersonT/quick-vote/internal/domain"
	"github.com/HendersonT/quick-vote/internal/store"
)

// BuildRoomState assembles the normative room-state snapshot (see the plan's
// "Shared contract: room-state snapshot" section) for a single requester.
// requester is nil when the caller is a spectator (not joined, or presented
// no/an invalid session token) — in that case "you" is null in the result.
//
// It panics if the vote's stored settings or results JSON is malformed;
// that data is only ever written by this package, so corruption indicates a
// programming error rather than bad user input. Callers should run behind a
// panic-recovering middleware.
func BuildRoomState(v store.VoteRow, parts []store.ParticipantRow, opts []store.OptionRow,
	ballots map[string]map[string]int, requester *store.ParticipantRow) map[string]any {

	var settings domain.Settings
	if err := json.Unmarshal([]byte(v.Settings), &settings); err != nil {
		panic(fmt.Errorf("decode vote settings: %w", err))
	}
	// Legacy-decode normalization (F4/F5): a vote created before the
	// "advanced options" round stored settings JSON lacking
	// voteScalingExponent/runoffFallback, which would otherwise silently
	// decode to Go's zero value instead of today's defaults.
	settings = settings.Normalized()

	active, err := decodeActiveOptions(v)
	if err != nil {
		panic(fmt.Errorf("decode vote active options: %w", err))
	}

	suggestedCount := make(map[string]int, len(opts))
	for _, o := range opts {
		suggestedCount[o.ParticipantID]++
	}

	participants := make([]map[string]any, 0, len(parts))
	for _, p := range parts {
		_, hasVoted := ballots[p.ID]
		participants = append(participants, map[string]any{
			"id":             p.ID,
			"name":           p.Name,
			"isCreator":      p.IsCreator,
			"hasSuggested":   suggestedCount[p.ID] > 0,
			"hasVoted":       hasVoted,
			"wantsRevote":    p.WantsRevote,
			"doneSuggesting": p.DoneSuggesting,
		})
	}

	options := make([]map[string]any, 0, len(opts))
	for _, o := range opts {
		options = append(options, map[string]any{
			"id":            o.ID,
			"title":         o.Title,
			"suggestedById": o.ParticipantID,
			"active":        active == nil || active[o.ID],
		})
	}
	budgetOptions := activeOptionIDs(opts, active)

	var phaseDeadline any
	if v.PhaseDeadline != nil {
		phaseDeadline = time.Unix(*v.PhaseDeadline, 0).UTC().Format(time.RFC3339)
	}

	var you any
	if requester != nil {
		// A missing key means the participant never saved a ballot (null).
		// A present-but-empty map means they submitted an empty ballot
		// (abstain), which must round-trip as {} rather than null.
		var ballot map[string]int
		if b, ok := ballots[requester.ID]; ok {
			ballot = b
		}
		you = map[string]any{
			"participantId": requester.ID,
			"isCreator":     requester.IsCreator,
			"ballot":        ballot,
		}
	}

	var results any
	if v.Phase == string(domain.PhaseResults) && v.Results != nil {
		var res domain.Results
		if err := json.Unmarshal([]byte(*v.Results), &res); err != nil {
			panic(fmt.Errorf("decode vote results: %w", err))
		}
		calls := 0
		for _, p := range parts {
			if p.WantsRevote {
				calls++
			}
		}
		res.RevoteCalls = calls
		res.RevoteNeeded = domain.RevoteNeeded(len(parts), settings.RevoteThresholdPct)
		results = res
	}

	return map[string]any{
		"slug":          v.Slug,
		"title":         v.Title,
		"phase":         v.Phase,
		"phaseDeadline": phaseDeadline,
		"settings":      settings,
		"participants":  participants,
		"options":       options,
		// budget is scoped to the active options: during a runoff round
		// (active != nil) only the tied options are votable, so the budget
		// shrinks to match (F5).
		"budget":  settings.CreditsPerOption * len(budgetOptions),
		"you":     you,
		"results": results,
		// runoff is true iff a runoff round (tiebreaker "runoff") is
		// currently restricting voting to a subset of options.
		"runoff": active != nil,
		// closed is true while the creator has closed the room (spec B3):
		// every write is rejected until it is reopened.
		"closed": v.ClosedAt != nil,
	}
}

// decodeActiveOptions parses v.ActiveOptions (a JSON array of option IDs
// restricting voting/scoring to those options during a runoff round, see
// F5) into a set. A nil result (no error) means every option is active —
// the common case, since active_options is NULL outside of a runoff.
func decodeActiveOptions(v store.VoteRow) (map[string]bool, error) {
	if v.ActiveOptions == nil {
		return nil, nil
	}
	var ids []string
	if err := json.Unmarshal([]byte(*v.ActiveOptions), &ids); err != nil {
		return nil, fmt.Errorf("decode active_options: %w", err)
	}
	set := make(map[string]bool, len(ids))
	for _, id := range ids {
		set[id] = true
	}
	return set, nil
}

// activeOptionIDs returns the IDs of opts that are active per active (all of
// them when active is nil), preserving opts' creation order — the order
// ComputeResults relies on for the "earliest" tiebreaker.
func activeOptionIDs(opts []store.OptionRow, active map[string]bool) []string {
	ids := make([]string, 0, len(opts))
	for _, o := range opts {
		if active == nil || active[o.ID] {
			ids = append(ids, o.ID)
		}
	}
	return ids
}
