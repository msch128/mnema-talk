package authn

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

// The WaitGroup misuse panic ("Add called concurrently with Wait", "WaitGroup
// is reused before previous Wait has returned") needs a window of a few
// nanoseconds, so it cannot be forced. The child process hammers the window
// and dies if it hits; the parent only reports, so the test binary survives.
// Deep probe: run with MNEMA_DRAIN_HAMMER=1 (add -race to see the race report).
const hammerChildEnv = "MNEMA_DRAIN_HAMMER_CHILD"

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
	t.Logf("child: err=%v misuse-panic=%v race=%v", err,
		strings.Contains(string(out), "WaitGroup"), strings.Contains(string(out), "DATA RACE"))
	// Known gap, inverse assertion: the child is expected to die of the misuse
	// panic. It turns red when Drain no longer shares a WaitGroup with Add.
	hit := strings.Contains(string(out), "WaitGroup is reused") || strings.Contains(string(out), "Add called concurrently")
	if len(out) > 3000 {
		out = out[:3000]
	}
	t.Logf("%s", out)
	if !hit {
		t.Fatal("fixed (or the window was not hit in 15 s): no WaitGroup misuse panic in the child")
	}
}

func hammerChild() {
	h := &Handler{rec: newRecovery()}
	stop := time.Now().Add(15 * time.Second)
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(2)
		go func() {
			defer wg.Done()
			for time.Now().Before(stop) {
				h.background(func(context.Context) {})
			}
		}()
		go func() {
			defer wg.Done()
			for time.Now().Before(stop) {
				h.Drain(time.Second)
			}
		}()
	}
	wg.Wait()
}
