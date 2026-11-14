// Package safego starts every goroutine and timer of the server, and runs its
// callbacks on other people's goroutines, with a fixed answer to a panic. The
// server is one process: an unrecovered panic on any goroutine ends the API,
// the hub, the SFU and the TURN relay together. A panic here is always
// counted (mnema_goroutine_panics_total) and logged with a bounded value and
// stack, and then the site's own decision runs:
//
//   - work that belongs to one connection or one peer passes an onPanic that
//     closes that connection or peer, and the process carries on;
//   - a process-wide loop passes Crash: the panic continues and the process
//     exits, because a restart is better than running on with broken shared
//     state.
//
// A panic inside onPanic is not recovered: when the clean-up itself breaks,
// the process exits. Fatal runtime errors (concurrent map writes, a stack
// overflow, out of memory) cannot be recovered by anyone and end the process
// as before. internal/repo checks that goroutines and timers start nowhere
// else; callbacks that Pion or the HTTP server invoke are wrapped by hand.
package safego

import (
	"context"
	"fmt"
	"log/slog"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/msch128/mnema-talk/internal/metrics"
)

const (
	// stackLimit bounds the logged stack, the frame that panicked first.
	stackLimit = 4096
	// valueLimit bounds the logged panic value: it may quote what a member sent.
	valueLimit = 512
	// A site logs its first logBurst panics of every logWindow in full. More
	// are only counted: a panic a member can trigger must not fill the log.
	logBurst  = 5
	logWindow = time.Minute
)

// crashRequest is what Crash panics with, so Run can tell it apart.
type crashRequest struct{}

// Crash is the onPanic of a process-wide loop: after the count and the log
// line the original panic continues and ends the process.
func Crash() { panic(crashRequest{}) }

// Abandon is the onPanic of work that leaves nothing behind to close: the
// panic is counted and logged, and the work is given up.
func Abandon() {}

// Conn is the id of the connection a site works for (conn_id in the log, see
// doc/operations.md). GoConn, AfterFuncConn, RunConn and RecoveredConn are Go,
// AfterFunc, Run and Recovered with that id on the panic's log line, so the
// panic of one of a member's sessions is told apart from the other's. The
// empty Conn is a site that belongs to no connection.
type Conn string

// Go runs fn on a new goroutine under Run.
func Go(name string, onPanic func(), fn func()) { GoConn("", name, onPanic, fn) }

// GoConn is Go for a site of connection conn.
func GoConn(conn Conn, name string, onPanic func(), fn func()) {
	mustHandle(name, onPanic)
	go RunConn(conn, name, onPanic, fn)
}

// GoWaited is Go for work that wg tracks: it adds one to wg before the
// goroutine starts and marks it done only after Run returned, so whoever
// waits on wg finds a panic of fn already counted, logged and handled. A
// `defer wg.Done()` inside fn runs before Run's recover and lets the waiter
// go first. With Crash the panic continues past Run: Done still runs while
// the goroutine unwinds, and then the process ends.
func GoWaited(wg *sync.WaitGroup, name string, onPanic func(), fn func()) {
	mustHandle(name, onPanic)
	wg.Add(1)
	go func() {
		defer wg.Done()
		Run(name, onPanic, fn)
	}()
}

// AfterFunc is time.AfterFunc with fn under Run.
func AfterFunc(name string, d time.Duration, onPanic func(), fn func()) *time.Timer {
	return AfterFuncConn("", name, d, onPanic, fn)
}

// AfterFuncConn is AfterFunc for a site of connection conn.
func AfterFuncConn(conn Conn, name string, d time.Duration, onPanic func(), fn func()) *time.Timer {
	mustHandle(name, onPanic)
	return time.AfterFunc(d, func() { RunConn(conn, name, onPanic, fn) })
}

// OnDone is context.AfterFunc with fn under Run: fn runs on a goroutine of
// its own once ctx is done, unless the returned stop is called first.
func OnDone(name string, ctx context.Context, onPanic func(), fn func()) (stop func() bool) {
	mustHandle(name, onPanic)
	return context.AfterFunc(ctx, func() { Run(name, onPanic, fn) })
}

