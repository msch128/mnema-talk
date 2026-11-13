package safego

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/msch128/mnema-talk/internal/metrics"
)

// captureLog collects what the package logs while the test runs. Every site
// starts with a full log budget: the budget is the package's and lasts a
// minute, so a second run in the same process (go test -count) would find it
// used up by the first.
func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	RefillLogBudget()
	t.Cleanup(RefillLogBudget)
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

func TestRunRecoversLogsCountsAndCallsOnPanic(t *testing.T) {
	logged := captureLog(t)
	before := metrics.GoroutinePanics.With("test_run").Load()
	closed := false

	Run("test_run", func() { closed = true }, func() { panic("boom") })

	if !closed {
		t.Fatal("onPanic did not run")
	}
	if got := metrics.GoroutinePanics.With("test_run").Load() - before; got != 1 {
		t.Fatalf("panic counted %d times, want 1", got)
	}
	for _, want := range []string{"goroutine panicked", "name=test_run", "boom", "safego_test.go"} {
		if !strings.Contains(logged.String(), want) {
			t.Errorf("log line lacks %q: %s", want, logged.String())
		}
	}
}

func TestRunWithoutPanicCallsNothing(t *testing.T) {
	before := metrics.GoroutinePanics.With("test_quiet").Load()
	ran := false

	Run("test_quiet", func() { t.Error("onPanic ran without a panic") }, func() { ran = true })

	if !ran {
		t.Fatal("fn did not run")
	}
	if got := metrics.GoroutinePanics.With("test_quiet").Load() - before; got != 0 {
		t.Fatalf("counted %d panics, want 0", got)
	}
}

func TestCrashLetsThePanicContinueAfterLogAndCount(t *testing.T) {
	logged := captureLog(t)
	before := metrics.GoroutinePanics.With("test_crash").Load()

	var got any
	func() {
		defer func() { got = recover() }()
		Run("test_crash", Crash, func() { panic("fatal") })
	}()

	if got != "fatal" {
		t.Fatalf("panic value %v, want the original one", got)
	}
	if n := metrics.GoroutinePanics.With("test_crash").Load() - before; n != 1 {
		t.Fatalf("panic counted %d times, want 1", n)
	}
	if !strings.Contains(logged.String(), "name=test_crash") {
		t.Errorf("the panic was not logged before it continued: %s", logged.String())
	}
}

func TestPanicInOnPanicIsNotRecovered(t *testing.T) {
	captureLog(t)
	var got any
	func() {
		defer func() { got = recover() }()
		Run("test_cleanup", func() { panic("cleanup broke") }, func() { panic("first") })
	}()
	if got != "cleanup broke" {
		t.Fatalf("recovered %v, want the clean-up's own panic", got)
	}
}

func TestGoRunsOnAnotherGoroutineAndRecovers(t *testing.T) {
	captureLog(t)
	done := make(chan struct{})
	Go("test_go", func() { close(done) }, func() { panic("boom") })
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("onPanic never ran")
	}
}

// TestGoWaitedReleasesTheGroupOnlyAfterThePanicIsHandled holds onPanic open
// and checks that the wait group is still held then: a waiter that wakes
// finds the panic counted and its clean-up done.
func TestGoWaitedReleasesTheGroupOnlyAfterThePanicIsHandled(t *testing.T) {
	captureLog(t)
	before := metrics.GoroutinePanics.With("test_go_waited").Load()
	var wg sync.WaitGroup
	inOnPanic, release := make(chan struct{}), make(chan struct{})
	GoWaited(&wg, "test_go_waited", func() { close(inOnPanic); <-release }, func() { panic("boom") })
	<-inOnPanic
	waited := make(chan struct{})
	go func() { wg.Wait(); close(waited) }()
	select {
	case <-waited:
		t.Fatal("the wait group was released while onPanic still ran")
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	<-waited
	if got := metrics.GoroutinePanics.With("test_go_waited").Load() - before; got != 1 {
		t.Errorf("panics counted: %d, want 1", got)
	}
}

func TestAfterFuncRecoversAndCanBeStopped(t *testing.T) {
	captureLog(t)
	done := make(chan struct{})
	AfterFunc("test_timer", time.Millisecond, func() { close(done) }, func() { panic("boom") })
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("onPanic never ran")
	}

	fired := make(chan struct{})
	timer := AfterFunc("test_timer", time.Hour, Abandon, func() { close(fired) })
	if !timer.Stop() {
		t.Fatal("a pending timer could not be stopped")
	}
	select {
	case <-fired:
		t.Fatal("a stopped timer fired")
	default:
	}
}

