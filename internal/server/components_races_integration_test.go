//go:build integration

package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/msch128/mnema-talk/internal/bot"
	"github.com/msch128/mnema-talk/internal/chat/core"
	"github.com/msch128/mnema-talk/internal/testutil/racetest"
)

// The races of a click through the real routes. Whatever wins, the click is
// a clean refusal or a delivered interaction, never a 500.

// clickers are members who share the clicks of a sweep: each may click 30
// times a minute.
func clickers(cf *componentsFixture, rounds int) []*client {
	out := []*client{cf.max}
	for i := 1; i <= rounds/25; i++ {
		out = append(out, cf.a.register(cf.owner, fmt.Sprintf("clicker%d", i)))
	}
	return out
}

// A click against the deletion of its message: a click that was accepted
// delivered the message as it was, and the bot can still answer it (a reply
// works; an update of the deleted message answers 10008 and leaves the
// interaction answerable). A refused click answers 404 and delivers nothing.
func TestAClickRacingTheDeletionOfItsMessage(t *testing.T) {
	cf := newComponentsFixture(t)
	rounds := racetest.Rounds(10, 200)
	members := clickers(cf, rounds)
	var msg core.Message
	delivered := 0
	racetest.Sweep(t, rounds, 2,
		func(round int) { msg = cf.postWithComponents(t, cf.open, fmt.Sprintf("round %d", round), deployRows()) },
		func(round, i int) error {
			if i == 0 {
				return statusErr(click(members[round%len(members)], cf.open, msg.ID, "cancel"), http.StatusAccepted, http.StatusNotFound)
			}
			return statusErr(cf.client.delete(botPath(cf.open, "/messages/"+msg.ID.String())), http.StatusNoContent)
		},
		func(round int, r racetest.Round) string {
			if r.Errs[0] != nil || r.Errs[1] != nil {
				return fmt.Sprintf("round %d: click %v, delete %v", round, r.Errs[0], r.Errs[1])
			}
			if cf.a.count(`SELECT COUNT(*) FROM messages WHERE id = $1`, msg.ID) != 0 {
				return fmt.Sprintf("round %d: the message survived its deletion", round)
			}
			_, _, d, ok := cf.g.dispatch(quiet)
			if !ok {
				return "" // the click was refused, or lost the race: nothing to answer
			}
			delivered++
			return answerAfterDeletion(cf, round, d, msg)
		}).FailOnViolation(t)
	t.Logf("%d of %d clicks were delivered before the deletion", delivered, rounds)
}

func answerAfterDeletion(cf *componentsFixture, round int, d []byte, msg core.Message) string {
	var in botInteraction
	if err := json.Unmarshal(d, &in); err != nil {
		return err.Error()
	}
	if got := messageOf(cf.a.t, in.Message); got.ID != msg.ID || got.Content != msg.Content {
		return fmt.Sprintf("round %d: delivered another message state: %s", round, in.Message)
	}
	res := cf.callback(in, 7, map[string]any{"content": "updated"})
	if res.status == http.StatusNotFound && decodeDiscord(cf.a.t, res).Code != 10008 {
		return fmt.Sprintf("round %d: update of the deleted message: %d %s", round, res.status, res.body)
	}
	if res.status != http.StatusNotFound {
		return fmt.Sprintf("round %d: updated a deleted message: %d %s", round, res.status, res.body)
	}
	if res := cf.callback(in, 4, map[string]any{"content": "answered"}); res.status != http.StatusNoContent {
		return fmt.Sprintf("round %d: the delivered click could not be answered: %d %s", round, res.status, res.body)
	}
	return ""
}

