//go:build integration

package server

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/perm"
)

type notifySettings struct {
	Channels []struct {
		ChannelID  uuid.UUID  `json:"channel_id"`
		Level      string     `json:"level"`
		MutedUntil *time.Time `json:"muted_until"`
	} `json:"channels"`
	Categories []struct {
		CategoryID uuid.UUID  `json:"category_id"`
		Level      string     `json:"level"`
		MutedUntil *time.Time `json:"muted_until"`
	} `json:"categories"`
}

func newCategory(t *testing.T, owner *client, name string) uuid.UUID {
	t.Helper()
	res := owner.post("/api/admin/categories", map[string]any{"name": name})
	wantStatus(t, "create category", res, http.StatusCreated)
	var cat struct{ ID uuid.UUID }
	res.decode(t, &cat)
	return cat.ID
}

func channelIn(t *testing.T, owner *client, name string, cat uuid.UUID) uuid.UUID {
	t.Helper()
	res := owner.post("/api/admin/channels", map[string]any{"name": name, "category_id": cat})
	wantStatus(t, "create channel", res, http.StatusCreated)
	var ch struct{ ID uuid.UUID }
	res.decode(t, &ch)
	return ch.ID
}

func TestChannelAndCategoryNotificationLevels(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	cat := newCategory(t, admin, "Music")
	one := channelIn(t, admin, "one", cat)
	two := channelIn(t, admin, "two", cat)
	catPath := "/api/categories/" + cat.String() + "/notifications"

	// The category decides for channels without a setting of their own.
	wantStatus(t, "category mute", admin.put(catPath, map[string]any{"level": "mute"}), http.StatusNoContent)
	for _, ch := range []uuid.UUID{one, two} {
		if got := admin.readState(t, ch); got.NotifyLevel != "mute" {
			t.Fatalf("inherited level %q, want mute", got.NotifyLevel)
		}
	}
	// Reading a channel must not pin it to "all".
	wantStatus(t, "read", admin.post("/api/channels/"+one.String()+"/read", nil), http.StatusNoContent)
	if got := admin.readState(t, one); got.NotifyLevel != "mute" {
		t.Fatalf("after read %q, want mute", got.NotifyLevel)
	}
	// An own setting wins; inherit hands the decision back.
	own := "/api/channels/" + one.String() + "/notifications"
	wantStatus(t, "own", admin.put(own, map[string]any{"level": "mentions"}), http.StatusNoContent)
	if got := admin.readState(t, one); got.NotifyLevel != "mentions" {
		t.Fatalf("own level %q, want mentions", got.NotifyLevel)
	}
	wantStatus(t, "inherit", admin.put(own, map[string]any{"level": "inherit"}), http.StatusNoContent)
	if got := admin.readState(t, one); got.NotifyLevel != "mute" {
		t.Fatalf("inherited again %q, want mute", got.NotifyLevel)
	}

	var s notifySettings
	admin.get("/api/notification-settings").decode(t, &s)
	if len(s.Categories) != 1 || s.Categories[0].Level != "mute" || len(s.Channels) != 0 {
		t.Fatalf("settings: %+v", s)
	}
}

func TestTimedMuteValidationAndExpiry(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	ch := a.createChannel(admin, "timed", "text")
	path := "/api/channels/" + ch.String() + "/notifications"
	at := func(d time.Duration) map[string]any {
		return map[string]any{"level": "mute", "muted_until": time.Now().Add(d).UTC().Format(time.RFC3339)}
	}

	wantStatus(t, "past", admin.put(path, at(-time.Minute)), http.StatusBadRequest)
	wantStatus(t, "too far", admin.put(path, at(400*24*time.Hour)), http.StatusBadRequest)
	wantStatus(t, "not a mute", admin.put(path, map[string]any{"level": "all", "muted_until": time.Now().Add(time.Hour).UTC().Format(time.RFC3339)}), http.StatusBadRequest)
	wantStatus(t, "ok", admin.put(path, at(time.Hour)), http.StatusNoContent)
	if got := admin.readState(t, ch); got.NotifyLevel != "mute" {
		t.Fatalf("level %q, want mute", got.NotifyLevel)
	}
	var s notifySettings
	admin.get("/api/notification-settings").decode(t, &s)
	if len(s.Channels) != 1 || s.Channels[0].MutedUntil == nil {
		t.Fatalf("settings: %+v", s)
	}

	// The mute has run out: unmuted, and no longer listed.
	if _, err := a.db.Exec(context.Background(), `UPDATE channel_reads SET muted_until = NOW() - interval '1 minute'`); err != nil {
		t.Fatal(err)
	}
	if got := admin.readState(t, ch); got.NotifyLevel != "all" {
		t.Fatalf("expired mute: level %q, want all", got.NotifyLevel)
	}
	s = notifySettings{}
	admin.get("/api/notification-settings").decode(t, &s)
	if len(s.Channels) != 0 {
		t.Fatalf("expired mute listed: %+v", s)
	}
}

func TestReadAllOnlyTouchesViewableChannelsAndHiddenCategoryIsRefused(t *testing.T) {
	f := newAccessFixture(t, false)
	cat := newCategory(t, f.owner, "Staff area")
	secret := channelIn(t, f.owner, "secret", cat)
	open := f.a.createChannel(f.owner, "open", "text")
	wantStatus(t, "hide category", putOverwrites(f.owner, "categories", cat,
		overwrite("role", f.everyone, 0, perm.ViewChannel),
		overwrite("role", f.staff, perm.ViewChannel, 0)), http.StatusNoContent)
	f.a.send(f.owner, secret, "hidden news")
	f.a.send(f.owner, open, "open news")

	wantStatus(t, "read all", f.outsider.post("/api/read-state/read-all", nil), http.StatusNoContent)
	if got := f.outsider.readState(t, open); got.UnreadCount != 0 {
		t.Fatalf("open channel still unread: %+v", got)
	}
	var rows int
	if err := f.a.db.QueryRow(context.Background(),
		`SELECT count(*) FROM channel_reads cr JOIN users u ON u.id = cr.user_id WHERE u.username = 'outsider' AND cr.channel_id = $1`, secret).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 0 {
		t.Fatalf("read-all wrote %d rows for a channel the caller cannot view", rows)
	}
	if got := f.insider.readState(t, secret); got.UnreadCount != 1 {
		t.Fatalf("insider's unread = %d, want 1", got.UnreadCount)
	}

	hidden := "/api/categories/" + cat.String() + "/notifications"
	wantStatus(t, "hidden category", f.outsider.put(hidden, map[string]any{"level": "mute"}), http.StatusNotFound)
	wantStatus(t, "read-all in hidden category", f.outsider.post("/api/read-state/read-all", map[string]any{"category_id": cat}), http.StatusNotFound)
	wantStatus(t, "unknown category", f.outsider.put("/api/categories/"+uuid.NewString()+"/notifications", map[string]any{"level": "mute"}), http.StatusNotFound)
	wantStatus(t, "insider", f.insider.put(hidden, map[string]any{"level": "mentions"}), http.StatusNoContent)

	var s notifySettings
	f.outsider.get("/api/notification-settings").decode(t, &s)
	if len(s.Categories) != 0 {
		t.Fatalf("outsider sees category settings: %+v", s)
	}
	wantStatus(t, "insider read-all in category", f.insider.post("/api/read-state/read-all", map[string]any{"category_id": cat}), http.StatusNoContent)
	if got := f.insider.readState(t, secret); got.UnreadCount != 0 {
		t.Fatalf("insider still unread: %+v", got)
	}
}
