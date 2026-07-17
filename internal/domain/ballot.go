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
	}
	if c := BallotCost(votes); c > budget {
		return fmt.Errorf("ballot costs %d credits, budget is %d", c, budget)
	}
	return nil
}
