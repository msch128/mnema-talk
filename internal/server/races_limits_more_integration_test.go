//go:build integration

package server

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/msch128/mnema-talk/internal/automod"
	"github.com/msch128/mnema-talk/internal/chat/blocks"
	"github.com/msch128/mnema-talk/internal/later"
	"github.com/msch128/mnema-talk/internal/testutil/racetest"
)

// Lane 7 of the defect hunt, second batch: per-member and per-channel caps
// under callers released together one short of the cap.

// The block and reminder rates (per member and minute) would refuse later
// rounds of one member, so every round has its own.
func TestParallelBlocksStayWithinTheCap(t *testing.T) {
	a := newApp(t, false)
	a.seedAdmin()
	targets := make([]*client, raceCallers)
	for i := range targets {
		targets[i] = a.sqlMember(fmt.Sprintf("target%d", i))
	}
	a.runSQL(`INSERT INTO users (username, display_name, password_hash) SELECT 'filler' || g, 'filler', 'x' FROM generate_series(1, $1) g`, blocks.MaxBlocksPerMember-1)
	var blocker *client
	racetest.Sweep(t, racetest.Rounds(10, 200), raceCallers,
		func(round int) {
			blocker = a.sqlMember(fmt.Sprintf("blocker%d", round))
			a.runSQL(`INSERT INTO user_blocks (blocker_id, blocked_id) SELECT $1, id FROM users WHERE username LIKE 'filler%'`, blocker.user.ID)
		},
		func(_, i int) error {
			return statusErr(blocker.put("/api/me/blocks/"+targets[i].user.ID.String(), nil), http.StatusNoContent, http.StatusConflict)
		},
		strict(func(round int, _ racetest.Round) string {
			if n := a.count(`SELECT COUNT(*) FROM user_blocks WHERE blocker_id = $1`, blocker.user.ID); n != blocks.MaxBlocksPerMember {
				return fmt.Sprintf("round %d: %d blocks", round, n)
			}
			return ""
		})).FailOnViolation(t)
}

func TestParallelRemindersStayWithinTheCap(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	ch := a.createChannel(admin, "general", "text")
	msg := a.send(admin, ch, "remind me")
	var member *client
	racetest.Sweep(t, racetest.Rounds(10, 200), raceCallers,
		func(round int) {
			member = a.sqlMember(fmt.Sprintf("reminded%d", round))
			a.runSQL(`INSERT INTO message_reminders (user_id, message_id, remind_at)
				SELECT $1, $2, NOW() + INTERVAL '1 day' FROM generate_series(1, $3)`, member.user.ID, msg, later.MaxReminders-1)
		},
		func(_, i int) error {
			at := time.Now().Add(time.Duration(i+2) * time.Hour)
			return statusErr(member.post("/api/me/reminders", map[string]any{"message_id": msg, "remind_at": at}), http.StatusCreated, http.StatusOK, http.StatusConflict)
		},
		strict(func(round int, _ racetest.Round) string {
			if n := a.count(`SELECT COUNT(*) FROM message_reminders WHERE user_id = $1`, member.user.ID); n != later.MaxReminders {
				return fmt.Sprintf("round %d: %d reminders", round, n)
			}
			return ""
		})).FailOnViolation(t)
}

func TestParallelAutomodRulesStayWithinTheCapAndOneRaidRule(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	raid := map[string]any{"kind": "raid", "name": "raid", "enabled": true, "action": "flag", "config": map[string]any{"joins": 5, "window_seconds": 10, "restrict_seconds": 300}}
	// Only administrators write rules: the deep count stays under one
	// member's write rate (about 600 a minute).
	racetest.Sweep(t, racetest.Rounds(10, 60), raceCallers,
		func(int) {
			a.runSQL(`DELETE FROM automod_rules`)
			a.runSQL(`INSERT INTO automod_rules (kind, name, config) SELECT 'words', 'filler ' || g, '{"words":["x"]}' FROM generate_series(1, $1) g`, automod.MaxRules-2)
		},
		func(round, i int) error {
			body := wordRule(fmt.Sprintf("rule %d %d", round, i), "block", "y")
			if i%2 == 1 {
				body = raid
			}
			return statusErr(admin.post("/api/admin/automod/rules", body), http.StatusCreated, http.StatusConflict)
		},
		strict(func(round int, r racetest.Round) string {
			n := a.count(`SELECT COUNT(*) FROM automod_rules`)
			raids := a.count(`SELECT COUNT(*) FROM automod_rules WHERE kind = 'raid'`)
			if n != automod.MaxRules || raids > 1 {
				return fmt.Sprintf("round %d: %d rules, %d raid rules, answers %v", round, n, raids, r.Errs)
			}
			return ""
		})).FailOnViolation(t)
}

func TestParallelForumTagsStayWithinTheCap(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	forum := a.createChannel(admin, "board", "forum")
	const maxTags = 20 // errTooManyForumTags in internal/chat/forum
	// Only administrators write tags here: as for the automod rules.
	racetest.Sweep(t, racetest.Rounds(10, 60), raceCallers,
		func(round int) {
			a.runSQL(`DELETE FROM forum_tags WHERE channel_id = $1`, forum)
			a.runSQL(`INSERT INTO forum_tags (channel_id, name) SELECT $1, 'f' || g FROM generate_series(1, $2) g`, forum, maxTags-1)
		},
		func(round, i int) error {
			// Two callers share a name: the name stays unique as well.
			r := admin.post("/api/channels/"+forum.String()+"/tags", map[string]any{"name": fmt.Sprintf("n%d-%d", round, i/2)})
			return statusErr(r, http.StatusCreated, http.StatusConflict)
		},
		strict(func(round int, r racetest.Round) string {
			if n := a.count(`SELECT COUNT(*) FROM forum_tags WHERE channel_id = $1`, forum); n != maxTags {
				return fmt.Sprintf("round %d: %d tags, answers %v", round, n, r.Errs)
			}
			return ""
		})).FailOnViolation(t)
}
