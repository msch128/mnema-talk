//go:build integration

package authn

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/mailer"
	"github.com/msch128/mnema-talk/internal/perm/permtest"
	"github.com/msch128/mnema-talk/internal/testutil/racetest"
)

// recordingMail keeps every message it is asked to send.
type recordingMail struct {
	mu   sync.Mutex
	sent []mailer.Message
}

func (m *recordingMail) Send(_ context.Context, msg mailer.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sent = append(m.sent, msg)
	return nil
}

var mailedToken = regexp.MustCompile(`[A-Za-z0-9_-]{43}`)

// Lane 7 of the defect hunt. The reset mail promises that only the newest
// link works (auth.IssueAccountToken: "Earlier unused tokens of the same
// purpose stop working, so only the newest link is live"). Anyone can ask for
// a reset for any address; a few requests that arrive together, each within
// every limit, each mail a link. Oracle: of the links in the mailbox, at most
// one still opens the reset.
func TestParallelResetRequestsLeaveOnlyTheNewestLinkWorking(t *testing.T) {
	p, _, member := authFixture(t)
	ctx := context.Background()
	const address = "member@example.org"
	if _, err := p.Exec(ctx, `UPDATE users SET email = $2, email_verified_at = NOW() WHERE id = $1`, member.ID, address); err != nil {
		t.Fatal(err)
	}
	callers := auth.TokensPerHour
	var mail *recordingMail
	var h *Handler
	res := racetest.Sweep(t, racetest.Rounds(10, 300), callers,
		func(int) {
			if _, err := p.Exec(ctx, `DELETE FROM account_tokens`); err != nil {
				t.Fatal(err)
			}
			// A fresh handler per round: its in-memory budgets are those of a
			// process that has just started.
			mail = &recordingMail{}
			h = NewHandler(&auth.Sessions{DB: p, Secret: secret, TTL: time.Hour}, permtest.NewFixture().Service(), nil)
			h.Mail = mail
		},
		func(_, i int) error {
			r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"email":"`+address+`"}`))
			r.RemoteAddr = fmt.Sprintf("192.0.2.%d:1234", i+1)
			w := httptest.NewRecorder()
			err := h.requestPasswordReset(w, r)
			if err == nil && w.Code != http.StatusAccepted {
				err = fmt.Errorf("status %d", w.Code)
			}
			h.DrainMail()
			return err
		},
		func(round int, _ racetest.Round) string {
			h.DrainMail()
			working := 0
			for _, msg := range mail.sent {
				// Probe without spending: a token that would be accepted is
				// unused, unexpired and of this purpose.
				token := mailedToken.FindString(msg.Body)
				if !auth.WellFormedToken(token) {
					t.Fatalf("round %d: no link in the mail %q", round, msg.Body)
				}
				var live bool
				if err := p.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM account_tokens WHERE token_hash = $1
					AND purpose = 'password_reset' AND used_at IS NULL AND expires_at > NOW())`,
					auth.HashToken(token)).Scan(&live); err != nil {
					t.Fatal(err)
				}
				if live {
					working++
				}
			}
			if len(mail.sent) == 0 {
				t.Fatalf("round %d: no reset mail was sent, the sweep tested nothing", round)
			}
			if working != 1 {
				return fmt.Sprintf("round %d: %d mails, %d links work", round, len(mail.sent), working)
			}
			return ""
		})
	res.FailOnViolation(t)
}
