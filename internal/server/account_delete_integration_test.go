//go:build integration

package server

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
)

type listedMessage struct {
	ID          uuid.UUID `json:"id"`
	UserID      uuid.UUID `json:"user_id"`
	Username    string    `json:"username"`
	DisplayName string    `json:"display_name"`
	Content     string    `json:"content"`
	ReplyTo     *struct {
		Content string `json:"content"`
	} `json:"reply_to"`
}

func (a *app) listMessages(c *client, channel uuid.UUID) []listedMessage {
	a.t.Helper()
	res := c.get("/api/channels/" + channel.String() + "/messages")
	if res.status != http.StatusOK {
		a.t.Fatalf("messages: %d %s", res.status, res.body)
	}
	var out []listedMessage
	res.decode(a.t, &out)
	return out
}

func (a *app) deleteAccount(c *client, password, code string) response {
	body := map[string]string{"current_password": password}
	if code != "" {
		body["code"] = code
	}
	return c.post("/api/users/me/delete", body)
}

// foreignKeysToUsers lists every (table, column) that references users(id).
func (a *app) foreignKeysToUsers() [][2]string {
	a.t.Helper()
	rows, err := a.db.Query(context.Background(), `
		SELECT c.conrelid::regclass::text, att.attname
		FROM pg_constraint c
		JOIN pg_attribute att ON att.attrelid = c.conrelid AND att.attnum = ANY (c.conkey)
		WHERE c.contype = 'f' AND c.confrelid = 'users'::regclass`)
	if err != nil {
		a.t.Fatal(err)
	}
	defer rows.Close()
	var out [][2]string
	for rows.Next() {
		var table, col string
		if err := rows.Scan(&table, &col); err != nil {
			a.t.Fatal(err)
		}
		out = append(out, [2]string{table, col})
	}
	return out
}

// populate gives alice a footprint in every place the app keeps something
// about a member.
func (a *app) populate(admin, alice, bob *client, ch uuid.UUID) (aliceMsg, bobMsg uuid.UUID, files []string) {
	a.t.Helper()
	ctx := context.Background()
	aliceMsg = a.send(alice, ch, "alice's private words, call me at 555-0100 @bob")
	bobMsg = a.send(bob, ch, "bob's own words")
	reply := alice.post("/api/channels/"+ch.String()+"/messages", map[string]any{"content": "alice again", "reply_to_id": bobMsg})
	if reply.status != http.StatusCreated {
		a.t.Fatalf("reply: %d %s", reply.status, reply.body)
	}
	a.send(bob, ch, "bob quotes alice") // a thread of bob's around alice's message
	bob.post("/api/messages/"+aliceMsg.String()+"/reactions", map[string]string{"emoji": "🔥"})
	alice.post("/api/messages/"+bobMsg.String()+"/reactions", map[string]string{"emoji": "👍"})
	alice.put("/api/channels/"+ch.String()+"/notifications", map[string]any{"level": "mute"})
	alice.put("/api/users/me/profile", map[string]string{"display_name": "Alice Real", "bio": "my private bio"})
	alice.put("/api/users/me/status", map[string]string{"status_text": "at the dentist"})
	a.setEmail(alice, "alice@example.org")
	if _, err := a.db.Exec(ctx, `INSERT INTO push_subscriptions (user_id, endpoint, p256dh, auth) VALUES ($1, 'https://push.example.org/alice', 'k', 'a')`, alice.user.ID); err != nil {
		a.t.Fatal(err)
	}

	for i, name := range []string{"avatars/a.png", "uploads/a.pdf"} {
		files = append(files, name)
		if err := a.store.Upload(ctx, name, strings.NewReader("data"), "application/octet-stream", 4); err != nil {
			a.t.Fatal(err)
		}
		var msg any
		if i == 1 {
			msg = aliceMsg
		}
		_, err := a.db.Exec(ctx, `INSERT INTO media (uploader_id, channel_id, message_id, s3_bucket, s3_key, original_filename, mime_type, size_bytes)
			VALUES ($1, $2, $3, 'b', $4, 'tax-return-alice.pdf', 'application/pdf', 4)`, alice.user.ID, ch, msg, name)
		if err != nil {
			a.t.Fatal(err)
		}
	}
	// A moderator touched the account: audit entries with the name and details.
	admin.post("/api/admin/users/"+alice.user.ID.String()+"/disable", nil)
	admin.post("/api/admin/users/"+alice.user.ID.String()+"/enable", nil)
	return aliceMsg, bobMsg, files
}

