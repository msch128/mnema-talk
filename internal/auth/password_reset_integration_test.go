//go:build integration

package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/audit"
	"github.com/msch128/mnema-talk/internal/httpx"
)

type disconnectRecorder struct {
	LiveControl
	users []uuid.UUID
}

func (d *disconnectRecorder) DisconnectUser(id uuid.UUID) { d.users = append(d.users, id) }

func confirmResetRequest(t *testing.T, h *Handler, token string) *httptest.ResponseRecorder {
	t.Helper()
	body := `{"token":"` + token + `","new_password":"synthetic-new-password"}`
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	w := httptest.NewRecorder()
	httpx.Handle(h.confirmPasswordReset).ServeHTTP(w, r)
	return w
}

// The reset reads the account inside its own transaction: a failing read after
// the fact can no longer leave a changed password without audit and without
// closing the old sessions' sockets, and the link stays usable for a retry.
func TestPasswordResetFollowUpNeverGetsLostToALaterReadFailure(t *testing.T) {
	p, _, member := authFixture(t)
	ctx := context.Background()
	token, err := issueToken(ctx, p, member.ID, purposePasswordReset, "", time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	fault := authHookPool(t, p, &authQueryHook{match: "display_name, bio", cancel: true})
	broken := NewHandler(&Sessions{DB: fault, Secret: secret, TTL: time.Hour}, nil)
	if w := confirmResetRequest(t, broken, token); w.Code != http.StatusInternalServerError {
		t.Fatalf("faulty read: %d %s", w.Code, w.Body.String())
	}
	if _, _, err := Login(ctx, p, member.Username, testPassword); err != nil {
		t.Fatalf("a failed reset changed the password: %v", err)
	}

	live := &disconnectRecorder{}
	h := NewHandler(&Sessions{DB: p, Secret: secret, TTL: time.Hour}, nil)
	h.Audit = &audit.Log{DB: p}
	h.Live = live
	if w := confirmResetRequest(t, h, token); w.Code != http.StatusNoContent {
		t.Fatalf("retry with the same link: %d %s", w.Code, w.Body.String())
	}
	if len(live.users) != 1 || live.users[0] != member.ID {
		t.Fatalf("sockets of the account not closed: %v", live.users)
	}
	var n int
	if err := p.QueryRow(ctx, `SELECT COUNT(*) FROM audit_log WHERE action = 'auth.password_reset_mail' AND target_id = $1`, member.ID.String()).Scan(&n); err != nil || n != 1 {
		t.Fatalf("audit entries = %d (%v)", n, err)
	}
}
