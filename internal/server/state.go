package server

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/quickvote/quickvote/internal/domain"
	"github.com/quickvote/quickvote/internal/store"
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

	suggestedCount := make(map[string]int, len(opts))
	for _, o := range opts {
		suggestedCount[o.ParticipantID]++
	}

	participants := make([]map[string]any, 0, len(parts))
	for _, p := range parts {
		_, hasVoted := ballots[p.ID]
		participants = append(participants, map[string]any{
			"id":           p.ID,
			"name":         p.Name,
			"isCreator":    p.IsCreator,
			"hasSuggested": suggestedCount[p.ID] > 0,
			"hasVoted":     hasVoted,
			"wantsRevote":  p.WantsRevote,
		})
	}

	options := make([]map[string]any, 0, len(opts))
	for _, o := range opts {
		options = append(options, map[string]any{
			"id":            o.ID,
			"title":         o.Title,
			"suggestedById": o.ParticipantID,
		})
	}

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
		"budget":        settings.CreditsPerOption * len(opts),
		"you":           you,
		"results":       results,
	}
}
