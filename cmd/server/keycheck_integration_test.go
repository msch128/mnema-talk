//go:build integration

package main

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/testutil"
)

// enrolledAccount creates an account with two-factor sign-in on, its secret
// sealed with the keys of s.
func enrolledAccount(t *testing.T, pool *db.Pool, s *auth.Sessions, name string) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	var id uuid.UUID
	if err := pool.QueryRow(ctx, `INSERT INTO users (username, display_name, password_hash, role)
		VALUES ($1, $1, 'x', 'user') RETURNING id`, name).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if _, err := s.BeginEnrolment(ctx, pool, id); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE user_totp SET confirmed_at = NOW() WHERE user_id = $1`, id); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestTheStartReportsAuthenticatorSecretsNoConfiguredKeyOpens(t *testing.T) {
	pool := testutil.DB(t)
	testutil.Reset(t, pool)
	ctx := context.Background()
	withKey := &auth.Sessions{Secret: strings.Repeat("j", 40), DataKey: strings.Repeat("d", 40)}
	enrolledAccount(t, pool, withKey, "quiet_member")
	enrolledAccount(t, pool, withKey, "other_member")

	report := func(s *auth.Sessions) string {
		var log bytes.Buffer
		reportStoredSecrets(ctx, s, pool, slog.New(slog.NewJSONHandler(&log, nil)), storedSecretsCheckLimit)
		return log.String()
	}
	healthy := report(withKey)
	if !strings.Contains(healthy, `"msg":"authenticator secrets are sealed under DATA_KEY"`) ||
		!strings.Contains(healthy, `"fingerprint":"`+withKey.KeySetup().Fingerprint+`"`) || strings.Contains(healthy, `"level":"ERROR"`) {
		t.Fatalf("a start with every key configured: %s", healthy)
	}

	// DATA_KEY was taken out of the settings again.
	removed := &auth.Sessions{Secret: withKey.Secret}
	broken := report(removed)
	for _, want := range []string{`"level":"ERROR"`, "AUTHENTICATOR SECRETS CANNOT BE READ", `"accounts":2`, `"name_a_data_key_that_is_not_configured":2`, "rotate-keys"} {
		if !strings.Contains(broken, want) {
			t.Errorf("log lacks %q: %s", want, broken)
		}
	}
	for _, secret := range []string{withKey.Secret, withKey.DataKey, "quiet_member", "other_member"} {
		if strings.Contains(broken, secret) {
			t.Fatalf("the log line carries a key or an account name: %s", broken)
		}
	}
	if strings.Contains(broken, withKey.KeySetup().Fingerprint) {
		t.Fatal("another sealing key has another fingerprint")
	}
}

func TestTheServerStartRunsTheStoredSecretCheck(t *testing.T) {
	pool := testutil.DB(t)
	testutil.Reset(t, pool)
	startupEnv(t, pool)
	enrolledAccount(t, pool, &auth.Sessions{Secret: strings.Repeat("x", 40)}, "stranded_member")

	// The start logs to stdout; it stops at the voice address, after the check.
	t.Setenv("WEBRTC_NAT_1TO1_IP", "::1")
	logFile, err := os.Create(filepath.Join(t.TempDir(), "stdout"))
	if err != nil {
		t.Fatal(err)
	}
	stdout := os.Stdout
	os.Stdout = logFile
	err = runWithContext(context.Background())
	os.Stdout = stdout
	_ = logFile.Close()
	if err == nil || !strings.Contains(err.Error(), "IPv4") {
		t.Fatalf("start: %v", err)
	}
	log, err := os.ReadFile(logFile.Name())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"authenticator secrets are sealed under the key derived from JWT_SECRET", "AUTHENTICATOR SECRETS CANNOT BE READ", `"accounts":1`} {
		if !strings.Contains(string(log), want) {
			t.Errorf("start log lacks %q:\n%s", want, log)
		}
	}
}
