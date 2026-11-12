//go:build integration

package server

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/chat/messages"
	"github.com/msch128/mnema-talk/internal/chat/saved"
	"github.com/msch128/mnema-talk/internal/perm"
	"github.com/msch128/mnema-talk/internal/testutil/racetest"
)

// Lane 7 of the defect hunt: limits and "only one" through the real routes,
// with callers released together one short of the limit and an invariant
// query after every round.

const raceCallers = 8

func statusErr(r response, ok ...int) error {
	for _, s := range ok {
		if r.status == s {
			return nil
		}
	}
	return fmt.Errorf("%d %s", r.status, r.body)
}

// strict makes an answer outside the expected ones (statusErr) a violation,
// so a sweep whose callers were all refused for another reason (a rate
// limit, a permission) cannot pass by doing nothing. The checks also require
// the cap to be reached exactly, which needs at least one caller through.
func strict(check func(int, racetest.Round) string) func(int, racetest.Round) string {
	return func(round int, r racetest.Round) string {
		for _, err := range r.Errs {
			if err != nil {
				return fmt.Sprintf("round %d: unexpected answer %v", round, err)
			}
		}
		return check(round, r)
	}
}

func (a *app) count(sql string, args ...any) int {
	a.t.Helper()
	var n int
	if err := a.db.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		a.t.Fatal(err)
	}
	return n
}

// sqlMember creates a member with SQL and signs it in without the sign-up
// and login routes, whose per-address limits a sweep of many rounds meets.
func (a *app) sqlMember(name string) *client {
	a.t.Helper()
	u := &auth.User{Username: name, DisplayName: name, Role: "user"}
	if err := a.db.QueryRow(context.Background(), `INSERT INTO users (username, display_name, password_hash, role)
		VALUES ($1, $1, 'x', 'user') RETURNING id`, name).Scan(&u.ID); err != nil {
		a.t.Fatal(err)
	}
	c := a.sessionFor(u.ID)
	c.user = u
	return c
}

func TestParallelReactionsStayWithinBothCaps(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	ch := a.createChannel(admin, "general", "text")
	msg := a.send(admin, ch, "react to me")
	path := "/api/messages/" + msg.String() + "/reactions"
	members := make([]*client, raceCallers)
	for i := range members {
		members[i] = a.register(admin, fmt.Sprintf("reactor%d", i))
	}
	clear := func() { a.runSQL(`DELETE FROM message_reactions WHERE message_id = $1`, msg) }

	t.Run("per member", func(t *testing.T) {
		// A member of its own per round: the reaction rate is per member.
		var reactor *client
		racetest.Sweep(t, racetest.Rounds(10, 200), raceCallers,
			func(round int) {
				clear()
				reactor = a.sqlMember(fmt.Sprintf("solo%d", round))
				a.runSQL(`INSERT INTO message_reactions (message_id, user_id, emoji)
					SELECT $1, $2, 'f' || g FROM generate_series(1, $3) g`, msg, reactor.user.ID, messages.MaxReactionsPerUser-1)
			},
			func(round, i int) error {
				return statusErr(reactor.post(path, map[string]string{"emoji": fmt.Sprintf("r%d", i)}), http.StatusOK, http.StatusConflict)
			},
			strict(func(round int, _ racetest.Round) string {
				if n := a.count(`SELECT COUNT(*) FROM message_reactions WHERE message_id = $1 AND user_id = $2`, msg, reactor.user.ID); n != messages.MaxReactionsPerUser {
					return fmt.Sprintf("round %d: %d reactions of one member", round, n)
				}
				return ""
			})).FailOnViolation(t)
	})
	t.Run("distinct per message", func(t *testing.T) {
		racetest.Sweep(t, racetest.Rounds(10, 200), raceCallers,
			func(int) {
				clear()
				a.runSQL(`INSERT INTO message_reactions (message_id, user_id, emoji)
					SELECT $1, $2, 'f' || g FROM generate_series(1, $3) g`, msg, admin.user.ID, messages.MaxReactionsPerUser)
				a.runSQL(`INSERT INTO message_reactions (message_id, user_id, emoji)
					SELECT $1, $2, 'g' || g FROM generate_series(1, $3) g`, msg, members[0].user.ID, messages.MaxEmojiPerMessage-messages.MaxReactionsPerUser-1)
			},
			func(round, i int) error {
				return statusErr(members[i].post(path, map[string]string{"emoji": fmt.Sprintf("n%d", i)}), http.StatusOK, http.StatusConflict)
			},
			strict(func(round int, _ racetest.Round) string {
				if n := a.count(`SELECT COUNT(DISTINCT emoji) FROM message_reactions WHERE message_id = $1`, msg); n != messages.MaxEmojiPerMessage {
					return fmt.Sprintf("round %d: %d distinct emoji", round, n)
				}
				return ""
			})).FailOnViolation(t)
	})
}

