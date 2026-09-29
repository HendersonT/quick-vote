package server

import "testing"

func TestCSVSafe(t *testing.T) {
	cases := map[string]string{
		"":         "",
		"plain":    "plain",
		"=1+1":     "'=1+1",
		"+1":       "'+1",
		"-1":       "'-1",
		"@x":       "'@x",
		"\tx":      "'\tx",
		"\rx":      "'\rx",
		"a=b":      "a=b",
		"'already": "'already",
		"éclair":   "éclair",
	}
	for in, want := range cases {
		if got := csvSafe(in); got != want {
			t.Errorf("csvSafe(%q) = %q, want %q", in, got, want)
		}
	}
}
