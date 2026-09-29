package server

import (
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/HendersonT/quick-vote/internal/clock"
	"github.com/HendersonT/quick-vote/internal/store"
)

// timedSettings: manual advances, a 60 s suggest timer and a 600 s vote timer.
const timedSettings = `{"creditsPerOption":3,"maxSuggestionsPerUser":3,"suggestAdvanceMode":"manual",` +
	`"voteAdvanceMode":"manual","tiebreaker":"most-backers","revoteThresholdPct":33,` +
	`"suggestTimerSecs":60,"voteTimerSecs":600}`

// newInternalFakeServer returns a fake-clock server plus its store, for tests
// that need to reach unexported timer and scheduler internals.
func newInternalFakeServer(t *testing.T) (*Server, *clock.Fake, *store.Store) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	fc := clock.NewFake(time.Unix(1_700_000_000, 0))
	return NewWithConfig(st, nil, Config{Clock: fc}), fc, st
}

// seedTimedVote stores suggest-phase vote "r" with two options and the given
// stored deadline (not armed in the scheduler).
func seedTimedVote(t *testing.T, st *store.Store, now time.Time, deadline *int64) {
	t.Helper()
	if err := st.CreateVote(store.VoteRow{Slug: "r", Title: "R", Phase: "suggesting", Settings: timedSettings,
		CreatorToken: "ct", PhaseDeadline: deadline, CreatedAt: now.Unix()}); err != nil {
		t.Fatal(err)
	}
	if err := st.AddParticipant(store.ParticipantRow{ID: "p1", VoteSlug: "r", Name: "Ann", Token: "tok",
		IsCreator: true, JoinedAt: now.Unix()}); err != nil {
		t.Fatal(err)
	}
	for i, id := range []string{"o1", "o2"} {
		if err := st.AddOption(store.OptionRow{ID: id, VoteSlug: "r", ParticipantID: "p1", Title: id,
			CreatedAt: now.Unix() + int64(i)}); err != nil {
			t.Fatal(err)
		}
	}
}

// TestStaleTimerCallbackDoesNotEndNewPhase reproduces the lock-wait race: the
// suggest timer fires while a handler holds the slug lock and advances into a
// timed voting phase. The suggest callback, already dispatched and waiting on
// the lock, must not then end the brand-new voting phase.
func TestStaleTimerCallbackDoesNotEndNewPhase(t *testing.T) {
	s, fc, st := newInternalFakeServer(t)
	entered := make(chan struct{})
	var once sync.Once
	s.scheduler = NewScheduler(fc, func(slug string) {
		once.Do(func() { close(entered) })
		s.timerFired(slug)
	})
	suggestDeadline := fc.Now().Add(60 * time.Second).Unix()
	seedTimedVote(t, st, fc.Now(), &suggestDeadline)
	s.armOrClear("r", &suggestDeadline)

	unlock := s.lockSlug("r")
	advanced := make(chan struct{})
	go func() {
		fc.Advance(2 * time.Minute)
		close(advanced)
	}()
	// The suggest timer has fired; its callback is now blocked on the lock.
	<-entered
	err := s.advancePhase("r", true, "")
	unlock()
	if err != nil {
		t.Fatalf("advance into voting: %v", err)
	}
	<-advanced

	v, err := st.GetVote("r")
	if err != nil {
		t.Fatal(err)
	}
	// The fake clock reads the suggest deadline while its callback runs, so
	// the manual advance set the vote deadline 600 s after it.
	want := suggestDeadline + 600
	if v.Phase != "voting" || v.PhaseDeadline == nil || *v.PhaseDeadline != want {
		t.Fatalf("phase=%s deadline=%v, want voting with deadline %d", v.Phase, v.PhaseDeadline, want)
	}
	// The new voting deadline is still armed and ends the phase on time.
	fc.Advance(10 * time.Minute)
	if v, _ := st.GetVote("r"); v.Phase != "results" {
		t.Fatalf("phase=%s after the vote deadline, want results", v.Phase)
	}
}

// TestTimerFiredSkipsClosedVote pins the backstop for a callback that was
// already waiting on the slug lock when close cleared the scheduler: the
// vote is closed (even if a deadline were somehow still stored), so the
// callback must leave it alone.
func TestTimerFiredSkipsClosedVote(t *testing.T) {
	s, fc, st := newInternalFakeServer(t)
	due := fc.Now().Unix()
	seedTimedVote(t, st, fc.Now(), &due)
	closedAt := fc.Now().Unix()
	if err := st.SetClosed("r", &closedAt); err != nil {
		t.Fatal(err)
	}

	s.timerFired("r")

	v, err := st.GetVote("r")
	if err != nil {
		t.Fatal(err)
	}
	if v.Phase != "suggesting" || v.PhaseDeadline == nil || *v.PhaseDeadline != due {
		t.Fatalf("closed vote changed: phase=%s deadline=%v", v.Phase, v.PhaseDeadline)
	}
}

// TestTimerFiredAfterCloseIsNoop: a callback that outlives Server.Close (it
// was already dispatched when StopAll ran) must not touch a store the caller
// is about to close.
func TestTimerFiredAfterCloseIsNoop(t *testing.T) {
	s, fc, st := newInternalFakeServer(t)
	due := fc.Now().Unix()
	seedTimedVote(t, st, fc.Now(), &due)
	s.Close()

	s.timerFired("r")

	if v, _ := st.GetVote("r"); v.Phase != "suggesting" {
		t.Fatalf("phase=%s, want suggesting: timer fired after Close advanced the vote", v.Phase)
	}
}

// manualClock hands timer callbacks to the test to run whenever it likes, so
// a callback can be delivered after a newer Set — the interleaving the real
// clock produces when a timer fires just as it is being replaced.
type manualClock struct{ timers []*manualTimer }

type manualTimer struct {
	f              func()
	fired, stopped bool
}

func (m *manualClock) Now() time.Time { return time.Unix(0, 0) }

func (m *manualClock) AfterFunc(_ time.Duration, f func()) clock.Timer {
	t := &manualTimer{f: f}
	m.timers = append(m.timers, t)
	return t
}

func (t *manualTimer) Stop() bool {
	if t.fired || t.stopped {
		return false
	}
	t.stopped = true
	return true
}

func TestSchedulerStaleCallbackKeepsNewerTimer(t *testing.T) {
	mc := &manualClock{}
	sc := NewScheduler(mc, func(string) {})
	sc.Set("x", time.Unix(10, 0))
	first := mc.timers[0]
	first.fired = true // dispatched by the clock; its callback hasn't run yet
	sc.Set("x", time.Unix(20, 0))
	second := mc.timers[1]

	first.f() // the stale callback finally runs
	sc.Clear("x")

	if !second.stopped {
		t.Fatal("Clear must still cancel the newer timer after a stale callback ran")
	}
}

func TestSchedulerSetAfterStopAllIsNoop(t *testing.T) {
	mc := &manualClock{}
	sc := NewScheduler(mc, func(string) {})
	sc.StopAll()
	sc.Set("x", time.Unix(10, 0))
	if len(mc.timers) != 0 {
		t.Fatalf("Set after StopAll armed %d timer(s), want none", len(mc.timers))
	}
}
