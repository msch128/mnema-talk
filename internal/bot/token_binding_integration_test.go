//go:build integration

package bot

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/perm"
)

// A failed bind lookup must not decide the issuer: with the creator query
// failing, the Owner must not be offered alone.
func TestBindCandidatesReportsDatabaseErrors(t *testing.T) {
	e := newEnv(t)
	f := e.newBot()
	a := NewAuthenticator(e.p)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got, err := a.bindCandidates(ctx, f.token.ID); err == nil {
		t.Fatalf("candidates = %v, want the database error", got)
	}
	got, err := a.bindCandidates(context.Background(), f.token.ID)
	if err != nil || len(got) != 1 || got[0] != e.owner.ID {
		t.Fatalf("candidates = %v, %v, want the Owner (also the creator)", got, err)
	}
}

func TestRegenerateTokenKeepsTheOldOneOnFailureAndRefusesDeadTokens(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	f := e.newBot()

	// Expired and revoked tokens cannot be regenerated; nothing changes.
	var expired, revoked uuid.UUID
	if err := e.p.QueryRow(ctx, `INSERT INTO bot_tokens (bot_id, name, token_hash, hint, expires_at) VALUES ($1, 'e', $2, 'xxxx', NOW() - INTERVAL '1 hour') RETURNING id`, f.bot.ID, HashToken("e")).Scan(&expired); err != nil {
		t.Fatal(err)
	}
	if err := e.p.QueryRow(ctx, `INSERT INTO bot_tokens (bot_id, name, token_hash, hint, revoked_at) VALUES ($1, 'r', $2, 'yyyy', NOW()) RETURNING id`, f.bot.ID, HashToken("r")).Scan(&revoked); err != nil {
		t.Fatal(err)
	}
	h := e.handler(e.p)
	for _, id := range []uuid.UUID{expired, revoked} {
		w := e.serve(h, "POST", "/admin/bots/"+f.bot.ID.String()+"/tokens/"+id.String()+"/regenerate", nil, nil)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("regenerate dead token: %d %s", w.Code, w.Body)
		}
	}
	var n int
	if err := e.p.QueryRow(ctx, `SELECT COUNT(*) FROM bot_tokens WHERE bot_id = $1`, f.bot.ID).Scan(&n); err != nil || n != 3 {
		t.Fatalf("tokens = %d, %v, want 3 (no new token)", n, err)
	}

	// An active one is replaced in one step, with its name and expiry.
	exp := time.Now().Add(48 * time.Hour).UTC().Truncate(time.Second)
	live, _, err := IssueToken(ctx, e.p, f.bot.ID, "live", &exp, e.owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	w := e.serve(h, "POST", "/admin/bots/"+f.bot.ID.String()+"/tokens/"+live.ID.String()+"/regenerate", nil, nil)
	if w.Code != http.StatusCreated {
		t.Fatalf("regenerate: %d %s", w.Code, w.Body)
	}
	var out NewTokenResponse
	if err := json.NewDecoder(strings.NewReader(w.Body.String())).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.Token.State != "active" || out.Token.Name != "live" || out.Token.ExpiresAt == nil || !out.Token.ExpiresAt.Equal(exp) {
		t.Fatalf("new token = %+v", out.Token)
	}
	var revokedAt *time.Time
	if err := e.p.QueryRow(ctx, `SELECT revoked_at FROM bot_tokens WHERE id = $1`, live.ID).Scan(&revokedAt); err != nil || revokedAt == nil {
		t.Fatalf("old token revoked_at = %v, %v", revokedAt, err)
	}

	// When the new token cannot be issued (the cap), the old one stays valid.
	for {
		if _, _, err := IssueToken(ctx, e.p, f.bot.ID, "fill", nil, e.owner.ID); err != nil {
			break
		}
	}
	keep := f.token.ID
	if _, _, err := RegenerateToken(ctx, e.p, f.bot.ID, uuid.New(), e.owner.ID); err == nil {
		t.Fatal("unknown token regenerated")
	}
	// At the cap the old token's slot is freed by its own revocation, so this succeeds.
	if _, _, err := RegenerateToken(ctx, e.p, f.bot.ID, keep, e.owner.ID); err != nil {
		t.Fatalf("regenerate at the cap: %v", err)
	}
}

