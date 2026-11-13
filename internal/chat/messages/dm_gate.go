package messages

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/chat/core"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
)

var errDMWait = httpx.NewAPIError(http.StatusTooManyRequests, httpx.CodeRateLimited,
	fmt.Sprintf("Wait for a reply before you send more than %d messages.", core.MaxUnansweredDMMessages))

// GatePost applies what a new message needs besides the permission checks of
// the request, inside the transaction that stores it: slow mode in a channel,
// the direct message rules in a conversation. It runs for every way a message
// is created (members, uploads, polls, commands), so none can go around it.
//
// The author's exemption from slow mode and their wait as a new member are
// read from ctx (see rulesFrom).
func GatePost(ctx context.Context, tx pgx.Tx, nm NewMessage) error {
	return gatePost(ctx, tx, nm, rulesFrom(ctx))
}

func gatePost(ctx context.Context, tx pgx.Tx, nm NewMessage, rules sendRules) error {
	var kind string
	var seconds int
	var talkOf *uuid.UUID
	err := tx.QueryRow(ctx, `SELECT type, slow_mode_seconds, dm_id FROM channels WHERE id = $1`, nm.ChannelID).Scan(&kind, &seconds, &talkOf)
	if errors.Is(err, pgx.ErrNoRows) {
		return core.ErrChannelNotFound
	}
	if err != nil {
		return fmt.Errorf("load channel: %w", err)
	}
	if kind == core.ChannelKindDM {
		if err := gateLifetime(ctx, tx, kind, nm); err != nil {
			return err
		}
		return gateDM(ctx, tx, nm, rules)
	}
	if talkOf != nil && nm.WebhookID == nil {
		if err := checkPairWritable(ctx, tx, nm.ChannelID, nm.UserID); err != nil {
			return err
		}
	}
	if nm.ParentID != nil {
		if err := gateReply(ctx, tx, *nm.ParentID, rules); err != nil {
			return err
		}
	} else if kind == "forum" && (nm.Title == nil || *nm.Title == "") {
		return core.ErrPostTitleRequired
	}
	if err := gateLifetime(ctx, tx, kind, nm); err != nil {
		return err
	}
	if nm.WebhookID == nil && !rules.SlowModeExempt {
		return enforceSlowMode(ctx, tx, nm.ChannelID, nm.UserID, seconds)
	}
	return nil
}

func gateReply(ctx context.Context, tx pgx.Tx, parentID uuid.UUID, rules sendRules) error {
	var isLocked bool
	err := tx.QueryRow(ctx, `SELECT is_locked FROM messages WHERE id = $1 AND (expires_at IS NULL OR expires_at > now())`, parentID).Scan(&isLocked)
	if errors.Is(err, pgx.ErrNoRows) {
		return httpx.ErrInvalidInput("parent message not found")
	}
	if err != nil {
		return fmt.Errorf("check thread lock: %w", err)
	}
	if isLocked && !rules.SlowModeExempt {
		return httpx.ErrForbidden("this post is locked")
	}
	return nil
}

// dmState is what the rules need to know about a conversation.
type dmState struct {
	creator *uuid.UUID
	replied bool
	sent    int
	group   bool
	members []uuid.UUID
}

func loadDMState(ctx context.Context, tx pgx.Tx, id uuid.UUID) (dmState, error) {
	var st dmState
	// The row lock serializes the writers of one conversation, so the cap on
	// unanswered messages cannot be passed by two requests at once.
	err := tx.QueryRow(ctx, `SELECT dm_created_by, dm_replied, dm_creator_sent, dm_key IS NULL FROM channels WHERE id = $1 FOR UPDATE`, id).
		Scan(&st.creator, &st.replied, &st.sent, &st.group)
	if err != nil {
		return st, fmt.Errorf("load direct message: %w", err)
	}
	st.members, err = core.DMMemberIDs(ctx, tx, id)
	return st, err
}

