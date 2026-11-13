//go:build integration

package server

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/perm"
)

// An interaction does not outlive the bot being disabled or its token being
// regenerated: enabled again within the 15 minutes, the bot cannot answer
// what it was sent before.
func TestDisablingABotOrRegeneratingItsTokenEndsItsPendingInteractions(t *testing.T) {
	cf := newComponentsFixture(t)
	in := cf.command(t)
	wantStatus(t, "disable", cf.owner.patch(cf.botPath, map[string]any{"disabled": true}), http.StatusOK)
	wantStatus(t, "enable", cf.owner.patch(cf.botPath, map[string]any{"disabled": false}), http.StatusOK)
	wantStatus(t, "answer after re-enable", cf.callback(in, 4, map[string]any{"content": "late"}), http.StatusNotFound)

	cf.g = cf.dialGW("Bot " + cf.token.Secret)
	cf.g.identify("", 0)
	in = cf.command(t)
	res := cf.owner.post(cf.botPath+"/tokens/"+cf.token.Token.ID.String()+"/regenerate", nil)
	wantStatus(t, "regenerate", res, http.StatusCreated)
	var fresh struct {
		Secret string `json:"secret"`
	}
	res.decode(t, &fresh)
	cf.client.token = fresh.Secret
	wantStatus(t, "answer with the new token", cf.callback(in, 4, map[string]any{"content": "late"}), http.StatusNotFound)
}

// A note or a modal is shown only to a member who can still see the channel.
func TestANoteOrAModalNeedsAMemberWhoStillSeesTheChannel(t *testing.T) {
	cf := newComponentsFixture(t)
	var maxID uuid.UUID
	if err := cf.a.db.QueryRow(t.Context(), `SELECT id FROM users WHERE username = 'max'`).Scan(&maxID); err != nil {
		t.Fatal(err)
	}
	note, modal := cf.command(t), cf.command(t)
	wantStatus(t, "hide the channel from max", putOverwrites(cf.owner, "channels", cf.open,
		overwrite("role", cf.roleID, perm.ViewChannel, 0), overwrite("member", maxID, 0, perm.ViewChannel)), http.StatusNoContent)
	res := cf.callback(note, 4, map[string]any{"content": "psst", "flags": 64})
	wantStatus(t, "ephemeral reply", res, http.StatusForbidden)
	if e := decodeDiscord(t, res); e.Message != "the member can no longer see this channel" {
		t.Fatalf("refusal: %s", res.body)
	}
	wantStatus(t, "modal", cf.callback(modal, 9, feedbackModal()), http.StatusForbidden)
	// Neither reached max, and the interactions can still be answered otherwise.
	if _, ok := cf.maxWS.expect("interaction_response", quiet); ok {
		t.Fatal("max got the note")
	}
	if _, ok := cf.maxWS.expect("interaction_modal", quiet); ok {
		t.Fatal("max got the modal")
	}
	wantStatus(t, "defer instead", cf.callback(modal, 5, nil), http.StatusNoContent)
}

// A deferral (callback 5, or 6 on a click) tells the member the bot is on it,
// with the interaction id their run or click was answered with.
func TestADeferralTellsTheMemberTheBotIsOnIt(t *testing.T) {
	cf := newComponentsFixture(t)
	in := cf.command(t)
	wantStatus(t, "defer", cf.callback(in, 5, nil), http.StatusNoContent)
	p, ok := cf.maxWS.expect("interaction_deferred", soon)
	var got struct {
		InteractionID uuid.UUID `json:"interaction_id"`
		ChannelID     uuid.UUID `json:"channel_id"`
		BotID         uuid.UUID `json:"bot_id"`
	}
	if !ok || json.Unmarshal(p, &got) != nil || got.InteractionID != in.ID || got.ChannelID != cf.open || got.BotID != cf.bot.ID {
		t.Fatalf("interaction_deferred: %s", p)
	}

	m := cf.postWithComponents(t, cf.open, "Deploy?", deployRows())
	res := click(cf.max, cf.open, m.ID, "cancel")
	wantStatus(t, "click", res, http.StatusAccepted)
	var clicked struct {
		InteractionID uuid.UUID `json:"interaction_id"`
	}
	res.decode(t, &clicked)
	in = cf.interaction(t)
	wantStatus(t, "defer update", cf.callback(in, 6, nil), http.StatusNoContent)
	p, ok = cf.maxWS.expect("interaction_deferred", soon)
	if !ok || json.Unmarshal(p, &got) != nil || got.InteractionID != clicked.InteractionID {
		t.Fatalf("interaction_deferred after a click: %s", p)
	}
	// Another member watching the same channel is not told.
	ownerWS := mustDial(t, cf.owner)
	in = cf.command(t)
	wantStatus(t, "defer for max", cf.callback(in, 5, nil), http.StatusNoContent)
	if _, ok := cf.maxWS.expect("interaction_deferred", soon); !ok {
		t.Fatal("max was not told")
	}
	if p, ok := ownerWS.expect("interaction_deferred", quiet); ok {
		t.Fatalf("another member got max's deferral: %s", p)
	}
	// A direct answer is no deferral.
	in = cf.command(t)
	wantStatus(t, "reply", cf.callback(in, 4, map[string]any{"content": "done"}), http.StatusNoContent)
	if p, ok := cf.maxWS.expect("interaction_deferred", quiet); ok {
		t.Fatalf("a reply announced a deferral: %s", p)
	}
}
