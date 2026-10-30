//go:build integration

package main

import (
	"bytes"
	"context"
	"net"
	"strings"
	"testing"

	"github.com/msch128/mnema-talk/internal/config"
	"github.com/msch128/mnema-talk/internal/testutil"
)

// A second program on the web port makes the start fail with a message that
// names the port, says another program holds it and names the variable.
func TestStartupNamesAPortThatIsInUse(t *testing.T) {
	pool := testutil.DB(t)
	testutil.Reset(t, pool)
	startupEnv(t, pool)
	taken, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer taken.Close()
	_, port, _ := net.SplitHostPort(taken.Addr().String())
	t.Setenv("PORT", port)
	t.Setenv("PUBLIC_URL", "http://"+taken.Addr().String())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	err = runWithContext(ctx)
	if err == nil {
		t.Fatal("started on a port that is in use")
	}
	for _, want := range []string{"TCP port " + port + " is already in use by another program", "PORT in .env"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error lacks %q: %v", want, err)
		}
	}
}

// The doctor asks the real database; everything else is a fixture.
func TestDoctorAnswersFromTheRealDatabase(t *testing.T) {
	pool := testutil.DB(t)
	startupEnv(t, pool)
	t.Setenv("APP_ENV", "production")
	t.Setenv("JWT_SECRET", strings.Repeat("k", 40))
	t.Setenv("S3_ACCESS_KEY", "doctor-access-key")
	t.Setenv("S3_SECRET_KEY", "doctor-secret-key-value")
	t.Setenv("PUBLIC_URL", "https://chat.example.com")
	t.Setenv("WEBRTC_UDP_MUX_PORT", "")
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	p := healthyProbes()
	p.DBPing = pool.Ping
	var out bytes.Buffer
	if err := runDoctor(context.Background(), cfg, p, &out); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "[ ok ] The database is reachable.") {
		t.Errorf("report:\n%s", out.String())
	}
}