// gateDM decides whether nm may be written in a conversation. Everything that
// concerns another member (a block, a closed or disabled account) answers
// errDMSend, which says nothing about why.
func gateDM(ctx context.Context, tx pgx.Tx, nm NewMessage, rules sendRules) error {
	if nm.WebhookID != nil {
		return core.ErrDMSend // webhooks and bots never write in a conversation
	}
	st, err := loadDMState(ctx, tx, nm.ChannelID)
	if err != nil {
		return err
	}
	var others []uuid.UUID
	isMember := false
	for _, m := range st.members {
		if m == nm.UserID {
			isMember = true
		} else {
			others = append(others, m)
		}
	}
	if !isMember {
		return core.ErrChannelNotFound
	}
	if err := checkDMReachable(ctx, tx, nm.UserID, others, st.group); err != nil {
		return err
	}
	if err := gateNewMemberFirstWrite(nm, st, rules.NewMemberWait); err != nil {
		return err
	}
	if err := countDMMessage(ctx, tx, nm, st); err != nil {
		return err
	}
	// A new message opens the conversation again for whoever closed it (not for
	// a member who blocked the sender: they would see nothing new).
	_, err = tx.Exec(ctx, `
		UPDATE dm_members SET hidden_at = NULL
		WHERE channel_id = $1 AND hidden_at IS NOT NULL
		  AND NOT EXISTS (SELECT 1 FROM user_blocks ub WHERE ub.blocker_id = dm_members.user_id AND ub.blocked_id = $2)`,
		nm.ChannelID, nm.UserID)
	return err
}

// checkDMReachable requires the switch to be on and somebody to write to: in a
// conversation with one other person that person must be active and neither
// side may have blocked the other; in a group at least one other member must
// be active (a block between two members does not stop the group).
func checkDMReachable(ctx context.Context, tx pgx.Tx, sender uuid.UUID, others []uuid.UUID, group bool) error {
	on, active, blocked, err := dmReach(ctx, tx, sender, others)
	if err != nil {
		return err
	}
	if !on || active == 0 || (!group && (active != len(others) || blocked > 0)) {
		return core.ErrDMSend
	}
	return nil
}

// dmReach reads the community's direct message switch and, of others, how
// many are active and how many blocked sender or were blocked by them.
func dmReach(ctx context.Context, tx pgx.Tx, sender uuid.UUID, others []uuid.UUID) (on bool, active, blocked int, err error) {
	err = tx.QueryRow(ctx, `
		SELECT (SELECT direct_messages_enabled FROM instance_settings),
		       COUNT(*), COUNT(*) FILTER (WHERE EXISTS (
		           SELECT 1 FROM user_blocks b WHERE (b.blocker_id = u.id AND b.blocked_id = $1) OR (b.blocker_id = $1 AND b.blocked_id = u.id)))
		FROM users u WHERE u.id = ANY($2::uuid[]) AND `+auth.ActiveSQL("u")+` AND NOT u.is_bot`, sender, others).Scan(&on, &active, &blocked)
	if err != nil {
		err = fmt.Errorf("check direct message: %w", err)
	}
	return on, active, blocked, err
}

