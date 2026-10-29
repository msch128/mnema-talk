package ws

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
)

// A browser reporting a failed ICE connection reaches the default log level;
// its other diagnostics stay at debug.
func TestClientDiagLogsFailedICEAtWarn(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelInfo})))
	t.Cleanup(func() { slog.SetDefault(previous) })

	c := &Client{User: auth.User{ID: uuid.New(), Username: "marvin"}}
	c.handleDiag(json.RawMessage(`{"event":"ice","ice":"connected"}`))
	if logs.Len() != 0 {
		t.Fatalf("a healthy report is logged above debug: %s", logs.String())
	}
	c.handleDiag(json.RawMessage(`{"event":"ice","ice":"failed","candidates":{"udp/host":1},"ice_servers":0}`))
	out := logs.String()
	for _, want := range []string{"level=WARN", "webrtc client diag", "user=marvin", "udp/host"} {
		if !strings.Contains(out, want) {
			t.Errorf("log lacks %q: %s", want, out)
		}
	}
}
