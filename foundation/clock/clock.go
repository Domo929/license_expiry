// Package clock tells the time.
//
// Real reads the system clock. Simulated is set by hand, for tests and demos,
// so code that takes its time from a clock can run through weeks in a moment.
package clock

import (
	"sync"
	"time"
)

// Real is the system clock.
type Real struct{}

// Now returns the current time, in the local time zone.
func (Real) Now() time.Time {
	return time.Now()
}

// Simulated is a clock that moves only when it is told to. It is safe for concurrent use.
type Simulated struct {
	mu  sync.Mutex
	now time.Time
}

// NewSimulated returns a clock that shows now.
func NewSimulated(now time.Time) *Simulated {
	return &Simulated{now: now}
}

// Now returns the time that the clock shows.
func (s *Simulated) Now() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.now
}

// Set moves the clock to now. It can move the clock back.
func (s *Simulated) Set(now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.now = now
}

// AddDays moves the clock n calendar days ahead, or back if n is negative.
// The wall-clock time stays the same in the time zone of the clock.
// A day with a daylight saving change is 23 or 25 hours long, so the clock moves by that many hours.
func (s *Simulated) AddDays(n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.now = s.now.AddDate(0, 0, n)
}
