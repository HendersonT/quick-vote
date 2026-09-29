package server

import (
	"errors"
	"sync"
	"time"

	"github.com/HendersonT/quick-vote/internal/clock"
)

// Scheduler arms at most one deadline timer per vote slug. Setting a deadline
// for a slug replaces any existing timer; clearing cancels it. When a slug's
// deadline fires, the configured fire callback is invoked with the slug.
type Scheduler struct {
	mu     sync.Mutex
	clock  clock.Clock
	timers map[string]clock.Timer
	fire   func(slug string)
	// stopped is set by StopAll: at shutdown nothing may arm a new timer
	// (e.g. a handler or callback still finishing) that would outlive the
	// server and fire against a closed store.
	stopped bool
}

// NewScheduler returns a Scheduler that calls fire(slug) when a slug's armed
// deadline elapses on clock c.
func NewScheduler(c clock.Clock, fire func(slug string)) *Scheduler {
	return &Scheduler{clock: c, timers: map[string]clock.Timer{}, fire: fire}
}

// Set arms (or re-arms) the deadline for slug. A deadline at or before now
// fires as soon as possible. After StopAll it does nothing.
func (sc *Scheduler) Set(slug string, at time.Time) {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	if sc.stopped {
		return
	}
	if t, ok := sc.timers[slug]; ok {
		t.Stop()
		delete(sc.timers, slug)
	}
	d := at.Sub(sc.clock.Now())
	if d < 0 {
		d = 0
	}
	// t is assigned while sc.mu is held and the callback takes sc.mu before
	// reading it, so the callback always sees its own timer.
	var t clock.Timer
	t = sc.clock.AfterFunc(d, func() {
		sc.mu.Lock()
		// A timer that fired just as Set replaced it (Stop came too late)
		// must not delete the replacement's entry, or a later Clear
		// couldn't cancel the replacement.
		if sc.timers[slug] == t {
			delete(sc.timers, slug)
		}
		sc.mu.Unlock()
		if sc.fire != nil {
			sc.fire(slug)
		}
	})
	sc.timers[slug] = t
}

// Clear cancels any armed deadline for slug.
func (sc *Scheduler) Clear(slug string) {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	if t, ok := sc.timers[slug]; ok {
		t.Stop()
		delete(sc.timers, slug)
	}
}

// StopAll cancels and forgets every armed deadline and makes later Sets
// no-ops. Used at shutdown; the deadlines stay persisted and RearmTimers
// restores them on the next start.
func (sc *Scheduler) StopAll() {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	sc.stopped = true
	for slug, t := range sc.timers {
		t.Stop()
		delete(sc.timers, slug)
	}
}

// timerFired is the Scheduler callback: it advances the phase for slug. If the
// suggestion timer expires while there are fewer than two options, the phase
// is held and the deadline is dropped (the creator must resolve it manually).
//
// A callback can be dispatched and then wait on the slug lock while a handler
// changes the room, so by the time it runs it may be stale. It therefore
// re-checks what the room looks like now and leaves it untouched (no
// broadcast) when:
//   - the server is shutting down (the store may be closing under it);
//   - the vote was closed, or its stored deadline is gone (close cleared it);
//   - the stored deadline is still in the future: a handler moved the room
//     into a new timed phase (manual advance into voting, an all-voted
//     runoff), and this callback belongs to the phase that just ended.
func (s *Server) timerFired(slug string) {
	defer s.lockSlug(slug)()
	select {
	case <-s.stop:
		return
	default:
	}
	v, err := s.store.GetVote(slug)
	if err != nil || v.ClosedAt != nil || v.PhaseDeadline == nil {
		return
	}
	if s.now().Unix() < *v.PhaseDeadline {
		// Re-arm rather than trust that a timer is pending: normally the
		// handler already armed an identical one (this just replaces it),
		// but if the wall clock stepped back since arming, this is what
		// keeps the deadline from being lost until the next restart.
		s.armOrClear(slug, v.PhaseDeadline)
		return
	}
	err = s.advancePhase(slug, false, "")
	if errors.Is(err, errNeedTwoSuggestions) {
		s.clearDeadline(slug)
	}
	s.changed(slug)
}

// clearDeadline drops a vote's phase deadline in the store and the scheduler
// without changing its phase.
func (s *Server) clearDeadline(slug string) {
	v, err := s.store.GetVote(slug)
	if err != nil {
		return
	}
	v.PhaseDeadline = nil
	_ = s.store.UpdateVote(v)
	if s.scheduler != nil {
		s.scheduler.Clear(slug)
	}
}

// RearmTimers reloads every persisted phase deadline from the store and arms
// the scheduler for it, so in-flight suggestion/vote timers survive a server
// restart. Deadlines already in the past fire immediately (the Scheduler
// clamps a negative duration to zero), matching the live-timer semantics.
// Call once at startup, before serving.
func (s *Server) RearmTimers() error {
	if s.scheduler == nil {
		return nil
	}
	deadlines, err := s.store.ActiveDeadlines()
	if err != nil {
		return err
	}
	for slug, at := range deadlines {
		s.scheduler.Set(slug, time.Unix(at, 0))
	}
	return nil
}

// armOrClear arms the scheduler for slug when deadline is non-nil, or clears
// it otherwise.
func (s *Server) armOrClear(slug string, deadline *int64) {
	if s.scheduler == nil {
		return
	}
	if deadline != nil {
		s.scheduler.Set(slug, time.Unix(*deadline, 0))
	} else {
		s.scheduler.Clear(slug)
	}
}