// checkPairWritable applies the rule of a conversation with one other person
// to channelID when it is such a conversation or the chat of its Talk: a block
// in either direction, or the other side's account disabled or erased (then
// no other member is left), refuses
// the write with ErrDMSend, the refusal of a new message, which says nothing
// more about why. Edits run it, and new messages in the Talk's chat (new
// messages in the conversation itself go through gateDM). The Talk's chat
// follows the channel permissions, not the community's direct message switch,
// so the switch is only asked for the conversation. Other channels, groups and
// their Talks are not touched: a block does not stop a group.
func checkPairWritable(ctx context.Context, tx pgx.Tx, channelID, userID uuid.UUID) error {
	var dm uuid.UUID
	var talk bool
	err := tx.QueryRow(ctx, `
		SELECT d.id, d.id <> c.id FROM channels c
		JOIN channels d ON d.id = COALESCE(c.dm_id, c.id) AND d.type = $2 AND d.dm_key IS NOT NULL
		WHERE c.id = $1`, channelID, core.ChannelKindDM).Scan(&dm, &talk)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil // neither a conversation of two nor the chat of its Talk
	}
	if err != nil {
		return fmt.Errorf("load conversation: %w", err)
	}
	members, err := core.DMMemberIDs(ctx, tx, dm)
	if err != nil {
		return err
	}
	others := make([]uuid.UUID, 0, len(members))
	for _, m := range members {
		if m != userID {
			others = append(others, m)
		}
	}
	on, active, blocked, err := dmReach(ctx, tx, userID, others)
	if err != nil {
		return err
	}
	// An erased account leaves the conversation (its dm_members row goes), so
	// nobody else left is the erased case: refused like a disabled one.
	if (!on && !talk) || len(others) == 0 || active != len(others) || blocked > 0 {
		return core.ErrDMSend
	}
	return nil
}

// checkDMReactionAllowed refuses a new reaction under a message of a
// conversation with one other person when either side blocked the other: a
// block stops writing in both directions, and a reaction is writing. Taking a
// reaction back never goes through here.
func checkDMReactionAllowed(ctx context.Context, tx pgx.Tx, messageID, userID uuid.UUID) error {
	blocked, err := DMBlockedAt(ctx, tx, messageID, userID)
	if err != nil {
		return err
	}
	if blocked {
		return core.ErrDMReact
	}
	return nil
}

// DMBlockedAt reports whether messageID is in a conversation with one other
// person, or in the chat of its Talk, and either side blocked the other: the
// condition under which neither may react to it (or vote in its poll).
func DMBlockedAt(ctx context.Context, tx pgx.Tx, messageID, userID uuid.UUID) (bool, error) {
	var blocked bool
	err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM messages m
			JOIN channels t ON t.id = m.channel_id
			JOIN channels c ON c.id = COALESCE(t.dm_id, t.id) AND c.type = $3 AND c.dm_key IS NOT NULL
			JOIN dm_members o ON o.channel_id = c.id AND o.user_id <> $2
			JOIN user_blocks b ON (b.blocker_id = o.user_id AND b.blocked_id = $2) OR (b.blocker_id = $2 AND b.blocked_id = o.user_id)
			WHERE m.id = $1)`, messageID, userID, core.ChannelKindDM).Scan(&blocked)
	if err != nil {
		return false, fmt.Errorf("check direct message block: %w", err)
	}
	return blocked, nil
}

// countDMMessage keeps the cap on the starter's messages before anybody
// answers, in counters that deleting messages does not reset.
func countDMMessage(ctx context.Context, tx pgx.Tx, nm NewMessage, st dmState) error {
	switch {
	case st.replied || nm.Record:
		return nil
	case st.creator != nil && *st.creator == nm.UserID:
		if st.sent >= core.MaxUnansweredDMMessages {
			return errDMWait
		}
		_, err := tx.Exec(ctx, `UPDATE channels SET dm_creator_sent = dm_creator_sent + 1 WHERE id = $1`, nm.ChannelID)
		return err
	}
	_, err := tx.Exec(ctx, `UPDATE channels SET dm_replied = TRUE WHERE id = $1`, nm.ChannelID)
	return err
}

// gateNewMemberFirstWrite keeps a member who is still new from opening a
// conversation: while their write wait runs they may answer someone who wrote
// first, but not write first (raid protection, doc/moderation.md).
func gateNewMemberFirstWrite(nm NewMessage, st dmState, wait time.Duration) error {
	if wait <= 0 || nm.Record {
		return nil
	}
	answering := st.replied || (st.creator != nil && *st.creator != nm.UserID && st.sent > 0)
	if answering {
		return nil
	}
	return perm.NewMemberWriteError(wait)
}
