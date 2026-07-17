package domain

// RevoteNeeded returns the number of re-vote calls required to trigger a
// re-vote, given the current participant count and the configured
// percentage threshold. Always at least 1.
func RevoteNeeded(participants, thresholdPct int) int {
	n := (participants*thresholdPct + 99) / 100 // ceil
	if n < 1 {
		n = 1
	}
	return n
}

// RevoteMet reports whether enough participants have called for a re-vote.
func RevoteMet(calls, participants, thresholdPct int) bool {
	return calls >= RevoteNeeded(participants, thresholdPct)
}
