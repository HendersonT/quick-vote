package domain

import "fmt"

func BallotCost(votes map[string]int) int {
	total := 0
	for _, v := range votes {
		total += v * v
	}
	return total
}

// ValidateBallot checks votes against the option set and quadratic budget.
func ValidateBallot(votes map[string]int, optionIDs []string, budget int) error {
	known := make(map[string]bool, len(optionIDs))
	for _, id := range optionIDs {
		known[id] = true
	}
	for id, v := range votes {
		if !known[id] {
			return fmt.Errorf("unknown option %q", id)
		}
		if v < 0 {
			return fmt.Errorf("votes must be non-negative")
		}
		// Cap each option before summing squares. A single option can never
		// legitimately cost more than the whole budget (v^2 <= budget implies
		// v <= budget for budget >= 0), so rejecting v > budget never blocks a
		// valid ballot while preventing v*v from overflowing int64 and wrapping
		// negative to sneak past the budget check below.
		if v > budget {
			return fmt.Errorf("votes for a single option cannot exceed the budget of %d", budget)
		}
	}
	if c := BallotCost(votes); c > budget {
		return fmt.Errorf("ballot costs %d credits, budget is %d", c, budget)
	}
	return nil
}
