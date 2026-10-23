//go:build integration

package server

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/perm"
)

type exportStatus struct {
	State         string     `json:"state"`
	ExpiresAt     *time.Time `json:"expires_at"`
	NextRequestAt *time.Time `json:"next_request_at"`
}

func (a *app) exportStatus(c *client) exportStatus {
	a.t.Helper()
	res := c.get("/api/users/me/export")
	if res.status != http.StatusOK {
		a.t.Fatalf("export status: %d %s", res.status, res.body)
	}
	var st exportStatus
	res.decode(a.t, &st)
	return st
}

func (a *app) waitForExport(c *client) exportStatus {
	a.t.Helper()
	for i := 0; i < 200; i++ {
		if st := a.exportStatus(c); st.State != "pending" {
			return st
		}
		time.Sleep(25 * time.Millisecond)
	}
	a.t.Fatal("the export never finished")
	return exportStatus{}
}

func unzipAll(t *testing.T, data []byte) map[string]string {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(rc)
		_ = rc.Close()
		out[f.Name] = string(b)
	}
	return out
}

func (a *app) requestExport(c *client, password, code string) response {
	return c.post("/api/users/me/export", map[string]string{"current_password": password, "code": code})
}

func TestAnExportHoldsOnlyTheMembersOwnData(t *testing.T) {
	a, _ := mailApp(t)
	admin := a.seedAdmin()
	alice := a.register(admin, "alice")
	bob := a.register(admin, "bob")
	open := a.createChannel(admin, "general", "text")
	hidden := a.createChannel(admin, "backroom", "text")
	aliceOpen := a.send(alice, open, "alice's first wording")
	a.send(alice, hidden, "alice in the backroom")
	bobMsg := a.send(bob, open, "bob's earlier plan")
	// Both edit; the export holds the earlier version of hers only.
	for c, edit := range map[*client][2]string{alice: {aliceOpen.String(), "alice in the open"}, bob: {bobMsg.String(), "bob's secret plan"}} {
		if res := c.put("/api/channels/"+open.String()+"/messages/"+edit[0], map[string]any{"content": edit[1]}); res.status != http.StatusOK {
			t.Fatalf("edit: %d %s", res.status, res.body)
		}
	}
	alice.post("/api/messages/"+bobMsg.String()+"/reactions", map[string]string{"emoji": "👍"})
	admin.post("/api/admin/users/"+alice.user.ID.String()+"/disable", nil)
	admin.post("/api/admin/users/"+alice.user.ID.String()+"/enable", nil)
	alice = a.login("alice", memberPassword)
	a.setEmail(alice, "alice@example.org")
	// Alice loses sight of the backroom after writing there.
	everyone := everyoneID(t, admin)
	if res := putOverwrites(admin, "channels", hidden, overwrite("role", everyone, 0, perm.ViewChannel)); res.status != http.StatusNoContent {
		t.Fatalf("hide: %d", res.status)
	}
	ctx := context.Background()
	if _, err := a.db.Exec(ctx, `INSERT INTO media (uploader_id, channel_id, s3_bucket, s3_key, original_filename, mime_type, size_bytes)
		VALUES ($1, $2, 'b', 'uploads/export-test.png', 'holiday.png', 'image/png', 10), ($1, $2, 'b', 'uploads/gone.png', 'gone.png', 'image/png', 10)`, alice.user.ID, open); err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Exec(ctx, `UPDATE media SET is_deleted = TRUE WHERE s3_key = 'uploads/gone.png'`); err != nil {
		t.Fatal(err)
	}
	var category string
	if err := a.db.QueryRow(ctx, `INSERT INTO categories (name) VALUES ('Lounge') RETURNING id`).Scan(&category); err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Exec(ctx, `INSERT INTO category_notify (user_id, category_id, notify_level) VALUES ($1, $2, 'mute')`, alice.user.ID, category); err != nil {
		t.Fatal(err)
	}
	alice.put("/api/channels/"+open.String()+"/notifications", map[string]any{"level": "mentions"})
	before := a.store.Len()

	if res := a.requestExport(alice, "wrong-password", ""); res.status != http.StatusForbidden {
		t.Fatalf("export needs the password: %d", res.status)
	}
	if st := a.exportStatus(alice); st.State != "none" {
		t.Fatalf("nothing is built for a wrong password: %+v", st)
	}
	res := a.requestExport(alice, memberPassword, "")
	if res.status != http.StatusAccepted {
		t.Fatalf("request: %d %s", res.status, res.body)
	}
	st := a.waitForExport(alice)
	if st.State != "ready" || st.ExpiresAt == nil || time.Until(*st.ExpiresAt) > 24*time.Hour+time.Minute || time.Until(*st.ExpiresAt) < 23*time.Hour {
		t.Fatalf("status: %+v", st)
	}

	dl := alice.get("/api/users/me/export/download")
	if dl.status != http.StatusOK || dl.header.Get("Content-Type") != "application/zip" || !strings.Contains(dl.header.Get("Content-Disposition"), "attachment") {
		t.Fatalf("download: %d %v", dl.status, dl.header)
	}
	files := unzipAll(t, dl.body)
	for _, name := range []string{"README.txt", "profile.json", "settings.json", "messages.json", "edits.json", "reactions.json", "uploads.json", "audit.json", "security.json"} {
		if _, ok := files[name]; !ok {
			t.Errorf("missing %s", name)
		}
	}
	if !strings.Contains(files["README.txt"], "meant for machines") {
		t.Errorf("the README says the export is English and machine-readable: %s", files["README.txt"])
	}
	if !strings.Contains(files["edits.json"], "alice's first wording") || strings.Contains(files["edits.json"], "earlier plan") {
		t.Errorf("her own earlier versions, and nobody else's: %s", files["edits.json"])
	}
	var msgs struct {
		Truncated bool `json:"truncated"`
		Messages  []struct {
			Content     string  `json:"content"`
			ChannelName *string `json:"channel_name"`
		} `json:"messages"`
	}
	if err := json.Unmarshal([]byte(files["messages.json"]), &msgs); err != nil || len(msgs.Messages) != 2 {
		t.Fatalf("messages: %v %s", err, files["messages.json"])
	}
	for _, m := range msgs.Messages {
		visible := m.ChannelName != nil
		if (m.Content == "alice in the open") != visible || (m.Content == "alice in the backroom" && visible) {
			t.Errorf("channel names only for channels she can see: %+v", m)
		}
	}
	if !strings.Contains(files["uploads.json"], "holiday.png") || !strings.Contains(files["uploads.json"], "/api/media/") ||
		!strings.Contains(files["settings.json"], "mute") || !strings.Contains(files["settings.json"], "mentions") {
		t.Errorf("uploads and settings: %s %s", files["uploads.json"], files["settings.json"])
	}
	if !strings.Contains(files["profile.json"], "alice@example.org") {
		t.Error("her own address belongs in her export")
	}

	// Nothing of anybody else, anywhere in the archive.
	for name, body := range files {
		for _, other := range []string{"bob", "Herzog", "secret plan", bob.user.ID.String(), admin.user.ID.String()} {
			if strings.Contains(strings.ToLower(body), strings.ToLower(other)) {
				t.Errorf("%s names or quotes another member: %q", name, other)
			}
		}
	}

	// Once: the file is taken, deleted, and a second download finds nothing.
	if again := alice.get("/api/users/me/export/download"); again.status != http.StatusNotFound {
		t.Fatalf("second download: %d", again.status)
	}
	if a.store.Len() != before {
		t.Fatalf("the archive must be removed after the download: %d vs %d", a.store.Len(), before)
	}
	if st := a.exportStatus(alice); st.State != "downloaded" {
		t.Fatalf("status after download: %+v", st)
	}
}

