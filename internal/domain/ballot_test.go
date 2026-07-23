package domain

import (
	"strings"
	"testing"
)

func TestBallotCost(t *testing.T) {
	cases := []struct {
		name     string
		votes    map[string]int
		exponent float64
		vetoCost int
		want     int
	}{
		{"mixed quadratic", map[string]int{"a": 2, "b": 1}, 2.0, 0, 5},
		{"empty", map[string]int{}, 2.0, 0, 0},
		{"linear exponent", map[string]int{"a": 2, "b": 1}, 1.0, 0, 3},
		{"veto costs flat vetoCost", map[string]int{"a": -1}, 2.0, 4, 4},
		{"veto plus positive votes", map[string]int{"a": -1, "b": 2}, 2.0, 4, 8},
		{"zero votes contribute nothing", map[string]int{"a": 0}, 2.0, 4, 0},
		{
			// 3^1.5 = 5.196..., exponent 1.5 with the 1e-9 epsilon still
			// ceils up to 6 (not an exact integer power, so the epsilon
			// shouldn't round it down).
			"exponent 1.5 fractional power rounds up", map[string]int{"a": 3}, 1.5, 0, 6,
		},
		{
			// 2^2 = 4 exactly; without the epsilon, float error in math.Pow
			// could push this a hair over 4 and ceil to 5. With exponent
			// 2.0 this is covered by the "mixed quadratic" case (2 -> 4 via
			// a:2 contributing 4 of the 5 total); this case isolates a
			// single exact square.
			"exact integer power not rounded up by epsilon", map[string]int{"a": 2}, 2.0, 0, 4,
		},
		{
			// At exponent 4.0, v^4 overflows math.MaxInt64 around v ~= 55,109.
			// Before the maxCost clamp, int(math.Ceil(math.Pow(60000,4)-1e-9))
			// wrapped to math.MinInt64 on amd64, making BallotCost report a
			// huge NEGATIVE cost for this single option and silently
			// defeating the credit budget. It must instead saturate at
			// maxCost, a huge POSITIVE cost.
			"single-option overflow at exponent 4 saturates instead of wrapping negative",
			map[string]int{"a": 60000}, 4.0, 0, maxCost,
		},
		{
			// Two options each individually overflowing voteCost must not
			// let their saturated costs sum back around to a small or
			// negative total (maxCost = MaxInt64/4 leaves headroom for this).
			"multiple overflowing options still saturate the total, not wrap",
			map[string]int{"a": 60000, "b": 70000}, 4.0, 0, maxCost,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := BallotCost(c.votes, c.exponent, c.vetoCost); got != c.want {
				t.Errorf("BallotCost(%v, %v, %d) = %d, want %d", c.votes, c.exponent, c.vetoCost, got, c.want)
			}
		})
	}
}

