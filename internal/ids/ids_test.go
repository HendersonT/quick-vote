package ids

import (
	"regexp"
	"testing"
)

// Confusable-free alphabet: excludes I, O, l (and similar) to avoid
// ambiguous share-link characters.
var slugPattern = regexp.MustCompile(`^[ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789]{10}$`)

func TestNewSlug(t *testing.T) {
	seen := make(map[string]bool, 1000)
	for i := 0; i < 1000; i++ {
		s := NewSlug()
		if len(s) != 10 {
			t.Fatalf("expected slug length 10, got %d (%q)", len(s), s)
		}
		if !slugPattern.MatchString(s) {
			t.Fatalf("slug %q contains characters outside the confusable-free alphabet", s)
		}
		if seen[s] {
			t.Fatalf("duplicate slug generated: %q", s)
		}
		seen[s] = true
	}
}

var tokenPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

func TestNewToken(t *testing.T) {
	seen := make(map[string]bool, 1000)
	for i := 0; i < 1000; i++ {
		tok := NewToken()
		if len(tok) != 32 {
			t.Fatalf("expected token length 32, got %d (%q)", len(tok), tok)
		}
		if !tokenPattern.MatchString(tok) {
			t.Fatalf("token %q is not lowercase hex", tok)
		}
		if seen[tok] {
			t.Fatalf("duplicate token generated: %q", tok)
		}
		seen[tok] = true
	}
}
