//go:build integration

package chat

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/google/uuid"
)

func TestTalksAreNamedAfterTheLobbyAndKeepTheirNameOnRename(t *testing.T) {
	w := newTalksWorld(t)
	ctx := context.Background()
	nameOf := func(id uuid.UUID) string {
		var n string
		if err := w.p.QueryRow(ctx, `SELECT name FROM channels WHERE id = $1`, id).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	first, err := w.mgr.Open(ctx, w.alice.ID, w.lobby.ID)
	if err != nil {
		t.Fatal(err)
	}
	renamed := "Study room"
	if _, err := UpdateChannel(ctx, w.p, w.lobby.ID, &renamed, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	second, err := w.mgr.Open(ctx, w.bob.ID, w.lobby.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := nameOf(first.ID); got != "Create a Talk 1" {
		t.Fatalf("an open Talk keeps its name, got %q", got)
	}
	if got := nameOf(second.ID); got != "Study room 1" {
		t.Fatalf("new Talk after the rename = %q; want Study room 1", got)
	}
	third, err := w.mgr.Open(ctx, w.alice.ID, w.lobby.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := nameOf(third.ID); got != "Study room 2" {
		t.Fatalf("got %q; want Study room 2", got)
	}
}

func TestTalkNameOfALongLobbyFitsTheLimit(t *testing.T) {
	w := newTalksWorld(t)
	ctx := context.Background()
	long := strings.Repeat("\u00e4", MaxChannelNameLen)
	if _, err := UpdateChannel(ctx, w.p, w.lobby.ID, &long, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	ch, err := w.mgr.Open(ctx, w.alice.ID, w.lobby.ID)
	if err != nil {
		t.Fatal(err)
	}
	var name string
	if err := w.p.QueryRow(ctx, `SELECT name FROM channels WHERE id = $1`, ch.ID).Scan(&name); err != nil {
		t.Fatal(err)
	}
	if n := utf8.RuneCountInString(name); n > MaxChannelNameLen || !strings.HasSuffix(name, " 1") {
		t.Fatalf("name %q has %d runes", name, n)
	}
}
