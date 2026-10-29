package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"log/slog"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/msch128/mnema-talk/internal/config"
	"github.com/msch128/mnema-talk/internal/setupcheck"
)

// syncBuffer is a log sink the test can read while the logger still writes.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func productionTestConfig(t *testing.T) *config.Config {
	t.Helper()
	env := map[string]string{
		"APP_ENV": "production", "DATABASE_URL": "postgres://mnema:s3cret-db-pass@postgres:5432/mnema_talk",
		"JWT_SECRET": strings.Repeat("k", 32), "S3_ACCESS_KEY": "mnema_admin", "S3_SECRET_KEY": "a-real-secret-key",
		"PUBLIC_URL": "https://chat.example.com", "CLOCK_CHECK_ENABLED": "false",
	}
	cfg, err := config.FromEnv(func(k string) (string, bool) { v, ok := env[k]; return v, ok })
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestWatchClockRecordsTheSkewOfTheNTPServer(t *testing.T) {
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pc.Close() })
	go func() {
		buf := make([]byte, 64)
		for {
			_, from, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			resp := make([]byte, 48)
			resp[0], resp[1] = 0x1C, 2
			binary.BigEndian.PutUint32(resp[40:], uint32(time.Now().Add(time.Minute).Unix()+2208988800))
			_, _ = pc.WriteTo(resp, from)
		}
	}()
	rt := &setupcheck.Runtime{}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go watchClock(ctx, rt, []string{pc.LocalAddr().String()}, time.Hour)

	cfg := productionTestConfig(t)
	deadline := time.Now().Add(5 * time.Second)
	for {
		for _, c := range setupcheck.Run(setupcheck.Input{Cfg: cfg, Runtime: rt}) {
			if c.ID == setupcheck.Clock {
				if c.Level != setupcheck.Fail {
					t.Fatalf("a clock a minute behind the server must be red: %+v", c)
				}
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("the clock skew was never recorded")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestStartSetupChecksLogsTheChecklistAfterTheDelay(t *testing.T) {
	var buf syncBuffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(old) })
	oldDelay := setupLogDelay
	setupLogDelay = 10 * time.Millisecond
	t.Cleanup(func() { setupLogDelay = oldDelay })

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	startSetupChecks(ctx, productionTestConfig(t), &setupcheck.Runtime{},
		func(context.Context) error { return nil }, func() error { return errors.New("starting") })

	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(buf.String(), `"msg":"setup checks"`) {
		if time.Now().After(deadline) {
			t.Fatalf("no summary logged: %s", buf.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
	out := buf.String()
	if !strings.Contains(out, "WEBRTC_NAT_1TO1_IP") || !strings.Contains(out, `"check":"storage"`) {
		t.Errorf("the open items and their fixes must be logged: %s", out)
	}
}
