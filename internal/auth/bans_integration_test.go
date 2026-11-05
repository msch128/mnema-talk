//go:build integration

package auth

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/msch128/mnema-talk/internal/httpx"
)

func TestBannedErrorTellsWhyAndUntilWhen(t *testing.T) {
	p, _, member := authFixture(t)
	ctx := context.Background()

	// The ban message needs the ban; a failing lookup is an error, a missing ban is plain "disabled".
	if err := bannedError(ctx, p, member.ID); !errors.Is(err, ErrAccountDisabled) {
		t.Fatalf("no ban: %v", err)
	}
	until := time.Now().Add(time.Hour)
	if _, err := p.Exec(ctx, `UPDATE users SET banned_at = NOW(), banned_until = $2, ban_reason = $3 WHERE id = $1`, member.ID, until, "because"); err != nil {
		t.Fatal(err)
	}
	err := bannedError(ctx, p, member.ID)
	if ae, ok := httpx.AsAPIError(err); !ok || !strings.Contains(ae.Message, "until") || !strings.Contains(ae.Message, "because") {
		t.Fatalf("ban message: %v", err)
	}
	broken := authHookPool(t, p, &authQueryHook{match: "SELECT ban_reason, banned_until", cancel: true})
	if err := bannedError(ctx, broken, member.ID); err == nil || errors.Is(err, ErrAccountDisabled) {
		t.Fatalf("lookup failure = %v", err)
	}
}
