package main

import (
	"os"
	"time"

	"github.com/dimonomid/clock"
	"github.com/juju/errors"
)

const e2eTestNowEnv = "NERDLOG_E2E_TEST_NOW"

// newAppClock returns the application's source of wall-clock time.
//
// Normally it returns an unmodified real clock. End-to-end tests can set
// NERDLOG_E2E_TEST_NOW to an RFC3339 timestamp to move the clock's initial
// wall time to a stable date. The returned clock is shifted rather than frozen:
// if one real second passes, its reported time also advances by one second.
// This keeps asynchronous application behavior realistic while making calendar
// decisions, such as inferring the year of a syslog timestamp, reproducible.
func newAppClock() (clock.Clock, error) {
	realClock := clock.New()
	wantNowStr := os.Getenv(e2eTestNowEnv)
	if wantNowStr == "" {
		return realClock, nil
	}

	wantNow, err := time.Parse(time.RFC3339, wantNowStr)
	if err != nil {
		return nil, errors.Annotatef(err, "parsing %s as RFC3339", e2eTestNowEnv)
	}

	return &shiftedClock{
		Clock:  realClock,
		offset: wantNow.Sub(realClock.Now()),
	}, nil
}

// shiftedClock presents another clock in a different wall-clock time frame.
// It reports the wrapped clock's time plus a constant offset while delegating
// timers, sleeps, ticks, and other scheduling operations to the wrapped clock.
// Consequently, time progresses at the normal rate and timer behavior is not
// affected by what may be a large shift in the displayed calendar date.
//
// This differs deliberately from a mock or frozen clock. A frozen clock would
// require the E2E harness to advance every timer manually and could deadlock
// connection timeouts, UI refreshes, or other background work. shiftedClock is
// intended for tests which need a stable calendar reference but still exercise
// the application using real elapsed time.
//
// offset is immutable after construction, so shiftedClock has the same
// concurrency properties as the wrapped clock.
type shiftedClock struct {
	clock.Clock
	offset time.Duration
}

// Now returns the wrapped clock's current time translated by offset.
func (c *shiftedClock) Now() time.Time {
	return c.Clock.Now().Add(c.offset)
}

// Since returns elapsed time in the shifted clock's time frame.
//
// It must not delegate to the wrapped clock's Since method: t will normally
// have come from shiftedClock.Now, while the wrapped clock operates in the
// unshifted time frame. Mixing those values would add the wall-clock offset to
// every measured duration (for example, turning a millisecond query into a
// duration of many months).
func (c *shiftedClock) Since(t time.Time) time.Duration {
	return c.Now().Sub(t)
}
