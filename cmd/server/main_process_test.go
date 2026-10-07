package main

import (
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// TestCommandFatalConfiguration checks the executable's exit contract; the
// child must exit without taking down the test runner or sending host signals.
func TestCommandFatalConfiguration(t *testing.T) {
	if os.Getenv("MNEMA_TEST_FATAL_CHILD") == "1" {
		main()
		return
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(executable, "-test.run=^TestCommandFatalConfiguration$")
	cmd.Env = append(os.Environ(), "MNEMA_TEST_FATAL_CHILD=1", "DATABASE_URL=")
	output, err := cmd.CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 1 {
		t.Fatalf("child exit=%v output=%s", err, output)
	}
	if !strings.Contains(string(output), "fatal") || !strings.Contains(string(output), "DATABASE_URL is required") {
		t.Fatalf("missing fatal configuration error: %s", output)
	}
}
