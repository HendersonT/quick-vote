package domain

import "testing"

func TestDefaultSettingsIsValid(t *testing.T) {
	if err := DefaultSettings().Validate(); err != nil {
		t.Fatalf("DefaultSettings() failed Validate(): %v", err)
	}
}

func TestDefaultSettingsPreservesCurrentBehavior(t *testing.T) {
	d := DefaultSettings()
	if d.VetoCost != 0 {
		t.Errorf("VetoCost = %d, want 0 (feature off by default)", d.VetoCost)
	}
	if d.VoteScalingExponent != 2.0 {
		t.Errorf("VoteScalingExponent = %v, want 2.0 (quadratic default)", d.VoteScalingExponent)
	}
	if d.RunoffFallback != TiebreakRandom {
		t.Errorf("RunoffFallback = %q, want %q", d.RunoffFallback, TiebreakRandom)
	}
}

func TestSettingsValidate(t *testing.T) {
	base := DefaultSettings()

	cases := []struct {
		name    string
		mutate  func(s Settings) Settings
		wantErr bool
	}{
		{"defaults ok", func(s Settings) Settings { return s }, false},
		{"vetoCost negative", func(s Settings) Settings { s.VetoCost = -1; return s }, true},
		{"vetoCost 0 ok (off)", func(s Settings) Settings { s.VetoCost = 0; return s }, false},
		{"vetoCost 100 ok (max)", func(s Settings) Settings { s.VetoCost = 100; return s }, false},
		{"vetoCost 101 rejected", func(s Settings) Settings { s.VetoCost = 101; return s }, true},
		{"voteScalingExponent below 1.0 rejected", func(s Settings) Settings { s.VoteScalingExponent = 0.9; return s }, true},
		{"voteScalingExponent 1.0 ok (linear)", func(s Settings) Settings { s.VoteScalingExponent = 1.0; return s }, false},
		{"voteScalingExponent 4.0 ok (max)", func(s Settings) Settings { s.VoteScalingExponent = 4.0; return s }, false},
		{"voteScalingExponent above 4.0 rejected", func(s Settings) Settings { s.VoteScalingExponent = 4.1; return s }, true},
		{"tiebreaker earliest ok", func(s Settings) Settings { s.Tiebreaker = TiebreakEarliest; return s }, false},
		{"tiebreaker runoff ok", func(s Settings) Settings { s.Tiebreaker = TiebreakRunoff; return s }, false},
		{"tiebreaker unknown rejected", func(s Settings) Settings { s.Tiebreaker = "bogus"; return s }, true},
		{"runoffFallback most-backers ok", func(s Settings) Settings { s.RunoffFallback = TiebreakMostBackers; return s }, false},
		{"runoffFallback earliest ok", func(s Settings) Settings { s.RunoffFallback = TiebreakEarliest; return s }, false},
		{"runoffFallback runoff rejected", func(s Settings) Settings { s.RunoffFallback = TiebreakRunoff; return s }, true},
		{"runoffFallback unknown rejected", func(s Settings) Settings { s.RunoffFallback = "bogus"; return s }, true},
		{"suggestAdvanceMode suggestion-count ok with count", func(s Settings) Settings {
			s.SuggestAdvanceMode = "suggestion-count"
			s.SuggestAdvanceCount = 5
			return s
		}, false},
		{"suggestAdvanceMode all-done ok without count", func(s Settings) Settings {
			s.SuggestAdvanceMode = "all-done"
			return s
		}, false},
		{"suggestAdvanceMode unknown rejected", func(s Settings) Settings { s.SuggestAdvanceMode = "bogus"; return s }, true},
		{"suggestAdvanceMode count requires count 1..1000 (0 rejected)", func(s Settings) Settings {
			s.SuggestAdvanceMode = "count"
			s.SuggestAdvanceCount = 0
			return s
		}, true},
		{"suggestAdvanceMode count requires count 1..1000 (1 ok)", func(s Settings) Settings {
			s.SuggestAdvanceMode = "count"
			s.SuggestAdvanceCount = 1
			return s
		}, false},
		{"suggestAdvanceMode count requires count 1..1000 (1000 ok)", func(s Settings) Settings {
			s.SuggestAdvanceMode = "count"
			s.SuggestAdvanceCount = 1000
			return s
		}, false},
		{"suggestAdvanceMode count requires count 1..1000 (1001 rejected)", func(s Settings) Settings {
			s.SuggestAdvanceMode = "count"
			s.SuggestAdvanceCount = 1001
			return s
		}, true},
		{"suggestAdvanceMode suggestion-count requires count 1..1000 (0 rejected)", func(s Settings) Settings {
			s.SuggestAdvanceMode = "suggestion-count"
			s.SuggestAdvanceCount = 0
			return s
		}, true},
		{"suggestAdvanceMode manual ignores count of 0", func(s Settings) Settings {
			s.SuggestAdvanceMode = "manual"
			s.SuggestAdvanceCount = 0
			return s
		}, false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.mutate(base).Validate()
			if c.wantErr && err == nil {
				t.Errorf("expected error, got nil")
			}
			if !c.wantErr && err != nil {
				t.Errorf("expected no error, got %v", err)
			}
		})
	}
}

func TestSettingsNormalized(t *testing.T) {
	t.Run("legacy zero-value exponent maps to 2.0", func(t *testing.T) {
		s := Settings{VoteScalingExponent: 0}
		got := s.Normalized()
		if got.VoteScalingExponent != 2.0 {
			t.Errorf("VoteScalingExponent = %v, want 2.0", got.VoteScalingExponent)
		}
	})

	t.Run("legacy empty runoffFallback maps to random", func(t *testing.T) {
		s := Settings{RunoffFallback: ""}
		got := s.Normalized()
		if got.RunoffFallback != TiebreakRandom {
			t.Errorf("RunoffFallback = %q, want %q", got.RunoffFallback, TiebreakRandom)
		}
	})

	t.Run("non-zero exponent left untouched", func(t *testing.T) {
		s := Settings{VoteScalingExponent: 1.5}
		got := s.Normalized()
		if got.VoteScalingExponent != 1.5 {
			t.Errorf("VoteScalingExponent = %v, want unchanged 1.5", got.VoteScalingExponent)
		}
	})

	t.Run("non-empty runoffFallback left untouched", func(t *testing.T) {
		s := Settings{RunoffFallback: TiebreakCreator}
		got := s.Normalized()
		if got.RunoffFallback != TiebreakCreator {
			t.Errorf("RunoffFallback = %q, want unchanged %q", got.RunoffFallback, TiebreakCreator)
		}
	})

	t.Run("vetoCost zero is left as-is (already the disabled default)", func(t *testing.T) {
		s := Settings{VetoCost: 0}
		got := s.Normalized()
		if got.VetoCost != 0 {
			t.Errorf("VetoCost = %d, want 0", got.VetoCost)
		}
	})

	t.Run("normalizing default settings is a no-op", func(t *testing.T) {
		d := DefaultSettings()
		got := d.Normalized()
		if got != d {
			t.Errorf("Normalized() changed DefaultSettings(): got %+v, want %+v", got, d)
		}
	})
}