// Run calls fn on the caller's goroutine, for a callback that somebody else's
// goroutine invokes (a Pion handler, a request after its connection was
// hijacked). When fn panics the panic is counted and logged under name, then
// onPanic runs; with Crash it continues instead. name is a short constant:
// it becomes a metric label.
func Run(name string, onPanic func(), fn func()) { RunConn("", name, onPanic, fn) }

// RunConn is Run for a site of connection conn.
func RunConn(conn Conn, name string, onPanic func(), fn func()) {
	mustHandle(name, onPanic)
	defer func() {
		if rec := recover(); rec != nil {
			RecoveredConn(conn, name, rec, onPanic)
		}
	}()
	fn()
}

// Recovered is the second half of Run for a path that is too hot for its
// closures (a call per RTP packet): the caller defers a function that calls
// recover itself and hands a non-nil value here, still inside that deferred
// call, so the stack is the panic's.
func Recovered(name string, rec any, onPanic func()) { RecoveredConn("", name, rec, onPanic) }

// RecoveredConn is Recovered for a site of connection conn.
func RecoveredConn(conn Conn, name string, rec any, onPanic func()) {
	metrics.GoroutinePanics.With(name).Inc()
	if logs.allow(name, time.Now()) {
		attrs := []any{"name", name, "err", boundedValue(rec), "stack", panicStack(debug.Stack())}
		if conn != "" {
			attrs = append(attrs, "conn_id", string(conn))
		}
		slog.Error("goroutine panicked", attrs...)
	}
	handle(rec, onPanic)
}

// Locked runs fn while holding l and releases l even when fn panics. A lock
// taken with a plain Lock/Unlock pair stays held after a recovered panic and
// blocks everyone who shares it.
func Locked(l sync.Locker, fn func()) {
	l.Lock()
	defer l.Unlock()
	fn()
}

// mustHandle refuses a site that forgot to say what a panic does, when the
// site starts rather than when it panics.
func mustHandle(name string, onPanic func()) {
	if onPanic == nil {
		panic("safego: " + name + " has no onPanic; pass a clean-up, safego.Abandon or safego.Crash")
	}
}

// handle runs onPanic. Crash asks for the original panic to continue.
func handle(rec any, onPanic func()) {
	defer func() {
		switch again := recover().(type) {
		case nil:
		case crashRequest:
			panic(rec)
		default:
			panic(again)
		}
	}()
	onPanic()
}

func boundedValue(rec any) string {
	s := fmt.Sprint(rec)
	if len(s) > valueLimit {
		s = s[:valueLimit]
	}
	return s
}

// panicStack drops the goroutine header and the frames that only describe
// the recovery (debug.Stack, the deferred closure, panic and the runtime's
// own helpers), so the limit cuts the tail of a deep stack and never the
// frame that panicked.
func panicStack(stack []byte) string {
	lines := strings.Split(string(stack), "\n")
	start := 0
	for i := 0; i+1 < len(lines); i++ {
		if strings.HasPrefix(lines[i], "panic(") {
			start = i + 2
			for start+1 < len(lines) && strings.HasPrefix(lines[start], "runtime.") {
				start += 2
			}
			break
		}
	}
	out := strings.Join(lines[start:], "\n")
	if len(out) > stackLimit {
		out = out[:stackLimit]
	}
	return out
}

// logs is the log budget of every site.
var logs = logBudget{sites: map[string]*logSite{}}

// RefillLogBudget gives every site its full log budget back. The budget is
// the process's and lasts a minute, so a test that reads a panic's log line
// calls this first: an earlier test, or an earlier run in the same process
// (go test -count), may have used the site's budget up.
func RefillLogBudget() {
	logs.mu.Lock()
	defer logs.mu.Unlock()
	clear(logs.sites)
}

type logBudget struct {
	mu    sync.Mutex
	sites map[string]*logSite
}

type logSite struct {
	since time.Time
	n     int
}

// allow reports whether name may log another panic at now.
func (b *logBudget) allow(name string, now time.Time) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	s := b.sites[name]
	if s == nil {
		s = &logSite{}
		b.sites[name] = s
	}
	if now.Sub(s.since) >= logWindow {
		s.since, s.n = now, 0
	}
	s.n++
	return s.n <= logBurst
}