// A click against disabling its bot: the click is delivered, refused as gone
// (404) or as not connected (409); afterwards every click answers 404.
func TestAClickRacingTheDisablingOfItsBot(t *testing.T) {
	cf := newComponentsFixture(t)
	rounds := racetest.Rounds(8, 60)
	members := clickers(cf, rounds)
	msg := cf.postWithComponents(t, cf.open, "Deploy?", deployRows())
	racetest.Sweep(t, rounds, 2,
		func(round int) {
			wantStatus(t, "enable", cf.owner.patch(cf.botPath, map[string]any{"disabled": false}), http.StatusOK)
			cf.g = cf.dialGW("Bot " + cf.token.Secret)
			cf.g.identify("", 0)
		},
		func(round, i int) error {
			if i == 0 {
				return statusErr(click(members[round%len(members)], cf.open, msg.ID, "cancel"), http.StatusAccepted, http.StatusNotFound, http.StatusConflict)
			}
			return statusErr(cf.owner.patch(cf.botPath, map[string]any{"disabled": true}), http.StatusOK)
		},
		func(round int, r racetest.Round) string {
			if r.Errs[0] != nil || r.Errs[1] != nil {
				return fmt.Sprintf("round %d: click %v, disable %v", round, r.Errs[0], r.Errs[1])
			}
			if res := click(members[round%len(members)], cf.open, msg.ID, "cancel"); res.status != http.StatusNotFound {
				return fmt.Sprintf("round %d: a click on a disabled bot's message: %d %s", round, res.status, res.body)
			}
			return ""
		}).FailOnViolation(t)
}

// A click against regenerating the bot's token: the old token's connection
// closes, so the click is delivered or answers 409; an interaction that was
// delivered is still the bot's and is answered with the new token.
func TestAClickRacingTheRegenerationOfTheBotsToken(t *testing.T) {
	cf := newComponentsFixture(t)
	rounds := racetest.Rounds(8, 60)
	members := clickers(cf, rounds)
	msg := cf.postWithComponents(t, cf.open, "Deploy?", deployRows())
	var fresh bot.NewTokenResponse
	racetest.Sweep(t, rounds, 2,
		nil,
		func(round, i int) error {
			if i == 0 {
				return statusErr(click(members[round%len(members)], cf.open, msg.ID, "cancel"), http.StatusAccepted, http.StatusConflict)
			}
			res := cf.owner.post(cf.botPath+"/tokens/"+cf.token.ID.String()+"/regenerate", nil)
			if res.status == http.StatusCreated {
				res.decode(t, &fresh)
			}
			return statusErr(res, http.StatusCreated)
		},
		func(round int, r racetest.Round) string {
			if r.Errs[0] != nil || r.Errs[1] != nil {
				return fmt.Sprintf("round %d: click %v, regenerate %v", round, r.Errs[0], r.Errs[1])
			}
			old := cf.g
			cf.token, cf.client.token = fresh, fresh.Secret
			if _, _, d, ok := old.dispatch(quiet); ok {
				var in botInteraction
				if err := json.Unmarshal(d, &in); err != nil {
					return err.Error()
				}
				if res := cf.callback(in, 6, nil); res.status != http.StatusNoContent {
					return fmt.Sprintf("round %d: a delivered click could not be answered with the new token: %d %s", round, res.status, res.body)
				}
			}
			cf.g = cf.dialGW("Bot " + fresh.Secret)
			cf.g.identify("", 0)
			return ""
		}).FailOnViolation(t)
}

// A click against banning the member who clicks: the click is delivered or
// refused as signed out (401); the banned member's session clicks no more.
func TestAClickRacingTheBanOfTheMemberWhoClicks(t *testing.T) {
	cf := newComponentsFixture(t)
	rounds := racetest.Rounds(6, 30)
	members := make([]*client, rounds)
	for i := range members {
		members[i] = cf.a.register(cf.owner, fmt.Sprintf("banned%d", i))
	}
	msg := cf.postWithComponents(t, cf.open, "Deploy?", deployRows())
	racetest.Sweep(t, rounds, 2,
		nil,
		func(round, i int) error {
			if i == 0 {
				return statusErr(click(members[round], cf.open, msg.ID, "cancel"), http.StatusAccepted, http.StatusUnauthorized)
			}
			return statusErr(cf.owner.post("/api/admin/users/"+members[round].user.ID.String()+"/ban", map[string]any{"reason": "x"}), http.StatusNoContent)
		},
		func(round int, r racetest.Round) string {
			if r.Errs[0] != nil || r.Errs[1] != nil {
				return fmt.Sprintf("round %d: click %v, ban %v", round, r.Errs[0], r.Errs[1])
			}
			if res := click(members[round], cf.open, msg.ID, "cancel"); res.status != http.StatusUnauthorized {
				return fmt.Sprintf("round %d: a banned member clicked: %d %s", round, res.status, res.body)
			}
			cf.g.dispatch(quiet / 4) // drain a delivered click
			return ""
		}).FailOnViolation(t)
}
