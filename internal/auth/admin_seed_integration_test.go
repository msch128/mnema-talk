//go:build integration

package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

func TestAdminSeedDatabaseFailuresAndGeneratedPassword(t *testing.T) {
	p, admin, _ := authFixture(t)
	ctx := context.Background()
	// Existing administrator ignores a now-invalid seed configuration.
	if err := EnsureAdminUser(ctx, p, "bad name", "short", true); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Exec(ctx, `DELETE FROM users WHERE id=$1`, admin.ID); err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{"SELECT username FROM users WHERE LOWER(username)", "INSERT INTO users (username, display_name, password_hash, role)"} {
		t.Run(query, func(t *testing.T) {
			fault := authHookPool(t, p, &authQueryHook{match: query, cancel: true})
			if e := EnsureAdminUser(ctx, fault, "newadmin", testPassword, true); !errors.Is(e, context.Canceled) {
				t.Fatalf("seed failure = %v", e)
			}
		})
	}
	fault := authHookPool(t, p, &authQueryHook{match: "INSERT INTO users (username, display_name, password_hash, role)", before: func() {
		if _, e := p.Exec(ctx, `INSERT INTO users(username,display_name,password_hash,role) VALUES('newadmin','newadmin','test-only-hash','user')`); e != nil {
			t.Fatal(e)
		}
	}})
	if e := EnsureAdminUser(ctx, fault, "newadmin", testPassword, true); e == nil || !strings.Contains(e.Error(), "taken by a regular user") {
		t.Fatalf("concurrent seed conflict = %v", e)
	}
	// Keep development's one-time generated password out of the test logs.
	// Its stored hash and generated-password branch are tested separately by
	// a capture logger below.
	var captured bytes.Buffer
	logger := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&captured, nil)))
	t.Cleanup(func() { slog.SetDefault(logger) })
	if err := EnsureAdminUser(ctx, p, "generated-admin", "", false); err != nil {
		t.Fatal(err)
	}
	var generated string
	for _, line := range bytes.Split(bytes.TrimSpace(captured.Bytes()), []byte("\n")) {
		var record map[string]any
		if err := json.Unmarshal(line, &record); err != nil {
			t.Fatal("generated seed log was not JSON")
		}
		if value, ok := record["password"].(string); ok {
			generated = value
		}
	}
	if len(generated) != 24 {
		t.Fatal("development seed did not emit a random 24-character password")
	}
	if _, _, err := Login(ctx, p, "generated-admin", generated); err != nil {
		t.Fatal("generated password did not authenticate")
	}
	captured.Reset()
	if err := EnsureAdminUser(ctx, p, "generated-admin", "", false); err != nil {
		t.Fatal(err)
	}
	if captured.Len() != 0 {
		t.Fatal("seed emitted credentials again after administrator existed")
	}
}
