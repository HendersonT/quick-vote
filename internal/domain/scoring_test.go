package domain

import (
	"math/rand"
	"reflect"
	"sort"
	"testing"
)

func scoreOf(t *testing.T, res Results, optionID string) OptionResult {
	t.Helper()
	for _, r := range res.Scores {
		if r.OptionID == optionID {
			return r
		}
	}
	t.Fatalf("option %q not found in results %+v", optionID, res)
	return OptionResult{}
}

func TestComputeResults_SimpleWinner(t *testing.T) {
	optionIDs := []string{"a", "b"}
	ballots := map[string]map[string]int{
		"p1": {"a": 2, "b": 1},
	}
	res := ComputeResults(optionIDs, ballots, 0, TiebreakConfig{Tiebreaker: TiebreakMostBackers}, rand.New(rand.NewSource(1)))
	if res.WinnerID != "a" {
		t.Errorf("WinnerID = %q, want %q", res.WinnerID, "a")
	}
	if res.TiePending {
		t.Errorf("TiePending = true, want false")
	}
}

func TestComputeResults_ScoreSumsAcrossBallots(t *testing.T) {
	optionIDs := []string{"a", "b"}
	ballots := map[string]map[string]int{
		"p1": {"a": 2},
		"p2": {"a": 1},
		"p3": {"b": 5},
	}
	res := ComputeResults(optionIDs, ballots, 0, TiebreakConfig{Tiebreaker: TiebreakMostBackers}, rand.New(rand.NewSource(1)))
	if got := scoreOf(t, res, "a").Score; got != 3 {
		t.Errorf("score a = %d, want 3", got)
	}
	if got := scoreOf(t, res, "b").Score; got != 5 {
		t.Errorf("score b = %d, want 5", got)
	}
	if res.WinnerID != "b" {
		t.Errorf("WinnerID = %q, want %q", res.WinnerID, "b")
	}
}

func TestComputeResults_BackersCountOnlyPositiveVotes(t *testing.T) {
	optionIDs := []string{"a"}
	ballots := map[string]map[string]int{
		"p1": {"a": 2},
		"p2": {"a": 0},
		"p3": {"a": 3},
	}
	res := ComputeResults(optionIDs, ballots, 0, TiebreakConfig{Tiebreaker: TiebreakMostBackers}, rand.New(rand.NewSource(1)))
	a := scoreOf(t, res, "a")
	if a.Score != 5 {
		t.Errorf("score a = %d, want 5", a.Score)
	}
	if a.Backers != 2 {
		t.Errorf("backers a = %d, want 2 (p2's zero vote should not count)", a.Backers)
	}
}

func TestComputeResults_EliminationThresholdOne(t *testing.T) {
	// threshold 1 => a total score of zero is a veto.
	optionIDs := []string{"a", "b", "c"}
	ballots := map[string]map[string]int{
		"p1": {"a": 2, "b": 0},
	}
	res := ComputeResults(optionIDs, ballots, 1, TiebreakConfig{Tiebreaker: TiebreakMostBackers}, rand.New(rand.NewSource(1)))
	if got := scoreOf(t, res, "a").Eliminated; got {
		t.Errorf("a eliminated = true, want false (score 2 >= threshold 1)")
	}
	if got := scoreOf(t, res, "b").Eliminated; !got {
		t.Errorf("b eliminated = false, want true (score 0 < threshold 1)")
	}
	if got := scoreOf(t, res, "c").Eliminated; !got {
		t.Errorf("c eliminated = false, want true (score 0 < threshold 1, no votes)")
	}
	if res.WinnerID != "a" {
		t.Errorf("WinnerID = %q, want %q", res.WinnerID, "a")
	}
}