func TestOneExportPerDayAndNoOneElsesFile(t *testing.T) {
	a, _ := mailApp(t)
	admin := a.seedAdmin()
	alice := a.register(admin, "alice")
	bob := a.register(admin, "bob")
	if res := a.requestExport(alice, memberPassword, ""); res.status != http.StatusAccepted {
		t.Fatalf("%d", res.status)
	}
	again := a.requestExport(alice, memberPassword, "")
	if again.status != http.StatusTooManyRequests || again.header.Get("Retry-After") == "" {
		t.Fatalf("a second request: %d %v", again.status, again.header)
	}
	a.waitForExport(alice)
	if res := bob.get("/api/users/me/export/download"); res.status != http.StatusNotFound {
		t.Fatalf("another member's download: %d", res.status)
	}
	if st := a.exportStatus(bob); st.State != "none" {
		t.Fatalf("bob sees %+v", st)
	}
	// Parallel requests cannot both pass the daily limit.
	carol := a.register(admin, "carol")
	results := make(chan int, 6)
	for i := 0; i < 6; i++ {
		go func() { results <- a.requestExport(carol, memberPassword, "").status }()
	}
	accepted := 0
	for i := 0; i < 6; i++ {
		if <-results == http.StatusAccepted {
			accepted++
		}
	}
	if accepted != 1 {
		t.Fatalf("%d exports accepted at once", accepted)
	}
}

