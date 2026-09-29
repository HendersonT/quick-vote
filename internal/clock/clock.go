// Package clock abstracts time so timer-driven behavior (phase deadlines,
// pruning, last-activity stamps) can be tested deterministically.
package clock

import "time"

// Clock is the subset of the time package the server depends on.
type Clock interface {
	Now() time.Time
	AfterFunc(d time.Duration, f func()) Timer
}

// Timer is a cancellable pending callback.
type Timer interface {
	// Stop prevents the callback from running, reporting whether it was
	// still pending.
	Stop() bool
}

type realClock struct{}

// Real returns a Clock backed by the time package.
func Real() Clock { return realClock{} }

func (realClock) Now() time.Time { return time.Now() }

func (realClock) AfterFunc(d time.Duration, f func()) Timer { return time.AfterFunc(d, f) }