// The pin and saved rates (per member and minute) would refuse later rounds of
// one member, so every round has its own; for pins @everyone gets Manage
// messages.
func TestParallelPinsStayWithinTheChannelCap(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	ch := a.createChannel(admin, "general", "text")
	wantStatus(t, "everyone manages messages", admin.patch("/api/admin/roles/"+everyoneID(t, admin).String(),
		map[string]any{"permissions": perm.DefaultEveryone | perm.ManageMessages}), http.StatusOK)
	ids := make([]uuid.UUID, raceCallers)
	for i := range ids {
		ids[i] = a.send(admin, ch, fmt.Sprintf("pin me %d", i))
	}
	a.runSQL(`INSERT INTO messages (channel_id, user_id, content) SELECT $1, $2, 'filler' FROM generate_series(1, $3)`, ch, admin.user.ID, messages.MaxPinsPerChannel-1)
	var pinner *client
	racetest.Sweep(t, racetest.Rounds(10, 200), raceCallers,
		func(round int) {
			pinner = a.sqlMember(fmt.Sprintf("pinner%d", round))
			a.runSQL(`UPDATE messages SET is_pinned = FALSE, pinned_at = NULL WHERE channel_id = $1`, ch)
			a.runSQL(`UPDATE messages SET is_pinned = TRUE, pinned_at = NOW() WHERE channel_id = $1 AND content = 'filler'`, ch)
		},
		func(_, i int) error {
			r := pinner.do(http.MethodPut, "/api/channels/"+ch.String()+"/messages/"+ids[i].String()+"/pin", nil, nil)
			return statusErr(r, http.StatusOK, http.StatusConflict)
		},
		strict(func(round int, _ racetest.Round) string {
			if n := a.count(`SELECT COUNT(*) FROM messages WHERE channel_id = $1 AND is_pinned`, ch); n != messages.MaxPinsPerChannel {
				return fmt.Sprintf("round %d: %d pins", round, n)
			}
			return ""
		})).FailOnViolation(t)
}

func TestParallelSavesStayWithinTheSavedCap(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	ch := a.createChannel(admin, "general", "text")
	ids := make([]uuid.UUID, raceCallers)
	for i := range ids {
		ids[i] = a.send(admin, ch, fmt.Sprintf("save me %d", i))
	}
	a.runSQL(`INSERT INTO messages (channel_id, user_id, content) SELECT $1, $2, 'filler' FROM generate_series(1, $3)`, ch, admin.user.ID, saved.MaxSavedPerMember-1)
	var saver *client
	racetest.Sweep(t, racetest.Rounds(10, 200), raceCallers,
		func(round int) {
			saver = a.sqlMember(fmt.Sprintf("saver%d", round))
			a.runSQL(`INSERT INTO saved_messages (user_id, message_id) SELECT $1, id FROM messages WHERE content = 'filler'`, saver.user.ID)
		},
		func(_, i int) error {
			return statusErr(saver.put("/api/me/saved/"+ids[i].String(), nil), http.StatusOK, http.StatusConflict)
		},
		strict(func(round int, _ racetest.Round) string {
			if n := a.count(`SELECT COUNT(*) FROM saved_messages WHERE user_id = $1`, saver.user.ID); n != saved.MaxSavedPerMember {
				return fmt.Sprintf("round %d: %d saved", round, n)
			}
			return ""
		})).FailOnViolation(t)
}

