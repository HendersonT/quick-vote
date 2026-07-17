package domain

import (
	"strings"
	"testing"
)

func TestBallotCost(t *testing.T) {
	cases := []struct {
		name  string
		votes map[string]int
		want  int
	}{
		{"mixed", map[string]int{"a": 2, "b": 1}, 5},
		{"empty", map[string]int{}, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := BallotCost(c.votes); got != c.want {
				t.Errorf("BallotCost(%v) = %d, want %d", c.votes, got, c.want)
			}
		})
	}
}

func TestValidateBallot(t *testing.T) {
	optionIDs := []string{"a", "b", "c"}

	t.Run("ok at exact budget", func(t *testing.T) {
		votes := map[string]int{"a": 2, "b": 1} // cost 5
		if err := ValidateBallot(votes, optionIDs, 5); err != nil {
			t.Errorf("expected no error, got %v", err)
		}
	})

	t.Run("over budget", func(t *testing.T) {
		votes := map[string]int{"a": 2, "b": 2} // cost 8
		err := ValidateBallot(votes, optionIDs, 5)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "budget") {
			t.Errorf("expected error to mention 'budget', got %q", err.Error())
		}
	})

	t.Run("negative vote", func(t *testing.T) {
		votes := map[string]int{"a": -1}
		if err := ValidateBallot(votes, optionIDs, 5); err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("unknown option key", func(t *testing.T) {
		votes := map[string]int{"z": 1}
		if err := ValidateBallot(votes, optionIDs, 5); err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("empty ballot ok", func(t *testing.T) {
		votes := map[string]int{}
		if err := ValidateBallot(votes, optionIDs, 5); err != nil {
			t.Errorf("expected no error, got %v", err)
		}
	})

	t.Run("per-option overflow rejected", func(t *testing.T) {
		// A vote value near sqrt(maxint64) makes v*v overflow int64 and wrap
		// negative, so a naive cost>budget check would pass. The per-option
		// cap must reject it before the quadratic sum is computed.
		votes := map[string]int{"a": 4000000000}
		if err := ValidateBallot(votes, optionIDs, 9); err == nil {
			t.Fatal("expected error for oversized per-option vote, got nil")
		}
	})

	t.Run("single option cannot exceed budget", func(t *testing.T) {
		// v==budget is the largest value whose square could ever fit the
		// budget only when budget<=1; for budget 5, v=6 clearly overspends.
		votes := map[string]int{"a": 6}
		if err := ValidateBallot(votes, optionIDs, 5); err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("max legitimate single-option vote ok", func(t *testing.T) {
		// budget 9 => a single option can legitimately hold 3 votes (cost 9).
		votes := map[string]int{"a": 3}
		if err := ValidateBallot(votes, optionIDs, 9); err != nil {
			t.Errorf("expected no error, got %v", err)
		}
	})
}
