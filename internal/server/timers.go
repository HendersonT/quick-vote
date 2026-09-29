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
}

// NewScheduler returns a Scheduler that calls fire(slug) when a slug's armed
// deadline elapses on clock c.
func NewScheduler(c clock.Clock, fire func(slug string)) *Scheduler {
	return &Scheduler{clock: c, timers: map[string]clock.Timer{}, fire: fire}
}

// Set arms (or re-arms) the deadline for slug. A deadline at or before now
// fires as soon as possible.
func (sc *Scheduler) Set(slug string, at time.Time) {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	if t, ok := sc.timers[slug]; ok {
		t.Stop()
		delete(sc.timers, slug)
	}
	d := at.Sub(sc.clock.Now())
	if d < 0 {
		d = 0
	}
	sc.timers[slug] = sc.clock.AfterFunc(d, func() {
		sc.mu.Lock()
		delete(sc.timers, slug)
		sc.mu.Unlock()
		if sc.fire != nil {
			sc.fire(slug)
		}
	})
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

// timerFired is the Scheduler callback: it advances the phase for slug. If the
// suggestion timer expires while there are fewer than two options, the phase
// is held and the deadline is dropped (the creator must resolve it manually).
func (s *Server) timerFired(slug string) {
	defer s.lockSlug(slug)()
	err := s.advancePhase(slug, false, "")
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
