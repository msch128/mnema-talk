package main

import (
	"errors"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/msch128/mnema-talk/internal/config"
)

// TestCommandFatalConfiguration checks the executable's exit contract; the
// child must exit without taking down the test runner or sending host signals.
func TestCommandFatalConfiguration(t *testing.T) {
	if os.Getenv("MNEMA_TEST_FATAL_CHILD") == "1" {
		configErrorDelay = 0
		main()
		return
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(executable, "-test.run=^TestCommandFatalConfiguration$")
	cmd.Env = append(os.Environ(), "MNEMA_TEST_FATAL_CHILD=1", "DATABASE_URL=", "APP_ENV=production", "JWT_SECRET=", "S3_ACCESS_KEY=", "S3_SECRET_KEY=")
	output, err := cmd.CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 78 {
		t.Fatalf("child exit=%v output=%s", err, output)
	}
	// One plain block, first in the log, listing every missing setting.
	if !strings.HasPrefix(string(output), "Mnema Talk cannot start - fix these settings in .env:\n") {
		t.Fatalf("the settings block is not the first output: %s", output)
	}
	for _, want := range []string{"DATABASE_URL is required", "JWT_SECRET", "S3_ACCESS_KEY", "S3_SECRET_KEY", "docker compose up -d"} {
		if !strings.Contains(string(output), want) {
			t.Fatalf("output lacks %q: %s", want, output)
		}
	}
	if strings.Contains(string(output), `"level"`) {
		t.Fatalf("a JSON log line instead of plain text: %s", output)
	}
}

func TestLogLevel(t *testing.T) {
	for _, tc := range []struct {
		level, env string
		want       slog.Level
	}{
		{"debug", "production", slog.LevelDebug}, {"info", "development", slog.LevelInfo},
		{"warn", "development", slog.LevelWarn}, {"error", "development", slog.LevelError},
		{"", "production", slog.LevelInfo}, {"", "development", slog.LevelDebug},
	} {
		if got := logLevel(&config.Config{LogLevel: tc.level, AppEnv: tc.env}); got != tc.want {
			t.Fatalf("%+v = %v", tc, got)
		}
	}
}

func TestRunRejectsInvalidConfiguration(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	if err := run(); err == nil {
		t.Fatal("invalid config accepted")
	}
}
