//go:build integration

package chat

import (
	"context"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/testutil"
)

func slugWorld(t *testing.T) (context.Context, *db.Pool) {
	t.Helper()
	p := testutil.DB(t)
	testutil.Reset(t, p)
	return context.Background(), p
}

func mustCreate(t *testing.T, ctx context.Context, p *db.Pool, name string, typ ChannelType) *Channel {
	t.Helper()
	ch, err := CreateChannel(ctx, p, nil, name, typ, "", 0)
	if err != nil {
		t.Fatalf("create %q: %v", name, err)
	}
	return ch
}

func mustRename(t *testing.T, ctx context.Context, p *db.Pool, id uuid.UUID, name string) *Channel {
	t.Helper()
	ch, err := UpdateChannel(ctx, p, id, &name, nil, nil, nil)
	if err != nil {
		t.Fatalf("rename %q: %v", name, err)
	}
	return ch
}

func requireSlug(t *testing.T, ch *Channel, slug string, previous ...string) {
	t.Helper()
	if previous == nil {
		previous = []string{}
	}
	if ch.Slug != slug || !reflect.DeepEqual(ch.PreviousSlugs, previous) {
		t.Fatalf("slug = %q previous = %v; want %q %v", ch.Slug, ch.PreviousSlugs, slug, previous)
	}
}

func TestChannelSlugsFollowNamesAndKeepHistory(t *testing.T) {
	ctx, p := slugWorld(t)

	general := mustCreate(t, ctx, p, "General", ChannelTypeText)
	requireSlug(t, general, "general")
	requireSlug(t, mustCreate(t, ctx, p, "general", ChannelTypeVoice), "general-2")
	requireSlug(t, mustCreate(t, ctx, p, "Größe", ChannelTypeForum), "groesse")
	requireSlug(t, mustCreate(t, ctx, p, "🎮", ChannelTypeText), "channel")

	// A rename that keeps the slug fitting does not grow the history.
	requireSlug(t, mustRename(t, ctx, p, general.ID, "GENERAL!"), "general")

	renamed := mustRename(t, ctx, p, general.ID, "Lobby")
	requireSlug(t, renamed, "lobby", "general")

	// The freed slug is not handed to another channel: old links never move.
	requireSlug(t, mustCreate(t, ctx, p, "General", ChannelTypeText), "general-3")

	// Renaming back takes the channel's own old slug again.
	requireSlug(t, mustRename(t, ctx, p, general.ID, "general"), "general", "lobby")

	var seen *Channel
	_, uncategorized, err := GetServerHierarchy(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	for i := range uncategorized {
		if uncategorized[i].ID == general.ID {
			seen = &uncategorized[i]
		}
	}
	if seen == nil {
		t.Fatal("renamed channel missing from the hierarchy")
	}
	requireSlug(t, seen, "general", "lobby")
}

func TestChannelSlugsGoWithTheChannel(t *testing.T) {
	ctx, p := slugWorld(t)
	a := mustCreate(t, ctx, p, "Lounge", ChannelTypeVoice)
	mustRename(t, ctx, p, a.ID, "Hall")
	if _, err := DeleteChannel(ctx, p, a.ID); err != nil {
		t.Fatal(err)
	}
	var left int
	if err := p.QueryRow(ctx, `SELECT count(*) FROM channel_slugs WHERE channel_id = $1`, a.ID).Scan(&left); err != nil || left != 0 {
		t.Fatalf("slug rows left = %d, err %v", left, err)
	}
	// With the channel gone its slugs are free again.
	requireSlug(t, mustCreate(t, ctx, p, "Lounge", ChannelTypeVoice), "lounge")
}

func TestChannelSlugsStayUniqueUnderConcurrentCreates(t *testing.T) {
	ctx, p := slugWorld(t)
	const n = 8
	var wg sync.WaitGroup
	slugs := make([]string, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if ch, err := CreateChannel(ctx, p, nil, "Same", ChannelTypeText, "", i); err == nil {
				slugs[i] = ch.Slug
			}
		}()
	}
	wg.Wait()
	seen := map[string]bool{}
	for _, s := range slugs {
		if s == "" || seen[s] {
			t.Fatalf("slugs not unique or missing: %v", slugs)
		}
		seen[s] = true
	}
}

func TestDuplicatedAndTemporaryChannelsGetSlugs(t *testing.T) {
	ctx, p := slugWorld(t)
	lobby := mustCreate(t, ctx, p, "Create a Talk", ChannelTypeVoice)
	if err := SetLobby(ctx, p, lobby.ID, true); err != nil {
		t.Fatal(err)
	}
	leader := uuid.New()
	if _, err := p.Exec(ctx, `INSERT INTO users (id, username, display_name, password_hash) VALUES ($1, 'slugleader', 'L', 'x')`, leader); err != nil {
		t.Fatal(err)
	}
	talk, err := OpenTempTalk(ctx, p, lobby.ID, leader)
	if err != nil {
		t.Fatal(err)
	}
	requireSlug(t, talk, "talk-1")
	if strings.Contains(talk.Slug, lobby.ID.String()) {
		t.Fatal("slug leaks an id")
	}
}