func TestComputeResults_EliminationThresholdThree(t *testing.T) {
	// threshold 3 => score 2 eliminated.
	optionIDs := []string{"a", "b"}
	ballots := map[string]map[string]int{
		"p1": {"a": 3, "b": 2},
	}
	res := ComputeResults(optionIDs, ballots, 3, TiebreakConfig{Tiebreaker: TiebreakMostBackers}, rand.New(rand.NewSource(1)))
	if got := scoreOf(t, res, "a").Eliminated; got {
		t.Errorf("a eliminated = true, want false (score 3 >= threshold 3)")
	}
	if got := scoreOf(t, res, "b").Eliminated; !got {
		t.Errorf("b eliminated = false, want true (score 2 < threshold 3)")
	}
	if res.WinnerID != "a" {
		t.Errorf("WinnerID = %q, want %q", res.WinnerID, "a")
	}
}

func TestComputeResults_AllEliminated(t *testing.T) {
	optionIDs := []string{"a", "b"}
	ballots := map[string]map[string]int{
		"p1": {"a": 1, "b": 1},
	}
	res := ComputeResults(optionIDs, ballots, 5, TiebreakConfig{Tiebreaker: TiebreakMostBackers}, rand.New(rand.NewSource(1)))
	if res.WinnerID != "" {
		t.Errorf("WinnerID = %q, want empty (all eliminated)", res.WinnerID)
	}
	if res.TiePending {
		t.Errorf("TiePending = true, want false (all-eliminated is not a tie)")
	}
	if len(res.Scores) != 2 {
		t.Fatalf("expected 2 score entries, got %d", len(res.Scores))
	}
	for _, r := range res.Scores {
		if !r.Eliminated {
			t.Errorf("option %q eliminated = false, want true", r.OptionID)
		}
	}
}

func TestComputeResults_MostBackersTiebreakPicksMoreBackers(t *testing.T) {
	// a: 1 backer contributes all 4 points. b: 2 backers contribute 2 each.
	// Both score 4 (tied); b should win on more distinct backers.
	optionIDs := []string{"a", "b"}
	ballots := map[string]map[string]int{
		"p1": {"a": 4},
		"p2": {"b": 2},
		"p3": {"b": 2},
	}
	res := ComputeResults(optionIDs, ballots, 1, TiebreakConfig{Tiebreaker: TiebreakMostBackers}, rand.New(rand.NewSource(1)))
	if res.WinnerID != "b" {
		t.Errorf("WinnerID = %q, want %q (more backers)", res.WinnerID, "b")
	}
	if res.TiePending {
		t.Errorf("TiePending = true, want false")
	}
	if res.TiebreakNote == "" {
		t.Errorf("expected a non-empty TiebreakNote")
	}
}

func TestComputeResults_MostBackersTiebreakStillTiedDeterministicRand(t *testing.T) {
	// a and b tied in both score (4) and backers (1) -- falls through to
	// random selection. With the same seed, the outcome must be repeatable.
	optionIDs := []string{"a", "b"}
	ballots := map[string]map[string]int{
		"p1": {"a": 4},
		"p2": {"b": 4},
	}

	res1 := ComputeResults(optionIDs, ballots, 1, TiebreakConfig{Tiebreaker: TiebreakMostBackers}, rand.New(rand.NewSource(42)))
	res2 := ComputeResults(optionIDs, ballots, 1, TiebreakConfig{Tiebreaker: TiebreakMostBackers}, rand.New(rand.NewSource(42)))

	if res1.WinnerID == "" {
		t.Fatalf("expected a winner to be chosen")
	}
	if res1.WinnerID != res2.WinnerID {
		t.Errorf("same seed produced different winners: %q vs %q", res1.WinnerID, res2.WinnerID)
	}
	if res1.WinnerID != "a" && res1.WinnerID != "b" {
		t.Errorf("winner %q is not one of the tied candidates", res1.WinnerID)
	}
	if res1.TiebreakNote != "tie broken randomly" {
		t.Errorf("TiebreakNote = %q, want %q", res1.TiebreakNote, "tie broken randomly")
	}

	// Verify the exact seeded choice against an independent rand source
	// running the same selection sequence, to pin down determinism (not
	// just self-consistency).
	ref := rand.New(rand.NewSource(42))
	wantIdx := ref.Intn(2)
	want := []string{"a", "b"}[wantIdx]
	if res1.WinnerID != want {
		t.Errorf("WinnerID = %q, want %q (seeded rand.Intn(2) index %d)", res1.WinnerID, want, wantIdx)
	}
}