func TestExportNeedsTheSecondFactorAndExpires(t *testing.T) {
	a, _ := mailApp(t)
	admin := a.seedAdmin()
	alice := a.register(admin, "alice")
	e := a.enroll(alice, memberPassword)
	a.freshStep()

	if res := a.requestExport(alice, memberPassword, ""); res.status != http.StatusForbidden {
		t.Fatalf("the code is needed: %d", res.status)
	}
	if res := a.requestExport(alice, memberPassword, e.auth.code(1)); res.status != http.StatusAccepted {
		t.Fatalf("with the code: %d %s", res.status, res.body)
	}
	a.waitForExport(alice)
	if _, err := a.db.Exec(context.Background(), `UPDATE data_exports SET expires_at = NOW() - INTERVAL '1 minute'`); err != nil {
		t.Fatal(err)
	}
	if res := alice.get("/api/users/me/export/download"); res.status != http.StatusNotFound {
		t.Fatalf("an expired export: %d", res.status)
	}
	if st := a.exportStatus(alice); st.State != "expired" {
		t.Fatalf("%+v", st)
	}
	// The sweeper removes the file.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go a.router.PreviewDeps().Accounts.RunExportSweep(ctx, 20*time.Millisecond)
	deadline := time.Now().Add(3 * time.Second)
	for a.store.Len() != 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if a.store.Len() != 0 {
		t.Fatal("the expired file must be swept")
	}
}

func TestExportNeedsObjectStorage(t *testing.T) {
	a := newAppWithDeps(t, false, nil, func(d *Deps) { d.Store = nil })
	alice := a.register(a.seedAdmin(), "alice")
	if res := a.requestExport(alice, memberPassword, ""); res.status != http.StatusServiceUnavailable {
		t.Fatalf("%d", res.status)
	}
	if res := alice.get("/api/users/me/export/download"); res.status != http.StatusServiceUnavailable {
		t.Fatalf("%d", res.status)
	}
}

func TestExportOfADeletedAccountIsGone(t *testing.T) {
	a, _ := mailApp(t)
	admin := a.seedAdmin()
	alice := a.register(admin, "alice")
	a.requestExport(alice, memberPassword, "")
	a.waitForExport(alice)
	if a.store.Len() == 0 {
		t.Fatal("the export file should exist")
	}
	if res := a.deleteAccount(alice, memberPassword, ""); res.status != http.StatusNoContent {
		t.Fatalf("%d", res.status)
	}
	if a.store.Len() != 0 {
		t.Fatalf("the export file must go with the account: %d objects", a.store.Len())
	}
}

