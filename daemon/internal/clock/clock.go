// Package clock holds the timer seam: real timers in production, manual timers in tests.
package clock

import "time"

// Clock makes timers. Tests replace it to control time.
type Clock interface {
	After(d time.Duration) <-chan time.Time
}

type realClock struct{}

func (realClock) After(d time.Duration) <-chan time.Time { return time.After(d) }

// Real is the wall clock.
var Real Clock = realClock{}

// Manual is a Clock for tests. A timer fires only when the test calls Fire.
type Manual struct{ timers chan chan time.Time }

func NewManual() *Manual { return &Manual{timers: make(chan chan time.Time, 8)} }

func (c *Manual) After(time.Duration) <-chan time.Time {
	t := make(chan time.Time, 1)
	c.timers <- t
	return t
}

// Fire fires the oldest timer. It waits until a call has set one.
func (c *Manual) Fire() { (<-c.timers) <- time.Time{} }

// WaitTimer waits until a call has set a timer, and leaves the timer unfired.
func (c *Manual) WaitTimer() { <-c.timers }