func TestComputeResults_RandomTiebreakDeterministic(t *testing.T) {
	optionIDs := []string{"a", "b", "c"}
	ballots := map[string]map[string]int{
		"p1": {"a": 3},
		"p2": {"b": 3},
		"p3": {"c": 1},
	}
	res1 := ComputeResults(optionIDs, ballots, 1, TiebreakConfig{Tiebreaker: TiebreakRandom}, rand.New(rand.NewSource(7)))
	res2 := ComputeResults(optionIDs, ballots, 1, TiebreakConfig{Tiebreaker: TiebreakRandom}, rand.New(rand.NewSource(7)))

	if res1.WinnerID != res2.WinnerID {
		t.Errorf("same seed produced different winners: %q vs %q", res1.WinnerID, res2.WinnerID)
	}
	if res1.WinnerID != "a" && res1.WinnerID != "b" {
		t.Errorf("winner %q is not one of the tied candidates (a,b); c is eliminated", res1.WinnerID)
	}
	if res1.TiebreakNote != "tie broken randomly" {
		t.Errorf("TiebreakNote = %q, want %q", res1.TiebreakNote, "tie broken randomly")
	}

	ref := rand.New(rand.NewSource(7))
	wantIdx := ref.Intn(2)
	want := []string{"a", "b"}[wantIdx]
	if res1.WinnerID != want {
		t.Errorf("WinnerID = %q, want %q (seeded rand.Intn(2) index %d)", res1.WinnerID, want, wantIdx)
	}
}

func TestComputeResults_CreatorTiebreakPending(t *testing.T) {
	optionIDs := []string{"a", "b"}
	ballots := map[string]map[string]int{
		"p1": {"a": 3},
		"p2": {"b": 3},
	}
	res := ComputeResults(optionIDs, ballots, 1, TiebreakConfig{Tiebreaker: TiebreakCreator}, rand.New(rand.NewSource(1)))
	if !res.TiePending {
		t.Errorf("TiePending = false, want true")
	}
	if res.WinnerID != "" {
		t.Errorf("WinnerID = %q, want empty while tie is pending", res.WinnerID)
	}
	want := []string{"a", "b"}
	got := append([]string{}, res.TiedOptionIDs...)
	sort.Strings(got)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("TiedOptionIDs = %v, want %v", res.TiedOptionIDs, want)
	}
	if res.TiebreakNote == "" {
		t.Errorf("expected a non-empty TiebreakNote")
	}
}

func TestComputeResults_ScoresSortedDescStableForTies(t *testing.T) {
	// b has the highest score; a and c are tied and must keep their
	// original creation order (a before c).
	optionIDs := []string{"a", "b", "c"}
	ballots := map[string]map[string]int{
		"p1": {"a": 3, "b": 5, "c": 3},
	}
	res := ComputeResults(optionIDs, ballots, 0, TiebreakConfig{Tiebreaker: TiebreakMostBackers}, rand.New(rand.NewSource(1)))
	if len(res.Scores) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(res.Scores))
	}
	gotOrder := []string{res.Scores[0].OptionID, res.Scores[1].OptionID, res.Scores[2].OptionID}
	wantOrder := []string{"b", "a", "c"}
	if !reflect.DeepEqual(gotOrder, wantOrder) {
		t.Errorf("Scores order = %v, want %v", gotOrder, wantOrder)
	}
}

