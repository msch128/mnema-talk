//go:build integration

package main

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/testutil"
)

func TestRotateKeysCommandMovesEverySecretToTheNewDataKey(t *testing.T) {
	pool := testutil.DB(t)
	testutil.Reset(t, pool)
	startupEnv(t, pool)
	ctx := context.Background()
	var id uuid.UUID
	if err := pool.QueryRow(ctx, `INSERT INTO users (username, display_name, password_hash, role)
		VALUES ('cool_guy', 'cool_guy', 'x', 'admin') RETURNING id`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	// Sealed by the instance as installed: under the key derived from JWT_SECRET.
	installed := &auth.Sessions{Secret: os.Getenv("JWT_SECRET")}
	if _, err := installed.BeginEnrolment(ctx, pool, id); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE user_totp SET confirmed_at = NOW() WHERE user_id = $1`, id); err != nil {
		t.Fatal(err)
	}
	stored := func() string {
		var sealed []byte
		if err := pool.QueryRow(ctx, `SELECT secret_enc FROM user_totp WHERE user_id = $1`, id).Scan(&sealed); err != nil {
			t.Fatal(err)
		}
		return string(sealed)
	}
	before := stored()

	// Nothing new is set: the command finds everything where it belongs.
	var out bytes.Buffer
	if err := rotateKeysCommand(ctx, nil, &out); err != nil || !strings.Contains(out.String(), "0 moved to the key derived from JWT_SECRET, 1 were under it already") {
		t.Fatalf("unchanged settings: %v %q", err, out.String())
	}
	if stored() != before {
		t.Fatal("an instance that sets nothing new must not be rewritten")
	}

	t.Setenv("DATA_KEY", strings.Repeat("d", 40))
	out.Reset()
	if err := rotateKeysCommand(ctx, nil, &out); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"1 moved to DATA_KEY, 0 were under it already", "No authenticator secret is under a previous key any more."} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output %q lacks %q", out.String(), want)
		}
	}
	if after := stored(); after == before || !strings.HasPrefix(after, "mtk1") {
		t.Fatal("the stored secret was not sealed again under DATA_KEY")
	}

	// The key is replaced without naming the old one: the command refuses to
	// call that done and says whose secret is stranded.
	t.Setenv("DATA_KEY", strings.Repeat("e", 40))
	err := rotateKeysCommand(ctx, nil, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "cool_guy") || !strings.Contains(err.Error(), "DATA_KEY_PREVIOUS") {
		t.Fatalf("a stranded secret: %v", err)
	}
	t.Setenv("DATA_KEY_PREVIOUS", strings.Repeat("d", 40))
	out.Reset()
	if err := rotateKeysCommand(ctx, nil, &out); err != nil || !strings.Contains(out.String(), "1 moved to DATA_KEY") {
		t.Fatalf("with the previous key named: %v %q", err, out.String())
	}
}

func TestRotateKeysCommandMentionsRecoveryCodes(t *testing.T) {
	pool := testutil.DB(t)
	testutil.Reset(t, pool)
	startupEnv(t, pool)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `WITH u AS (INSERT INTO users (username, display_name, password_hash, role)
		VALUES ('cool_guy', 'cool_guy', 'x', 'admin') RETURNING id)
		INSERT INTO recovery_codes (user_id, code_hash) SELECT id, '\x01' FROM u`); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := rotateKeysCommand(ctx, nil, &out); err != nil || !strings.Contains(out.String(), "1 account(s) hold unused recovery codes") {
		t.Fatalf("%v %q", err, out.String())
	}
}

func TestRotateKeysCommandUsageAndDispatch(t *testing.T) {
	if err := rotateKeysCommand(context.Background(), []string{"now"}, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "usage: mnema-talk rotate-keys") {
		t.Fatalf("arguments: %v", err)
	}
	if isServerStart([]string{"mnema-talk", "rotate-keys"}) {
		t.Fatal("rotate-keys is a subcommand, not a server start")
	}
	old := os.Args
	t.Cleanup(func() { os.Args = old })
	os.Args = []string{"mnema-talk", "rotate-keys", "now"}
	if err := run(); err == nil || !strings.Contains(err.Error(), "usage: mnema-talk rotate-keys") {
		t.Fatalf("run = %v", err)
	}
}

// The operator edited .env but did not restart: `docker exec` still sees the
// old settings. The run must not read like a finished rotation.
func TestRotateKeysCommandSaysWhatItRanWith(t *testing.T) {
	pool := testutil.DB(t)
	testutil.Reset(t, pool)
	startupEnv(t, pool)
	ctx := context.Background()
	old := &auth.Sessions{Secret: os.Getenv("JWT_SECRET")}
	enrolledAccount(t, pool, old, "cool_guy")

	var out bytes.Buffer
	if err := rotateKeysCommand(ctx, nil, &out); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"This run seals under the key derived from JWT_SECRET, fingerprint " + old.KeySetup().Fingerprint,
		"WARNING: no previous key is configured, so this run could not move anything",
		"docker compose up -d",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, out.String())
		}
	}
	if strings.Contains(out.String(), "No authenticator secret is under a previous key any more") || strings.Contains(out.String(), "Previous keys configured") {
		t.Fatalf("a run without a previous key must not read like a finished rotation:\n%s", out.String())
	}

	// The same step done right: the server was restarted with both values.
	t.Setenv("JWT_SECRET_PREVIOUS", old.Secret)
	t.Setenv("JWT_SECRET", strings.Repeat("n", 40))
	out.Reset()
	if err := rotateKeysCommand(ctx, nil, &out); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Previous keys configured for this run: JWT_SECRET_PREVIOUS.", "1 moved to the key derived from JWT_SECRET", "No authenticator secret is under a previous key any more."} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, out.String())
		}
	}
	if strings.Contains(out.String(), old.KeySetup().Fingerprint) || strings.Contains(out.String(), "WARNING") || strings.Contains(out.String(), old.Secret) {
		t.Fatalf("the new key has its own fingerprint, and no value is printed:\n%s", out.String())
	}
}

func TestRotateKeysCommandMovesSecretsBackWhenTheDataKeyIsTakenOut(t *testing.T) {
	pool := testutil.DB(t)
	testutil.Reset(t, pool)
	startupEnv(t, pool)
	ctx := context.Background()
	key := strings.Repeat("d", 40)
	enrolledAccount(t, pool, &auth.Sessions{Secret: os.Getenv("JWT_SECRET"), DataKey: key}, "cool_guy")

	t.Setenv("DATA_KEY_PREVIOUS", key)
	var out bytes.Buffer
	if err := rotateKeysCommand(ctx, nil, &out); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Previous keys configured for this run: DATA_KEY_PREVIOUS.", "1 moved to the key derived from JWT_SECRET"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, out.String())
		}
	}
	t.Setenv("DATA_KEY_PREVIOUS", "")
	out.Reset()
	if err := rotateKeysCommand(ctx, nil, &out); err != nil || !strings.Contains(out.String(), "0 moved to the key derived from JWT_SECRET, 1 were under it already") {
		t.Fatalf("with the data key gone from the settings: %v\n%s", err, out.String())
	}
}

func TestRotateKeysCommandReportsRemovedAbandonedSetups(t *testing.T) {
	pool := testutil.DB(t)
	testutil.Reset(t, pool)
	startupEnv(t, pool)
	ctx := context.Background()
	var id uuid.UUID
	if err := pool.QueryRow(ctx, `INSERT INTO users (username, display_name, password_hash, role)
		VALUES ('gave_up', 'gave_up', 'x', 'user') RETURNING id`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if _, err := (&auth.Sessions{Secret: strings.Repeat("x", 40)}).BeginEnrolment(ctx, pool, id); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := rotateKeysCommand(ctx, nil, &out); err != nil || !strings.Contains(out.String(), "1 two-factor setup(s) that were started but never confirmed could not be read and were removed: gave_up.") {
		t.Fatalf("%v\n%s", err, out.String())
	}
}