func TestValidateBallot(t *testing.T) {
	optionIDs := []string{"a", "b", "c"}

	t.Run("ok at exact budget", func(t *testing.T) {
		votes := map[string]int{"a": 2, "b": 1} // cost 5
		if err := ValidateBallot(votes, optionIDs, 5, 2.0, 0); err != nil {
			t.Errorf("expected no error, got %v", err)
		}
	})

	t.Run("over budget", func(t *testing.T) {
		votes := map[string]int{"a": 2, "b": 2} // cost 8
		err := ValidateBallot(votes, optionIDs, 5, 2.0, 0)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "budget") {
			t.Errorf("expected error to mention 'budget', got %q", err.Error())
		}
	})

	t.Run("negative vote below -1 always rejected", func(t *testing.T) {
		votes := map[string]int{"a": -2}
		if err := ValidateBallot(votes, optionIDs, 5, 2.0, 0); err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("negative vote below -1 rejected even with veto enabled", func(t *testing.T) {
		votes := map[string]int{"a": -5}
		if err := ValidateBallot(votes, optionIDs, 5, 2.0, 3); err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("veto rejected when vetoCost is 0", func(t *testing.T) {
		votes := map[string]int{"a": -1}
		err := ValidateBallot(votes, optionIDs, 5, 2.0, 0)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("veto accepted when vetoCost > 0", func(t *testing.T) {
		votes := map[string]int{"a": -1}
		if err := ValidateBallot(votes, optionIDs, 5, 2.0, 3); err != nil {
			t.Errorf("expected no error, got %v", err)
		}
	})

	t.Run("veto plus positive votes costed together", func(t *testing.T) {
		votes := map[string]int{"a": -1, "b": 2} // veto 3 + cost(2)=4 = 7
		if err := ValidateBallot(votes, optionIDs, 7, 2.0, 3); err != nil {
			t.Errorf("expected no error, got %v", err)
		}
		if err := ValidateBallot(votes, optionIDs, 6, 2.0, 3); err == nil {
			t.Fatal("expected error one credit over budget, got nil")
		}
	})

	t.Run("unknown option key", func(t *testing.T) {
		votes := map[string]int{"z": 1}
		if err := ValidateBallot(votes, optionIDs, 5, 2.0, 0); err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("empty ballot ok", func(t *testing.T) {
		votes := map[string]int{}
		if err := ValidateBallot(votes, optionIDs, 5, 2.0, 0); err != nil {
			t.Errorf("expected no error, got %v", err)
		}
	})

	t.Run("per-option overflow rejected", func(t *testing.T) {
		// A vote value near sqrt(maxint64) makes v*v overflow int64 and wrap
		// negative, so a naive cost>budget check would pass. The per-option
		// cap must reject it before the quadratic sum is computed.
		votes := map[string]int{"a": 4000000000}
		if err := ValidateBallot(votes, optionIDs, 9, 2.0, 0); err == nil {
			t.Fatal("expected error for oversized per-option vote, got nil")
		}
	})

	t.Run("single option cannot exceed budget", func(t *testing.T) {
		// v==budget is the largest value whose square could ever fit the
		// budget only when budget<=1; for budget 5, v=6 clearly overspends.
		votes := map[string]int{"a": 6}
		if err := ValidateBallot(votes, optionIDs, 5, 2.0, 0); err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("max legitimate single-option vote ok", func(t *testing.T) {
		// budget 9 => a single option can legitimately hold 3 votes (cost 9).
		votes := map[string]int{"a": 3}
		if err := ValidateBallot(votes, optionIDs, 9, 2.0, 0); err != nil {
			t.Errorf("expected no error, got %v", err)
		}
	})

	t.Run("linear exponent allows a bigger single-option vote at the same budget", func(t *testing.T) {
		// exponent 1.0 => cost(v) == v, so budget 9 allows up to 9 votes on
		// one option, unlike the quadratic default's cap of 3.
		votes := map[string]int{"a": 9}
		if err := ValidateBallot(votes, optionIDs, 9, 1.0, 0); err != nil {
			t.Errorf("expected no error, got %v", err)
		}
	})

	t.Run("large budget at high exponent no longer bypasses the budget via overflow", func(t *testing.T) {
		// Regression for the confirmed finding: with a large vote (e.g. ~600
		// suggested options at creditsPerOption=100), budget can legitimately
		// reach 60,000. At exponent 4.0, v == budget == 60000 passes the
		// per-option "v > budget" guard, but voteCost(60000, 4) used to
		// overflow int64 and wrap to a huge NEGATIVE number, making
		// BallotCost's c > budget check silently pass. It must now be
		// rejected.
		votes := map[string]int{"a": 60000}
		err := ValidateBallot(votes, optionIDs, 60000, 4.0, 0)
		if err == nil {
			t.Fatal("expected error: a single option costing 60000^4 must never fit a 60000-credit budget")
		}
		if !strings.Contains(err.Error(), "budget") {
			t.Errorf("expected error to mention 'budget', got %q", err.Error())
		}
	})
}