func TestDeletingAnAccountRemovesThePersonButKeepsThreadsReadable(t *testing.T) {
	a, _ := mailApp(t)
	admin := a.seedAdmin()
	alice := a.register(admin, "alice")
	bob := a.register(admin, "bob")
	ch := a.createChannel(admin, "general", "text")
	aliceMsg, bobMsg, files := a.populate(admin, alice, bob, ch)
	alice = a.login("alice", memberPassword) // the moderator's disable ended her session
	aliceID := alice.user.ID

	if res := a.deleteAccount(alice, "wrong-password", ""); res.status != http.StatusForbidden {
		t.Fatalf("a wrong password: %d", res.status)
	}
	if v := a.account(alice); v.Email == "" {
		t.Fatal("nothing may happen before the password is right")
	}
	res := a.deleteAccount(alice, memberPassword, "")
	if res.status != http.StatusNoContent {
		t.Fatalf("delete: %d %s", res.status, res.body)
	}
	if !strings.Contains(res.header.Get("Set-Cookie"), "Max-Age=0") && !strings.Contains(res.header.Get("Set-Cookie"), "Max-Age=-1") {
		t.Fatalf("the session cookie must be cleared: %q", res.header.Get("Set-Cookie"))
	}
	if me := alice.get("/api/auth/me"); me.status != http.StatusUnauthorized {
		t.Fatalf("session after deletion: %d", me.status)
	}
	if got := a.password(a.anon(), "alice", memberPassword); got.status != http.StatusUnauthorized {
		t.Fatalf("sign-in after deletion: %d", got.status)
	}

	// The thread stays readable; the words are gone.
	var found bool
	for _, m := range a.listMessages(bob, ch) {
		if m.UserID != aliceID {
			if m.ReplyTo != nil && m.ReplyTo.Content != "" && strings.Contains(m.ReplyTo.Content, "alice") {
				t.Fatalf("a quote must not keep her words: %+v", m)
			}
			continue
		}
		found = true
		if m.Content != "" || m.DisplayName != "Deleted user" || !strings.HasPrefix(m.Username, "deleted-") || strings.Contains(m.Username, "alice") {
			t.Fatalf("tombstone: %+v", m)
		}
	}
	if !found {
		t.Fatal("her messages must remain as tombstones")
	}
	if n := a.countRows(`SELECT COUNT(*) FROM messages WHERE user_id = $1 AND (content <> '' OR is_pinned OR embeds <> '[]'::jsonb)`, aliceID); n != 0 {
		t.Fatalf("content left in %d messages", n)
	}
	if n := a.countRows(`SELECT COUNT(*) FROM messages WHERE id = $1 AND content = 'bob''s own words'`, bobMsg); n != 1 {
		t.Fatal("other members' messages stay untouched")
	}
	_ = aliceMsg

	// Her files are gone from storage and from the database.
	for _, k := range files {
		if _, err := a.store.GetObjectFrom(context.Background(), k, 0); err == nil {
			t.Errorf("object %s still stored", k)
		}
	}
	// She is not a member any more, and her name is free again.
	if body := bob.get("/api/members").body; strings.Contains(string(body), "alice") || strings.Contains(string(body), "Alice Real") {
		t.Fatalf("member list: %s", body)
	}
	if res := a.registerAs(admin, "alice"); res != http.StatusCreated {
		t.Fatalf("the username must be free again: %d", res)
	}
	if n := a.countRows(`SELECT COUNT(*) FROM audit_log WHERE action = 'auth.account_delete'`); n != 1 {
		t.Fatalf("the deletion is audited once: %d", n)
	}
}

// registerAs makes a fresh invite and registers name; it returns the status.
func (a *app) registerAs(admin *client, name string) int {
	a.t.Helper()
	inv := admin.post("/api/admin/invites", map[string]any{})
	var invite struct {
		Code string `json:"code"`
	}
	inv.decode(a.t, &invite)
	return a.anon().post("/api/auth/register", map[string]string{"username": name, "password": memberPassword, "invite_code": invite.Code}).status
}