func TestOnDoneRunsAfterTheContextEndsAndRecovers(t *testing.T) {
	captureLog(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	OnDone("test_done", ctx, func() { close(done) }, func() { panic("boom") })
	select {
	case <-done:
		t.Fatal("fn ran before the context ended")
	case <-time.After(10 * time.Millisecond):
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("onPanic never ran")
	}

	live, stopLive := context.WithCancel(context.Background())
	defer stopLive()
	if stop := OnDone("test_done", live, Abandon, func() { t.Error("a stopped callback ran") }); !stop() {
		t.Fatal("a pending callback could not be stopped")
	}
}

func TestLockedReleasesTheLockWhenFnPanics(t *testing.T) {
	var mu sync.Mutex
	func() {
		defer func() { _ = recover() }()
		Locked(&mu, func() { panic("boom") })
	}()
	if !mu.TryLock() {
		t.Fatal("the lock stayed held after the panic")
	}
}

func TestNilOnPanicIsRefusedWhenTheSiteStarts(t *testing.T) {
	for what, start := range map[string]func(){
		"Go":        func() { Go("test_nil", nil, func() {}) },
		"AfterFunc": func() { AfterFunc("test_nil", time.Hour, nil, func() {}) },
		"Run":       func() { Run("test_nil", nil, func() {}) },
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s accepted a site without onPanic", what)
				}
			}()
			start()
		}()
	}
}

func TestLoggedValueAndStackAreBoundedAndStartAtThePanic(t *testing.T) {
	logged := captureLog(t)
	Run("test_bounds", Abandon, func() { panic(strings.Repeat("v", 3*valueLimit)) })
	out := logged.String()
	if strings.Contains(out, strings.Repeat("v", valueLimit+1)) {
		t.Error("the panic value was logged beyond its limit")
	}
	if !strings.Contains(out, `stack="github.com/msch128/mnema-talk/internal/safego.TestLoggedValueAndStack`) {
		t.Errorf("the stack does not start at the frame that panicked: %s", out)
	}
	if got := panicStack(bytes.Repeat([]byte("x\n"), 3*stackLimit)); len(got) != stackLimit {
		t.Fatalf("stack of %d bytes logged, want %d", len(got), stackLimit)
	}
}

// A panic a member can trigger over and over is counted every time but
// logged only a few times a minute.
func TestRepeatedPanicsOfOneSiteAreCountedButNotAllLogged(t *testing.T) {
	logged := captureLog(t)
	before := metrics.GoroutinePanics.With("test_flood").Load()
	for range 4 * logBurst {
		Run("test_flood", Abandon, func() { panic("again") })
	}
	if got := metrics.GoroutinePanics.With("test_flood").Load() - before; got != 4*logBurst {
		t.Fatalf("counted %d panics, want %d", got, 4*logBurst)
	}
	if got := strings.Count(logged.String(), "goroutine panicked"); got != logBurst {
		t.Fatalf("logged %d panics, want %d", got, logBurst)
	}
	Run("test_flood_other", Abandon, func() { panic("mine") })
	if !strings.Contains(logged.String(), "name=test_flood_other") {
		t.Error("another site lost its log line to the flood")
	}
}

func TestLogBudgetRefillsAfterItsWindow(t *testing.T) {
	b := logBudget{sites: map[string]*logSite{}}
	now := time.Now()
	for range logBurst {
		b.allow("x", now)
	}
	if b.allow("x", now.Add(logWindow/2)) {
		t.Fatal("a site logged beyond its budget")
	}
	if !b.allow("x", now.Add(logWindow)) {
		t.Fatal("a site stayed silent after its window")
	}
}

// A site that works for one connection names it on the panic's log line, so
// the panic of one of a member's two sessions is told apart from the other's.
// A site of no connection logs as before.
func TestPanicOfAConnectionsSiteNamesTheConnection(t *testing.T) {
	logged := captureLog(t)
	done := make(chan struct{}, 2)
	finish := func() { done <- struct{}{} }

	RunConn("tab-one", "test_conn", Abandon, func() { panic("first") })
	GoConn("tab-two", "test_conn", finish, func() { panic("second") })
	<-done
	AfterFuncConn("tab-three", "test_conn", time.Millisecond, finish, func() { panic("third") })
	<-done
	Run("test_conn", Abandon, func() { panic("nobody's") })

	lines := strings.Split(strings.TrimSpace(logged.String()), "\n")
	if len(lines) != 4 {
		t.Fatalf("%d lines logged, want 4: %s", len(lines), logged.String())
	}
	for i, want := range []string{"conn_id=tab-one", "conn_id=tab-two", "conn_id=tab-three"} {
		if !strings.Contains(lines[i], want) || !strings.Contains(lines[i], "name=test_conn") {
			t.Errorf("line %d lacks %q: %s", i+1, want, lines[i])
		}
	}
	if strings.Contains(lines[3], "conn_id") {
		t.Errorf("a site of no connection names one: %s", lines[3])
	}
}
