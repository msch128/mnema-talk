//go:build integration

package chat

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

// A message that reaches the member several ways (by name, through a role,
// as a reply to their message) counts once in the mention and unread counts.
// message_mentions has one row per (message, user) today; this guards the
// counts should that ever change.
func TestReadStatesCountAMessageAddressedSeveralWaysOnce(t *testing.T) {
	ctx := context.Background()
	f := newChatFixture(t)
	var other uuid.UUID
	if err := f.p.QueryRow(ctx, `INSERT INTO users (username, display_name, password_hash) VALUES ('rs_other','Other','test-only-unused') RETURNING id`).Scan(&other); err != nil {
		t.Fatal(err)
	}
	var role uuid.UUID
	if err := f.p.QueryRow(ctx, `INSERT INTO roles (name, position) VALUES ('rs-team', 1) RETURNING id`).Scan(&role); err != nil {
		t.Fatal(err)
	}
	// f.m is the member's own message; the other author replies to it and
	// mentions the member by name and through the role.
	reply, err := CreateMessage(ctx, f.p, NewMessage{ChannelID: f.c.ID, UserID: other, Content: "@coverage_member @rs-team", ReplyToID: &f.m})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := CreateMessage(ctx, f.p, NewMessage{ChannelID: f.c.ID, UserID: other, Content: "plain"}); err != nil {
		t.Fatal(err)
	}
	set := MentionSet{
		Users: []MentionedUser{{ID: f.u.ID, Kind: MentionKindName}, {ID: f.u.ID, Kind: MentionKindRole}},
		Roles: []uuid.UUID{role},
	}
	if err := saveMentionSet(ctx, f.p, reply, set); err != nil {
		t.Fatal(err)
	}

	states, err := GetReadStates(ctx, f.p, f.u)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range states {
		if s.ChannelID != f.c.ID {
			continue
		}
		if s.UnreadCount != 2 || s.MentionCount != 1 {
			t.Fatalf("unread %d, mentions %d; want 2 and 1", s.UnreadCount, s.MentionCount)
		}
		return
	}
	t.Fatal("channel missing from the read states")
}