func TestDeletionLeavesNoPersonalDataInAnyTable(t *testing.T) {
	a, _ := mailApp(t)
	admin := a.seedAdmin()
	alice := a.register(admin, "alice")
	bob := a.register(admin, "bob")
	ch := a.createChannel(admin, "general", "text")
	a.populate(admin, alice, bob, ch)
	alice = a.login("alice", memberPassword)
	e := a.enroll(alice, memberPassword)
	alice.post("/api/users/me/export", map[string]string{"current_password": memberPassword, "code": ""})
	id := alice.user.ID

	// Rows must exist before, or the assertion below proves nothing.
	var before int
	for _, fk := range a.foreignKeysToUsers() {
		before += a.countRows(`SELECT COUNT(*) FROM `+fk[0]+` WHERE `+fk[1]+` = $1`, id)
	}
	if before < 12 {
		t.Fatalf("the fixture is too small to prove anything: %d rows", before)
	}

	res := a.deleteAccount(alice, memberPassword, "")
	if res.status != http.StatusForbidden {
		t.Fatalf("with two-factor on, the code is needed: %d", res.status)
	}
	a.freshStep()
	if res := a.deleteAccount(alice, memberPassword, e.auth.code(1)); res.status != http.StatusNoContent {
		t.Fatalf("delete: %d %s", res.status, res.body)
	}

	// Per table: nothing refers to the person any more, but their tombstone
	// (users) and the authorship of their (emptied) messages.
	keep := map[string]bool{"users": true, "messages": true}
	for _, fk := range a.foreignKeysToUsers() {
		table := strings.TrimPrefix(fk[0], "public.")
		if keep[table] && fk[1] != "banned_by" && fk[1] != "timeout_by" {
			continue
		}
		if n := a.countRows(`SELECT COUNT(*) FROM `+fk[0]+` WHERE `+fk[1]+` = $1`, id); n != 0 {
			t.Errorf("%s.%s still refers to the deleted account in %d rows", fk[0], fk[1], n)
		}
	}
	// The tombstone holds nothing personal.
	var username, display, bio, status, locale, hash string
	var email, avatar *string
	var deleted, disabled bool
	err := a.db.QueryRow(context.Background(), `SELECT username, display_name, bio, status_text, locale, password_hash, email, avatar_s3_key,
		deleted_at IS NOT NULL, disabled_at IS NOT NULL FROM users WHERE id = $1`, id).
		Scan(&username, &display, &bio, &status, &locale, &hash, &email, &avatar, &deleted, &disabled)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(username, "deleted-") || display != "Deleted user" || bio != "" || status != "" || locale != "" ||
		email != nil || avatar != nil || !deleted || !disabled || strings.HasPrefix(hash, "$2") {
		t.Fatalf("tombstone: %s %s %q %q %q %v %v", username, display, bio, status, hash, email, avatar)
	}
	// No audit entry names her or carries details that mention her.
	for _, needle := range []string{"alice", "Alice Real", "alice@example.org", id.String()} {
		q := `SELECT COUNT(*) FROM audit_log WHERE actor_name ILIKE '%' || $1 || '%' OR target_name ILIKE '%' || $1 || '%'`
		if needle == id.String() {
			q = `SELECT COUNT(*) FROM audit_log WHERE details::text LIKE '%' || $1 || '%'`
		}
		if n := a.countRows(q, needle); n != 0 {
			t.Errorf("%d audit entries still hold %q", n, needle)
		}
	}
	if n := a.countRows(`SELECT COUNT(*) FROM audit_log WHERE actor_id = $1 OR target_id = $1::text`, id); n == 0 {
		t.Error("the entries themselves (action, time, id) stay, only names and details go")
	}
}

func TestTheOwnerAccountCannotBeDeleted(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	res := a.deleteAccount(admin, ownerPassword, "")
	if res.status != http.StatusForbidden || !strings.Contains(string(res.body), "Owner") {
		t.Fatalf("%d %s", res.status, res.body)
	}
	if me := admin.get("/api/auth/me"); me.status != http.StatusOK {
		t.Fatal("the Owner stays signed in")
	}
}

func TestDeletedPlaceholderNamesCannotBeRegistered(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	if got := a.registerAs(admin, "deleted-abcdef123456"); got != http.StatusBadRequest {
		t.Fatalf("%d", got)
	}
}
