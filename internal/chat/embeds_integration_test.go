//go:build integration

package chat

import (
	"context"
	"strings"
	"testing"
)

func TestMessageEmbedsRoundTripAndCorruption(t *testing.T) {
	f := newChatFixture(t)
	ctx := context.Background()
	title := "Run"
	id, err := InsertMessage(ctx, f.p, nil, NewMessage{
		ChannelID: f.c.ID, UserID: f.u.ID, Content: "x", AuthorName: "CI",
		Embeds: []Embed{{Title: title, Fields: []EmbedField{{Name: "n", Value: "v", Inline: true}}}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	m, err := GetMessage(ctx, f.p, id)
	if err != nil {
		t.Fatalf("get message: %v", err)
	}
	if m.DisplayName != "CI" {
		t.Fatalf("unexpected display name: %q", m.DisplayName)
	}
	if len(m.Embeds) != 1 || m.Embeds[0].Title != title {
		t.Fatalf("unexpected embeds list or title: %+v", m.Embeds)
	}
	if len(m.Embeds[0].Fields) == 0 || !m.Embeds[0].Fields[0].Inline {
		t.Fatalf("expected inline embed field: %+v", m.Embeds[0].Fields)
	}
	// Embeds that are not a list (a damaged row) fail loudly instead of
	// silently dropping the card.
	if _, err := f.p.Exec(ctx, `UPDATE messages SET embeds = '{"title": 1}' WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}
	if _, err := GetMessage(ctx, f.p, id); err == nil {
		t.Fatal("expected error decoding damaged embeds")
	} else if !strings.Contains(err.Error(), "decode embeds") {
		t.Fatalf("damaged embeds: %v", err)
	}
}