func TestAnExportCoversPollsPinsPushDevicesAndEmojiOfTheMember(t *testing.T) {
	a, _ := mailApp(t)
	admin := a.seedAdmin()
	alice := a.register(admin, "alice")
	bob := a.register(admin, "bob")
	assignRoles(t, admin, alice, createRole(t, admin, "helpers", perm.ManageMessages|perm.ManageEmoji))
	alice = a.login("alice", memberPassword)
	ch := a.createChannel(admin, "general", "text")

	// A poll of hers that bob voted in, a message of bob's she pinned, a push device and an emoji.
	res := alice.post("/api/channels/"+ch.String()+"/polls", map[string]any{"question": "Pizza or pasta?", "options": []string{"Pizza", "Pasta"}, "multiple": true})
	if res.status != http.StatusCreated {
		t.Fatalf("poll: %d %s", res.status, res.body)
	}
	var pollMsg struct {
		ID   string `json:"id"`
		Poll struct {
			Options []struct {
				ID string `json:"id"`
			} `json:"options"`
		} `json:"poll"`
	}
	res.decode(t, &pollMsg)
	if r := bob.put("/api/channels/"+ch.String()+"/messages/"+pollMsg.ID+"/poll/votes", map[string]any{"option_ids": []string{pollMsg.Poll.Options[0].ID}}); r.status != http.StatusOK {
		t.Fatalf("vote: %d %s", r.status, r.body)
	}
	bobMsg := a.send(bob, ch, "bob's pinned wording")
	if r := alice.do(http.MethodPut, "/api/channels/"+ch.String()+"/messages/"+bobMsg.String()+"/pin", nil, nil); r.status != http.StatusOK {
		t.Fatalf("pin: %d %s", r.status, r.body)
	}
	ctx := context.Background()
	if _, err := a.db.Exec(ctx, `INSERT INTO push_subscriptions (user_id, endpoint, p256dh, auth, label) VALUES ($1, 'https://push.example.org/secret-endpoint', 'device-key-one', 'device-key-two', 'Alice phone')`, alice.user.ID); err != nil {
		t.Fatal(err)
	}
	var emoji struct {
		ID uuid.UUID `json:"id"`
	}
	uploadEmoji(alice, "party", "x.png", "image/png", squarePNG(t, 32)).decode(t, &emoji)

	if r := a.requestExport(alice, memberPassword, ""); r.status != http.StatusAccepted {
		t.Fatalf("request: %d %s", r.status, r.body)
	}
	a.waitForExport(alice)
	files := unzipAll(t, alice.get("/api/users/me/export/download").body)

	for _, name := range []string{"polls.json", "pins.json", "push.json", "emoji.json", "emoji/party.png"} {
		if _, ok := files[name]; !ok {
			t.Errorf("missing %s", name)
		}
	}
	var polls []struct {
		Question  string `json:"question"`
		Multiple  bool   `json:"multiple"`
		Anonymous bool   `json:"anonymous"`
		ClosesAt  string `json:"closes_at"`
		Options   []struct {
			Label string `json:"label"`
			Votes int    `json:"votes"`
		} `json:"options"`
	}
	if err := json.Unmarshal([]byte(files["polls.json"]), &polls); err != nil || len(polls) != 1 {
		t.Fatalf("polls: %v %s", err, files["polls.json"])
	}
	if p := polls[0]; p.Question != "Pizza or pasta?" || !p.Multiple || p.Anonymous || p.ClosesAt == "" ||
		len(p.Options) != 2 || p.Options[0].Label != "Pizza" || p.Options[0].Votes != 1 || p.Options[1].Votes != 0 {
		t.Errorf("poll: %+v", p)
	}
	if !strings.Contains(files["pins.json"], bobMsg.String()) || strings.Contains(files["pins.json"], "wording") {
		t.Errorf("pins name the message but never its text: %s", files["pins.json"])
	}
	if !strings.Contains(files["push.json"], "Alice phone") || !strings.Contains(files["push.json"], `"show_message_text": true`) {
		t.Errorf("push devices: %s", files["push.json"])
	}
	if !strings.Contains(files["emoji.json"], `"name": "party"`) || !strings.Contains(files["emoji.json"], "emoji/party.png") ||
		!strings.HasPrefix(files["emoji/party.png"], "\x89PNG") {
		t.Errorf("emoji: %s", files["emoji.json"])
	}
	// No secret of the device and nothing of bob, anywhere in the new files.
	for _, name := range []string{"polls.json", "pins.json", "push.json", "emoji.json"} {
		for _, secret := range []string{"secret-endpoint", "device-key-one", "device-key-two", "bob", bob.user.ID.String()} {
			if strings.Contains(strings.ToLower(files[name]), strings.ToLower(secret)) {
				t.Errorf("%s leaks %q", name, secret)
			}
		}
	}
}