func TestComputeResults_EliminatedOptionCannotWinDespitePopularity(t *testing.T) {
	// "a" is backed by many participants (most backers) but each gives only
	// a single credit, so its total score falls below the survival
	// threshold and it is eliminated. "b" has fewer, more concentrated
	// backers and clears the threshold, so it must win even though "a" is
	// more broadly popular and has a non-trivial score of its own.
	optionIDs := []string{"a", "b"}
	ballots := map[string]map[string]int{
		"p1": {"a": 1},
		"p2": {"a": 1},
		"p3": {"a": 1},
		"p4": {"a": 1},
		"p5": {"b": 3},
		"p6": {"b": 3},
	}
	res := ComputeResults(optionIDs, ballots, 6, TiebreakConfig{Tiebreaker: TiebreakMostBackers}, rand.New(rand.NewSource(1)))
	a := scoreOf(t, res, "a")
	b := scoreOf(t, res, "b")
	if a.Score != 4 || a.Backers != 4 {
		t.Fatalf("a score/backers = %d/%d, want 4/4", a.Score, a.Backers)
	}
	if b.Score != 6 || b.Backers != 2 {
		t.Fatalf("b score/backers = %d/%d, want 6/2", b.Score, b.Backers)
	}
	if !a.Eliminated {
		t.Errorf("a eliminated = false, want true (score 4 < threshold 6)")
	}
	if b.Eliminated {
		t.Errorf("b eliminated = true, want false (score 6 >= threshold 6)")
	}
	if res.WinnerID != "b" {
		t.Errorf("WinnerID = %q, want %q (eliminated option must not win despite more backers)", res.WinnerID, "b")
	}
}

func TestComputeResults_VetoEliminatesRegardlessOfScore(t *testing.T) {
	// "a" is vetoed by one participant but still gets a healthy score from
	// others; it must be eliminated (and lose) anyway. "b" has a lower raw
	// score but no veto, so it wins.
	optionIDs := []string{"a", "b"}
	ballots := map[string]map[string]int{
		"p1": {"a": -1},
		"p2": {"a": 5},
		"p3": {"b": 2},
	}
	res := ComputeResults(optionIDs, ballots, 0, TiebreakConfig{Tiebreaker: TiebreakMostBackers}, rand.New(rand.NewSource(1)))
	a := scoreOf(t, res, "a")
	if a.VetoCount != 1 {
		t.Errorf("a.VetoCount = %d, want 1", a.VetoCount)
	}
	if a.Score != 5 {
		t.Errorf("a.Score = %d, want 5 (other voter's positive votes still count)", a.Score)
	}
	if !a.Eliminated {
		t.Errorf("a.Eliminated = false, want true (any veto is fatal)")
	}
	if res.WinnerID != "b" {
		t.Errorf("WinnerID = %q, want %q (vetoed option must never win)", res.WinnerID, "b")
	}
}

func TestComputeResults_VetoCountZeroWhenNoVetoes(t *testing.T) {
	optionIDs := []string{"a"}
	ballots := map[string]map[string]int{"p1": {"a": 3}}
	res := ComputeResults(optionIDs, ballots, 0, TiebreakConfig{Tiebreaker: TiebreakMostBackers}, rand.New(rand.NewSource(1)))
	if got := scoreOf(t, res, "a").VetoCount; got != 0 {
		t.Errorf("VetoCount = %d, want 0", got)
	}
}

func TestComputeResults_EarliestTiebreak(t *testing.T) {
	// a, b, c tied at score 3; "a" was suggested first (creation order) and
	// must win.
	optionIDs := []string{"a", "b", "c"}
	ballots := map[string]map[string]int{
		"p1": {"a": 3},
		"p2": {"b": 3},
		"p3": {"c": 3},
	}
	res := ComputeResults(optionIDs, ballots, 0, TiebreakConfig{Tiebreaker: TiebreakEarliest}, rand.New(rand.NewSource(1)))
	if res.WinnerID != "a" {
		t.Errorf("WinnerID = %q, want %q (earliest suggested)", res.WinnerID, "a")
	}
	if res.TiebreakNote != "tie broken by earliest suggestion" {
		t.Errorf("TiebreakNote = %q, want %q", res.TiebreakNote, "tie broken by earliest suggestion")
	}
}

