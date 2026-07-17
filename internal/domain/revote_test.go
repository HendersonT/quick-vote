package domain

import "testing"

func TestRevoteNeeded(t *testing.T) {
	cases := []struct {
		participants, thresholdPct, want int
	}{
		{3, 33, 1},
		{4, 33, 2},
		{10, 33, 4},
		{2, 50, 1},
		{3, 50, 2},
		{5, 100, 5},
		{1, 1, 1}, // min 1 even for tiny threshold
	}
	for _, c := range cases {
		got := RevoteNeeded(c.participants, c.thresholdPct)
		if got != c.want {
			t.Errorf("RevoteNeeded(%d, %d) = %d, want %d", c.participants, c.thresholdPct, got, c.want)
		}
	}
}

func TestRevoteMet(t *testing.T) {
	cases := []struct {
		calls, participants, thresholdPct int
		want                              bool
	}{
		{0, 3, 33, false},
		{1, 3, 33, true},
		{1, 4, 33, false},
		{2, 4, 33, true},
		{4, 10, 33, true},
		{3, 10, 33, false},
	}
	for _, c := range cases {
		got := RevoteMet(c.calls, c.participants, c.thresholdPct)
		if got != c.want {
			t.Errorf("RevoteMet(%d, %d, %d) = %v, want %v", c.calls, c.participants, c.thresholdPct, got, c.want)
		}
	}
}
