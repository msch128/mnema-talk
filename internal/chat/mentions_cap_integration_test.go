//go:build integration

package chat

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"
)

func seedMembers(t *testing.T, f chatFixture, n int) []uuid.UUID {
	t.Helper()
	ids := make([]uuid.UUID, n)
	for i := range ids {
		if err := f.p.QueryRow(context.Background(),
			`INSERT INTO users (username, display_name, password_hash) VALUES ($1,$1,'test-only-unused') RETURNING id`,
			fmt.Sprintf("member%03d", i)).Scan(&ids[i]); err != nil {
			t.Fatal(err)
		}
	}
	return ids
}

// Without the right to mention everyone, a message notifies at most
// MaxMentionRecipients members, however many names or role members it names.
func TestMentionCapForAuthorsWithoutEveryone(t *testing.T) {
	f := newChatFixture(t)
	ctx := context.Background()
	members := seedMembers(t, f, 200)
	big := MentionRole{ID: uuid.New(), Name: "Members", Members: func() []uuid.UUID { return members }}

	content := "@Members"
	for i := 0; i < 100; i++ {
		content += fmt.Sprintf(" @member%03d", i)
	}
	capped := WithMentionPolicy(ctx, MentionPolicy{NoEveryone: true, Roles: []MentionRole{big}})
	set, err := resolveMentionSet(capped, f.p, content, f.u.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(set.Users) != MaxMentionRecipients {
		t.Fatalf("notified %d members, want the cap %d", len(set.Users), MaxMentionRecipients)
	}
	if len(set.Roles) != 1 {
		t.Fatalf("the role pill must stay: %v", set.Roles)
	}
	// Named users come before role members, in a stable order.
	for _, u := range set.Users {
		if u.Kind != MentionKindName {
			t.Fatalf("a role member displaced a named user: %+v", u)
		}
	}
	again, _ := resolveMentionSet(capped, f.p, content, f.u.ID, nil)
	for i := range set.Users {
		if set.Users[i] != again.Users[i] {
			t.Fatal("the capped recipients are not stable")
		}
	}

	// A role alone is capped too.
	set, err = resolveMentionSet(capped, f.p, "@Members", f.u.ID, nil)
	if err != nil || len(set.Users) != MaxMentionRecipients {
		t.Fatalf("role expansion: %d members, %v", len(set.Users), err)
	}

	// Whoever may mention everyone is not capped.
	open := WithMentionPolicy(ctx, MentionPolicy{Roles: []MentionRole{big}})
	set, err = resolveMentionSet(open, f.p, "@Members @all", f.u.ID, nil)
	if err != nil || len(set.Users) < 200 {
		t.Fatalf("privileged author: %d members, %v", len(set.Users), err)
	}
}

func TestMentionNamesAreBounded(t *testing.T) {
	var names []string
	for i := 0; i < maxMentionNames+50; i++ {
		names = append(names, fmt.Sprintf("n%d", i))
	}
	if got := capNames(names); len(got) != maxMentionNames {
		t.Fatalf("%d names kept", len(got))
	}
	if got := capNames(names[:3]); len(got) != 3 {
		t.Fatal("short lists are untouched")
	}
}