func TestComputeResults_RunoffPendingOnFreshTie(t *testing.T) {
	optionIDs := []string{"a", "b", "c"}
	ballots := map[string]map[string]int{
		"p1": {"a": 3},
		"p2": {"b": 3},
		"p3": {"c": 1},
	}
	cfg := TiebreakConfig{Tiebreaker: TiebreakRunoff, RunoffFallback: TiebreakRandom, InRunoff: false}
	res := ComputeResults(optionIDs, ballots, 0, cfg, rand.New(rand.NewSource(1)))
	if !res.RunoffPending {
		t.Errorf("RunoffPending = false, want true")
	}
	if res.WinnerID != "" {
		t.Errorf("WinnerID = %q, want empty while a runoff is pending", res.WinnerID)
	}
	if res.TiePending {
		t.Errorf("TiePending = true, want false (runoffPending is a distinct state)")
	}
	want := []string{"a", "b"}
	got := append([]string{}, res.TiedOptionIDs...)
	sort.Strings(got)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("TiedOptionIDs = %v, want %v", res.TiedOptionIDs, want)
	}
}

func TestComputeResults_RunoffNoTieIsUnaffected(t *testing.T) {
	// A clear winner under tiebreaker "runoff" must not trigger a runoff.
	optionIDs := []string{"a", "b"}
	ballots := map[string]map[string]int{"p1": {"a": 5, "b": 1}}
	cfg := TiebreakConfig{Tiebreaker: TiebreakRunoff, RunoffFallback: TiebreakRandom}
	res := ComputeResults(optionIDs, ballots, 0, cfg, rand.New(rand.NewSource(1)))
	if res.RunoffPending {
		t.Errorf("RunoffPending = true, want false (no tie)")
	}
	if res.WinnerID != "a" {
		t.Errorf("WinnerID = %q, want %q", res.WinnerID, "a")
	}
}

func TestComputeResults_RunoffFallbackWhenTiedAgain(t *testing.T) {
	// InRunoff true simulates scoring the runoff round itself: a tie here
	// must resolve immediately via runoffFallback instead of pending again.
	optionIDs := []string{"a", "b"}
	ballots := map[string]map[string]int{
		"p1": {"a": 3},
		"p2": {"b": 3},
	}
	cfg := TiebreakConfig{Tiebreaker: TiebreakRunoff, RunoffFallback: TiebreakEarliest, InRunoff: true}
	res := ComputeResults(optionIDs, ballots, 0, cfg, rand.New(rand.NewSource(1)))
	if res.RunoffPending {
		t.Errorf("RunoffPending = true, want false (already in a runoff)")
	}
	if res.WinnerID != "a" {
		t.Errorf("WinnerID = %q, want %q (earliest fallback)", res.WinnerID, "a")
	}
	if res.TiebreakNote != "tie broken after runoff by earliest suggestion" {
		t.Errorf("TiebreakNote = %q, want distinguishable runoff-fallback note, got %q", res.TiebreakNote, res.TiebreakNote)
	}
}

func TestComputeResults_RunoffFallbackToCreatorStillPending(t *testing.T) {
	optionIDs := []string{"a", "b"}
	ballots := map[string]map[string]int{
		"p1": {"a": 3},
		"p2": {"b": 3},
	}
	cfg := TiebreakConfig{Tiebreaker: TiebreakRunoff, RunoffFallback: TiebreakCreator, InRunoff: true}
	res := ComputeResults(optionIDs, ballots, 0, cfg, rand.New(rand.NewSource(1)))
	if res.RunoffPending {
		t.Errorf("RunoffPending = true, want false (creator fallback uses TiePending, not runoffPending)")
	}
	if !res.TiePending {
		t.Errorf("TiePending = false, want true (creator fallback flow)")
	}
	if res.WinnerID != "" {
		t.Errorf("WinnerID = %q, want empty while tie is pending", res.WinnerID)
	}
	want := []string{"a", "b"}
	got := append([]string{}, res.TiedOptionIDs...)
	sort.Strings(got)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("TiedOptionIDs = %v, want %v", res.TiedOptionIDs, want)
	}
	if res.TiebreakNote == "" {
		t.Errorf("expected a non-empty TiebreakNote")
	}
	if !res.AfterRunoff {
		t.Errorf("AfterRunoff = false, want true (tie pended after an automatic runoff round)")
	}
}
