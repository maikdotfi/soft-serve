// Package realclock is a ci.Clock backed by time.Now.
package realclock

import "time"

// Clock implements ci.Clock against the real wall clock.
type Clock struct{}

// New constructs a real-clock adapter.
func New() Clock {
	return Clock{}
}

// Now returns the current wall-clock time.
func (Clock) Now() time.Time {
	return time.Now()
}
