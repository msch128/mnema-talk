//go:build integration

package server

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/bot"
	"github.com/msch128/mnema-talk/internal/perm"
	"github.com/msch128/mnema-talk/internal/testutil/racetest"
)

// deleteInstalled removes every bot added from a manifest (they have a
// description; bots created by hand have none), so rounds stay below the bot
// limit.
func deleteInstalled(t *testing.T, a *app) {
	t.Helper()
	rows, err := a.db.Query(context.Background(), `SELECT id FROM bots WHERE description <> ''`)
	if err != nil {
		t.Fatal(err)
	}
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	rows.Close()
	for _, id := range ids {
		if err := bot.Delete(context.Background(), a.db, id); err != nil {
			t.Fatal(err)
		}
	}
}

// An install racing the Owner taking the installer's role (Manage bots and
// Manage messages) ends fully installed, with the grant the installer held
// when it ran, or with nothing at all.
func TestInstallRacingTheInstallersDemotionIsAllOrNothing(t *testing.T) {
	a := newApp(t, false)
	owner := a.seedAdmin()
	open := a.createChannel(owner, "open", "text")
	makers := createRole(t, owner, "Bot makers", perm.ManageBots|perm.ManageMessages)
	var maker *client
	var status int
	body := installBody(testManifest("Racer"), []string{"view_channel", "manage_messages"}, nil, open)
	racetest.Sweep(t, racetest.Rounds(10, 100), 2,
		func(round int) {
			deleteInstalled(t, a)
			maker = a.sqlMember(fmt.Sprintf("maker%d", round))
			wantStatus(t, "promote", setRoles(owner, maker.user.ID, makers), http.StatusNoContent)
		},
		func(_, i int) error {
			if i == 1 {
				return statusErr(setRoles(owner, maker.user.ID), http.StatusNoContent)
			}
			res := maker.post("/api/admin/bots/install", body)
			status = res.status
			return statusErr(res, http.StatusCreated, http.StatusForbidden)
		},
		strict(func(round int, _ racetest.Round) string {
			bots := a.count(`SELECT COUNT(*) FROM bots WHERE created_by = $1`, maker.user.ID)
			granted := a.count(`
				SELECT COUNT(*) FROM permission_overwrites o JOIN roles r ON r.id = o.role_id
				JOIN bots b ON b.user_id = r.bot_user_id
				WHERE b.created_by = $1 AND o.channel_id = $2 AND o.allow = $3`, maker.user.ID, open, int64(perm.ViewChannel|perm.ManageMessages))
			tokens := a.count(`SELECT COUNT(*) FROM bot_tokens WHERE created_by = $1`, maker.user.ID)
			switch {
			case status == http.StatusCreated && (bots != 1 || granted != 1 || tokens != 1):
				return fmt.Sprintf("round %d: installed, but %d bots, %d grants, %d tokens", round, bots, granted, tokens)
			case status == http.StatusForbidden && (bots != 0 || tokens != 0):
				return fmt.Sprintf("round %d: refused, but %d bots and %d tokens were left", round, bots, tokens)
			}
			return ""
		})).FailOnViolation(t)
}

// Two installs of the same manifest at once are two bots; at the bot limit
// exactly one of them gets through and the other is refused cleanly.
func TestParallelInstallsOfOneManifest(t *testing.T) {
	a := newApp(t, false)
	owner := a.seedAdmin()
	open := a.createChannel(owner, "open", "text")
	body := installBody(testManifest("Twin"), []string{"view_channel", "send_messages"}, []string{"GUILDS"}, open)
	call := func(_, _ int) error {
		return statusErr(owner.post("/api/admin/bots/install", body), http.StatusCreated, http.StatusConflict)
	}
	installed := func() int { return a.count(`SELECT COUNT(*) FROM bots WHERE description <> ''`) }
	consistent := func(round, want int) string {
		n, tokens := installed(), a.count(`SELECT COUNT(*) FROM bot_tokens t JOIN bots b ON b.id = t.bot_id WHERE b.description <> ''`)
		ows := a.count(`SELECT COUNT(*) FROM permission_overwrites o JOIN roles r ON r.id = o.role_id WHERE r.bot_user_id IS NOT NULL AND o.channel_id = $1`, open)
		if n != want || tokens != want || ows != want || a.count(`SELECT COUNT(*) FROM bots`) > bot.MaxBots {
			return fmt.Sprintf("round %d: %d bots, %d tokens, %d grants, want %d each", round, n, tokens, ows, want)
		}
		return ""
	}

	t.Run("below the limit", func(t *testing.T) {
		racetest.Sweep(t, racetest.Rounds(10, 100), 2,
			func(int) { deleteInstalled(t, a) },
			call,
			strict(func(round int, r racetest.Round) string { return consistent(round, 2) }),
		).FailOnViolation(t)
	})
	t.Run("at the limit", func(t *testing.T) {
		deleteInstalled(t, a)
		for i := range bot.MaxBots - 1 {
			if _, err := bot.Create(context.Background(), a.db, fmt.Sprintf("filler%d", i), 0, owner.user.ID); err != nil {
				t.Fatal(err)
			}
		}
		racetest.Sweep(t, racetest.Rounds(10, 100), 2,
			func(int) { deleteInstalled(t, a) },
			call,
			strict(func(round int, r racetest.Round) string { return consistent(round, 1) }),
		).FailOnViolation(t)
	})
}
