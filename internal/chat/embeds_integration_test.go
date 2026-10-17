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
	if err != nil || m.DisplayName != "CI" || len(m.Embeds) != 1 || m.Embeds[0].Title != title || !m.Embeds[0].Fields[0].Inline {
		t.Fatalf("message %+v, %v", m, err)
	}
	// Embeds that are not a list (a damaged row) fail loudly instead of
	// silently dropping the card.
	if _, err := f.p.Exec(ctx, `UPDATE messages SET embeds = '{"title": 1}' WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}
	if _, err := GetMessage(ctx, f.p, id); err == nil || !strings.Contains(err.Error(), "decode embeds") {
		t.Fatalf("damaged embeds: %v", err)
	}
}
