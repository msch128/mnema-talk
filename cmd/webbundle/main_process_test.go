package main

import (
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestCommandUsage(t *testing.T) {
	if os.Getenv("MNEMA_TEST_WEBBUNDLE_CHILD") == "1" {
		os.Args = []string{"webbundle"}
		main()
		return
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(executable, "-test.run=^TestCommandUsage$")
	cmd.Env = append(os.Environ(), "MNEMA_TEST_WEBBUNDLE_CHILD=1")
	output, err := cmd.CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 2 {
		t.Fatalf("child exit=%v output=%s", err, output)
	}
	if !strings.Contains(string(output), "usage: webbundle <command>") {
		t.Fatalf("missing usage output: %s", output)
	}
}