func TestPatchCommandNeedsDisabled(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	f := e.newBot()
	if _, err := SetCommandDisabled(ctx, e.p, f.bot.ID, f.cmd.ID, true); err != nil {
		t.Fatal(err)
	}
	h := e.handler(e.p)
	path := "/admin/bots/" + f.bot.ID.String() + "/commands/" + f.cmd.ID.String()
	for _, body := range []string{`{}`, `{"disable":true}`, `{"disabled":null}`} {
		r, hdr := jsonBody(body)
		if w := e.serve(h, "PATCH", path, r, hdr); w.Code != http.StatusBadRequest {
			t.Fatalf("PATCH %s: %d, want 400", body, w.Code)
		}
	}
	var disabled bool
	if err := e.p.QueryRow(ctx, `SELECT disabled FROM bot_commands WHERE id = $1`, f.cmd.ID).Scan(&disabled); err != nil || !disabled {
		t.Fatalf("command disabled = %v, %v, want it still off", disabled, err)
	}
	r, hdr := jsonBody(`{"disabled":false}`)
	if w := e.serve(h, "PATCH", path, r, hdr); w.Code != http.StatusOK {
		t.Fatalf("PATCH false: %d", w.Code)
	}
}

// A banned or deleted member is not a valid user option.
func TestUserOptionRefusesBannedMembers(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	h, cmds := e.restHandler(e.p, true)
	st := e.restSetup(cmds)
	var member uuid.UUID
	if err := e.p.QueryRow(ctx, `INSERT INTO users (username, display_name, password_hash, role) VALUES ('banned', 'b', 'x', 'user') RETURNING id`).Scan(&member); err != nil {
		t.Fatal(err)
	}
	run := func() int {
		body := `{"command_id":"` + st.cmd.String() + `","options":{"text":"x","who":"` + member.String() + `"}}`
		return e.do(h, "POST", "/api/channels/"+e.ch.String()+"/interactions", "", body).Code
	}
	if got := run(); got != http.StatusAccepted {
		t.Fatalf("active member as option: %d, want 202", got)
	}
	if _, err := e.p.Exec(ctx, `UPDATE users SET banned_at = NOW() WHERE id = $1`, member); err != nil {
		t.Fatal(err)
	}
	if got := run(); got != http.StatusBadRequest {
		t.Fatalf("banned member as option: %d, want 400", got)
	}
}

// A response that could not be posted leaves the interaction answerable.
func TestFailedResponseKeepsTheInteractionAnswerable(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	h, cmds := e.restHandler(e.p, true)
	st := e.restSetup(cmds)
	ia := BasePath + "/interactions/" + st.interaction.String() + "/" + st.itoken

	// The bot lacks Send messages at this moment.
	if _, err := e.p.Exec(ctx, `UPDATE roles SET permissions = 0 WHERE id = $1`, e.role); err != nil {
		t.Fatal(err)
	}
	body := `{"type":4,"data":{"content":"hi"}}`
	if w := e.do(h, "POST", ia+"/callback", st.token, body); w.Code != http.StatusForbidden {
		t.Fatalf("callback without permission: %d %s", w.Code, w.Body)
	}
	if _, err := e.p.Exec(ctx, `UPDATE roles SET permissions = $1 WHERE id = $2`, int64(perm.SendMessages), e.role); err != nil {
		t.Fatal(err)
	}
	cmds.Perms.Invalidate()
	if got := e.do(h, "POST", ia+"/callback", st.token, body).Code; got != http.StatusNoContent {
		t.Fatalf("retry after a failed response: %d, want 204", got)
	}
}
