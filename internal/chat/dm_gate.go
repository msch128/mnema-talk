package chat

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/httpx"
)

var errDMWait = httpx.NewAPIError(http.StatusTooManyRequests, httpx.CodeRateLimited,
	fmt.Sprintf("Wait for a reply before you send more than %d messages.", MaxUnansweredDMMessages))

// gatePost applies what a new message needs besides the permission checks of
// the request, inside the transaction that stores it: slow mode in a channel,
// the direct message rules in a conversation. It runs for every way a message
// is created (members, uploads, polls, commands), so none can go around it.
func gatePost(ctx context.Context, tx pgx.Tx, nm NewMessage) error {
	var kind string
	var seconds int
	err := tx.QueryRow(ctx, `SELECT type, slow_mode_seconds FROM channels WHERE id = $1`, nm.ChannelID).Scan(&kind, &seconds)
	if errors.Is(err, pgx.ErrNoRows) {
		return errChannelNotFound
	}
	if err != nil {
		return fmt.Errorf("load channel: %w", err)
	}
	if kind == channelKindDM {
		return gateDM(ctx, tx, nm)
	}
	if nm.ParentID != nil {
		if err := gateReply(ctx, tx, *nm.ParentID); err != nil {
			return err
		}
	} else if kind == "forum" && (nm.Title == nil || *nm.Title == "") {
		return errPostTitleRequired
	}
	if nm.WebhookID == nil && !slowModeExempt(ctx) {
		return enforceSlowMode(ctx, tx, nm.ChannelID, nm.UserID, seconds)
	}
	return nil
}

func gateReply(ctx context.Context, tx pgx.Tx, parentID uuid.UUID) error {
	var isLocked bool
	err := tx.QueryRow(ctx, `SELECT is_locked FROM messages WHERE id = $1`, parentID).Scan(&isLocked)
	if errors.Is(err, pgx.ErrNoRows) {
		return httpx.ErrInvalidInput("parent message not found")
	}
	if err != nil {
		return fmt.Errorf("check thread lock: %w", err)
	}
	if isLocked && !slowModeExempt(ctx) {
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
	st.members, err = DMMemberIDs(ctx, tx, id)
	return st, err
}

// gateDM decides whether nm may be written in a conversation. Everything that
// concerns another member (a block, a closed or disabled account) answers
// errDMSend, which says nothing about why.
func gateDM(ctx context.Context, tx pgx.Tx, nm NewMessage) error {
	if nm.WebhookID != nil {
		return errDMSend // webhooks and bots never write in a conversation
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
		return errChannelNotFound
	}
	if err := checkDMReachable(ctx, tx, nm.UserID, others, st.group); err != nil {
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
	var on bool
	var active, blocked int
	err := tx.QueryRow(ctx, `
		SELECT (SELECT direct_messages_enabled FROM instance_settings),
		       COUNT(*), COUNT(*) FILTER (WHERE EXISTS (
		           SELECT 1 FROM user_blocks b WHERE (b.blocker_id = u.id AND b.blocked_id = $1) OR (b.blocker_id = $1 AND b.blocked_id = u.id)))
		FROM users u WHERE u.id = ANY($2::uuid[]) AND `+auth.ActiveSQL("u")+` AND NOT u.is_bot`, sender, others).Scan(&on, &active, &blocked)
	if err != nil {
		return fmt.Errorf("check direct message: %w", err)
	}
	if !on || active == 0 || (!group && (active != len(others) || blocked > 0)) {
		return errDMSend
	}
	return nil
}

// countDMMessage keeps the cap on the starter's messages before anybody
// answers, in counters that deleting messages does not reset.
func countDMMessage(ctx context.Context, tx pgx.Tx, nm NewMessage, st dmState) error {
	switch {
	case st.replied:
		return nil
	case st.creator != nil && *st.creator == nm.UserID:
		if st.sent >= MaxUnansweredDMMessages {
			return errDMWait
		}
		_, err := tx.Exec(ctx, `UPDATE channels SET dm_creator_sent = dm_creator_sent + 1 WHERE id = $1`, nm.ChannelID)
		return err
	}
	_, err := tx.Exec(ctx, `UPDATE channels SET dm_replied = TRUE WHERE id = $1`, nm.ChannelID)
	return err
}
