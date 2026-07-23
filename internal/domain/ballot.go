package domain

import (
	"fmt"
	"math"
)

// maxCost saturates both a single option's vote cost and the ballot's total
// cost. At voteScalingExponent up to 4.0, math.Pow(v, exponent) can exceed
// math.MaxInt64 for realistically-reachable v (budget = creditsPerOption *
// number of active options is not capped in absolute terms), and converting
// an overflowing float64 to int is implementation-defined — on amd64 it wraps
// to math.MinInt64, a huge NEGATIVE cost that would make BallotCost's
// c > budget check pass a ballot it must reject. maxCost is comfortably
// larger than any credit budget attainable under Settings' documented ranges,
// yet small enough (MaxInt64/4) that summing several saturated per-option
// costs together in BallotCost can never itself wrap back around.
const maxCost = math.MaxInt64 / 4

// BallotCost sums the credit cost of a ballot. For v > 0, cost(v) =
// ceil(v^exponent - 1e-9) (exponent 2.0 reproduces the classic v*v quadratic
// cost). A value of exactly -1 is an explicit veto and costs vetoCost flat,
// regardless of exponent. Any other negative value never contributes here —
// ValidateBallot is responsible for rejecting those before a ballot reaches
// storage/accounting. The running total is clamped to maxCost after every
// term so a pathological ballot (many options, each near the overflow
// threshold) can never wrap the sum negative — see voteCost and maxCost.
func BallotCost(votes map[string]int, exponent float64, vetoCost int) int {
	total := 0
	for _, v := range votes {
		switch {
		case v > 0:
			total += voteCost(v, exponent)
		case v == -1:
			total += vetoCost
		}
		if total > maxCost {
			total = maxCost
		}
	}
	return total
}

// voteCost is the credit price of putting v (>0) credits on a single option.
// The 1e-9 epsilon keeps exact integer powers (e.g. 3^2 == 9) from rounding
// up to the next integer due to floating-point error in math.Pow. The result
// is clamped to maxCost instead of being allowed to overflow int64 on
// conversion (see maxCost), which would otherwise silently wrap to a huge
// negative "cost" and defeat the credit budget entirely.
func voteCost(v int, exponent float64) int {
	c := math.Ceil(math.Pow(float64(v), exponent) - 1e-9)
	if math.IsNaN(c) || c > float64(maxCost) {
		return maxCost
	}
	return int(c)
}

// ValidateBallot checks votes against the option set and the credit budget.
// exponent and vetoCost mirror the vote's (normalized) Settings.
// VoteScalingExponent / VetoCost. A ballot value of -1 means "veto this
// option" and is only legal when vetoCost > 0; any value below -1 is always
// rejected, veto or not.
func ValidateBallot(votes map[string]int, optionIDs []string, budget int, exponent float64, vetoCost int) error {
	known := make(map[string]bool, len(optionIDs))
	for _, id := range optionIDs {
		known[id] = true
	}
	for id, v := range votes {
		if !known[id] {
			return fmt.Errorf("unknown option %q", id)
		}
		switch {
		case v < -1:
			return fmt.Errorf("votes must be -1 (veto) or non-negative")
		case v == -1:
			if vetoCost == 0 {
				return fmt.Errorf("vetoing is not enabled for this vote")
			}
		case v > budget:
			// Cap each positive option before summing costs. A single
			// option can never legitimately cost more than the whole
			// budget (cost(v) >= v for exponent >= 1 implies v <= budget
			// for a valid ballot), so rejecting v > budget never blocks a
			// valid ballot while guarding against a pathologically large v
			// blowing up math.Pow or overflowing the cost sum below.
			return fmt.Errorf("votes for a single option cannot exceed the budget of %d", budget)
		}
	}
	if c := BallotCost(votes, exponent, vetoCost); c > budget {
		return fmt.Errorf("ballot costs %d credits, budget is %d", c, budget)
	}
	return nil
}
