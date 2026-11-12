package authn

import (
	"context"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

// The WaitGroup misuse panic ("Add called concurrently with Wait", "WaitGroup
// is reused before previous Wait has returned") needs a window of a few
// nanoseconds, so it cannot be forced. The child process hammers the window,
// mail jobs and tracked jobs against several concurrent Drains, round after
// round on a fresh handler; it dies if the window opens again (H-L7-08). The
// parent only reports, so the test binary survives the panic. Deep probe, run
// with MNEMA_DRAIN_HAMMER=1, and with -race to also catch a race between Add
// and Wait in the child (the child is the same, race-enabled binary).
const hammerChildEnv = "MNEMA_DRAIN_HAMMER_CHILD"

// hammerFor is how long the child hammers.
const hammerFor = 10 * time.Second

func TestDrainHammer(t *testing.T) {
	if os.Getenv(hammerChildEnv) == "1" {
		hammerChild()
		return
	}
	if os.Getenv("MNEMA_DRAIN_HAMMER") != "1" {
		t.Skip("deep probe, set MNEMA_DRAIN_HAMMER=1")
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestDrainHammer$", "-test.v")
	cmd.Env = append(os.Environ(), hammerChildEnv+"=1")
	out, err := cmd.CombinedOutput()
	text := string(out)
	misuse := strings.Contains(text, "WaitGroup")
	race := strings.Contains(text, "DATA RACE")
	if err == nil && !misuse && !race {
		return
	}
	if len(text) > 3000 {
		text = text[:3000]
	}
	t.Fatalf("child: err=%v misuse-panic=%v race=%v\n%s", err, misuse, race, text)
}

func hammerChild() {
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil))) // the dropped-mail warnings
	for stop := time.Now().Add(hammerFor); time.Now().Before(stop); {
		hammerRound(&Handler{rec: newRecovery()})
	}
}

// hammerRound runs adders and drainers on h at once until all are done.
func hammerRound(h *Handler) {
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(2)
		go func() {
			defer wg.Done()
			for range 50 {
				h.background(func(context.Context) {})
				if done, ok := h.TrackJob(); ok {
					done()
				}
			}
		}()
		go func() {
			defer wg.Done()
			for range 5 {
				h.Drain(time.Second)
			}
		}()
	}
	wg.Wait()
}
