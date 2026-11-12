//go:build integration

// Package racetest releases N callers of one operation at the same moment,
// repeats that for a number of rounds, and lets the test check an invariant
// after each round. Every round also reports whether the calls really ran at
// the same time (their time spans share a point), so a sweep cannot pass by
// running the callers one after the other.
//
// Smoke mode runs a few rounds with the normal integration tests. Deep mode
// (RACE_SWEEP=1, for the nightly job) runs many more; RACE_ROUNDS overrides
// the deep count.
package racetest

import (
	"os"
	"strconv"
	"sync"
	"testing"
	"time"
)

// Deep reports whether the deep sweep was asked for.
func Deep() bool { return os.Getenv("RACE_SWEEP") == "1" }

// Rounds is smoke normally and deep (or RACE_ROUNDS) in the deep sweep.
func Rounds(smoke, deep int) int {
	if !Deep() {
		return smoke
	}
	if n, err := strconv.Atoi(os.Getenv("RACE_ROUNDS")); err == nil && n > 0 {
		return n
	}
	return deep
}

// Round is the outcome of one release of N callers.
type Round struct {
	Errs  []error
	spans [][2]time.Time
}

// Overlapped reports whether every call was running at one common moment.
func (r Round) Overlapped() bool {
	if len(r.spans) < 2 {
		return false
	}
	latestStart, earliestEnd := r.spans[0][0], r.spans[0][1]
	for _, s := range r.spans[1:] {
		if s[0].After(latestStart) {
			latestStart = s[0]
		}
		if s[1].Before(earliestEnd) {
			earliestEnd = s[1]
		}
	}
	return latestStart.Before(earliestEnd)
}

// Succeeded counts the calls that returned no error.
func (r Round) Succeeded() int {
	n := 0
	for _, err := range r.Errs {
		if err == nil {
			n++
		}
	}
	return n
}

// Together starts n goroutines, waits until all are ready, releases them at
// once and returns when every fn has returned.
func Together(n int, fn func(i int) error) Round {
	r := Round{Errs: make([]error, n), spans: make([][2]time.Time, n)}
	var ready, done sync.WaitGroup
	start := make(chan struct{})
	ready.Add(n)
	done.Add(n)
	for i := range n {
		go func() {
			defer done.Done()
			ready.Done()
			<-start
			r.spans[i][0] = time.Now()
			r.Errs[i] = fn(i)
			r.spans[i][1] = time.Now()
		}()
	}
	ready.Wait()
	close(start)
	done.Wait()
	return r
}

// Sweep runs rounds of Together. Before each round setup prepares the state
// (it may be nil); after it check returns a violation text or "". Sweep
// fails the test when fewer than half of the rounds overlapped, logs the
// counts, and returns the violations it saw (at most the first five texts are
// kept, all are counted).
func Sweep(t *testing.T, rounds, n int, setup func(round int), call func(round, i int) error, check func(round int, r Round) string) Result {
	t.Helper()
	var res Result
	for round := range rounds {
		if setup != nil {
			setup(round)
		}
		r := Together(n, func(i int) error { return call(round, i) })
		res.Rounds++
		if r.Overlapped() {
			res.Overlapped++
		}
		if v := check(round, r); v != "" {
			res.Violations++
			if len(res.Examples) < 5 {
				res.Examples = append(res.Examples, v)
			}
		}
	}
	t.Logf("race sweep: %d rounds of %d callers, %d overlapped, %d violated", res.Rounds, n, res.Overlapped, res.Violations)
	if res.Overlapped*2 < res.Rounds {
		t.Fatalf("only %d of %d rounds ran their callers at the same time: the sweep did not race", res.Overlapped, res.Rounds)
	}
	return res
}

// FailOnViolation fails the test when any round broke the invariant.
func (r Result) FailOnViolation(t *testing.T) {
	t.Helper()
	if r.Violations > 0 {
		t.Errorf("%d of %d rounds broke the invariant: %v", r.Violations, r.Rounds, r.Examples)
	}
}

// KnownFailure is the inverse assertion of a recorded finding: the sweep must
// still break the invariant, and the test turns red on the day it no longer
// does, so the entry has to be removed together with the fix.
func (r Result) KnownFailure(t *testing.T, finding string) {
	t.Helper()
	if r.Violations == 0 {
		t.Errorf("known failure %s no longer reproduces in %d rounds: remove it from the list", finding, r.Rounds)
		return
	}
	t.Logf("known failure %s still reproduces: %d of %d rounds, e.g. %v", finding, r.Violations, r.Rounds, r.Examples[0])
}

// Result counts what a sweep saw.
type Result struct {
	Rounds, Overlapped, Violations int
	Examples                       []string
}
