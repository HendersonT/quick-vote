package clock

import (
	"sync"
	"time"
)

// Fake is a manually advanced Clock. Timers fire synchronously, in deadline
// order, on the goroutine calling Advance — never on their own — so tests
// observe phase transitions without sleeping.
type Fake struct {
	mu     sync.Mutex
	now    time.Time
	seq    int
	timers []*fakeTimer // pending only; fired/stopped timers are removed
}

type fakeTimer struct {
	c       *Fake
	at      time.Time
	seq     int
	f       func()
	stopped bool
	fired   bool
}

// NewFake returns a Fake clock reading start.
func NewFake(start time.Time) *Fake { return &Fake{now: start} }

// Now returns the fake current time.
func (c *Fake) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// AfterFunc schedules f to run once the clock has been advanced by at least
// d. A non-positive d fires on the next Advance, including Advance(0).
func (c *Fake) AfterFunc(d time.Duration, f func()) Timer {
	c.mu.Lock()
	defer c.mu.Unlock()
	if d < 0 {
		d = 0
	}
	c.seq++
	t := &fakeTimer{c: c, at: c.now.Add(d), seq: c.seq, f: f}
	c.timers = append(c.timers, t)
	return t
}

// Advance moves the clock forward by d, firing every timer due at or before
// the new time (including timers scheduled by callbacks during the advance)
// in (deadline, creation) order. Now reads each timer's deadline while its
// callback runs. Callbacks run without the clock's lock held, so they may
// call back into the clock.
func (c *Fake) Advance(d time.Duration) {
	c.mu.Lock()
	target := c.now.Add(d)
	c.mu.Unlock()
	for {
		c.mu.Lock()
		due := -1
		for i, t := range c.timers {
			if t.at.After(target) {
				continue
			}
			if due < 0 || t.at.Before(c.timers[due].at) ||
				(t.at.Equal(c.timers[due].at) && t.seq < c.timers[due].seq) {
				due = i
			}
		}
		if due < 0 {
			c.now = target
			c.mu.Unlock()
			return
		}
		t := c.timers[due]
		c.timers = append(c.timers[:due], c.timers[due+1:]...)
		if t.at.After(c.now) {
			c.now = t.at
		}
		t.fired = true
		c.mu.Unlock()
		t.f()
	}
}

// Stop cancels a pending timer, reporting whether it had not yet fired or
// been stopped.
func (t *fakeTimer) Stop() bool {
	c := t.c
	c.mu.Lock()
	defer c.mu.Unlock()
	if t.stopped || t.fired {
		return false
	}
	t.stopped = true
	for i, p := range c.timers {
		if p == t {
			c.timers = append(c.timers[:i], c.timers[i+1:]...)
			break
		}
	}
	return true
}