func TestParallelReportsOfOneMessageFileOne(t *testing.T) {
	f := newReportsFixture(t)
	msg := f.a.send(f.author, f.channel, "report me")
	// The report rate (10 an hour per member) would refuse a second round of
	// the same reporter, so every round has its own.
	var reporter *client
	racetest.Sweep(t, racetest.Rounds(10, 200), raceCallers,
		func(round int) { reporter = f.a.sqlMember(fmt.Sprintf("reporter%d", round)) },
		func(_, i int) error {
			return statusErr(f.report(reporter, msg, "spam", fmt.Sprintf("note %d", i)), http.StatusCreated, http.StatusOK, http.StatusConflict)
		},
		func(round int, r racetest.Round) string {
			n := f.a.count(`SELECT COUNT(*) FROM message_reports WHERE reporter_id = $1 AND message_id = $2`, reporter.user.ID, msg)
			if n != 1 {
				return fmt.Sprintf("round %d: %d reports, answers %v", round, n, r.Errs)
			}
			return ""
		}).FailOnViolation(t)
}

// Both sides open their conversation of two at the same moment: one
// conversation, and both get it.
func TestBothSidesOpeningAConversationAtOnceGetTheSameOne(t *testing.T) {
	f := newDMFixture(t, false)
	ids := make([]uuid.UUID, 2)
	// The start rate (3 a minute per member) would refuse later rounds of one
	// pair, so every round has a pair of its own.
	var pair [2]*client
	racetest.Sweep(t, racetest.Rounds(20, 300), 2,
		func(round int) {
			f.a.runSQL(`DELETE FROM channels WHERE dm_key IS NOT NULL`)
			pair = [2]*client{f.a.sqlMember(fmt.Sprintf("left%d", round)), f.a.sqlMember(fmt.Sprintf("right%d", round))}
		},
		func(_, i int) error {
			from, to := pair[i], pair[1-i]
			r := from.startDM(to)
			if err := statusErr(r, http.StatusOK, http.StatusCreated); err != nil {
				return err
			}
			r.decodeID(t, &ids[i])
			return nil
		},
		func(round int, r racetest.Round) string {
			n := f.a.count(`SELECT COUNT(*) FROM channels WHERE dm_key IS NOT NULL`)
			if n != 1 || r.Succeeded() != 2 || ids[0] != ids[1] {
				return fmt.Sprintf("round %d: %d conversations, answers %v, ids %v", round, n, r.Errs, ids)
			}
			return ""
		}).FailOnViolation(t)
}

// Messages to someone who never answered: at most two, however many are sent
// at once.
func TestParallelFirstMessagesStayWithinTheUnansweredCap(t *testing.T) {
	f := newDMFixture(t, false)
	// A pair of its own per round: the message rate is per member.
	var sender *client
	var dm dmView
	racetest.Sweep(t, racetest.Rounds(10, 200), raceCallers,
		func(round int) {
			sender = f.a.sqlMember(fmt.Sprintf("sender%d", round))
			dm = sender.mustDM(t, f.a.sqlMember(fmt.Sprintf("silent%d", round)))
		},
		func(_, i int) error {
			// 429 is the answer of the gate; a fresh sender stays far below
			// the message rate, which answers 429 as well.
			return statusErr(sender.dmPost(dm.ID, fmt.Sprintf("hello %d", i)), http.StatusCreated, http.StatusTooManyRequests)
		},
		strict(func(round int, _ racetest.Round) string {
			if n := f.a.count(`SELECT COUNT(*) FROM messages WHERE channel_id = $1`, dm.ID); n != 2 {
				return fmt.Sprintf("round %d: %d unanswered messages", round, n)
			}
			return ""
		})).FailOnViolation(t)
}
