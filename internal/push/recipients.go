package push

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/chat"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/perm"
)

// candidate is an account with at least one subscription and what decides
// whether this message reaches it.
type candidate struct {
	userID      uuid.UUID
	locale      string
	preview     bool
	presence    string
	channelName string
	channelSlug string
	// dm is set when the channel is a direct message conversation: every
	// message in it is meant for the member (not just a mention), and a
	// mute still silences it.
	dm           bool
	channelLevel chat.NotifySetting
	categoryLvl  chat.NotifySetting
	// serverLvl is the member's server-wide default (never applied to a
	// conversation); suppressAll ignores @all and @here.
	serverLvl   chat.NotifySetting
	suppressAll bool
	// silenced is set for a conversation when the member muted all direct
	// messages and the conversation has no setting of its own: nothing is
	// pushed, not even a mention.
	silenced bool
	// now is the database's clock, the one the stored mute ends use.
	now time.Time
}

// subscriberIDs lists the accounts a message of authorID can be pushed to
// at all: with a valid subscription, not disabled and not blocking the author.
// It is the cheap first step, so the permission check can drop everyone who
// may not see the channel before their notification settings are read.
func subscriberIDs(ctx context.Context, q db.Querier, authorID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := q.Query(ctx, `
		SELECT u.id FROM users u
		WHERE u.id <> $1 AND u.disabled_at IS NULL
		  AND NOT EXISTS (SELECT 1 FROM user_blocks ub WHERE ub.blocker_id = u.id AND ub.blocked_id = $1)
		  AND EXISTS (SELECT 1 FROM push_subscriptions s WHERE s.user_id = u.id AND `+validSession+`)`, authorID)
	if err != nil {
		return nil, fmt.Errorf("load push subscribers: %w", err)
	}
	defer rows.Close()
	var out []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("load push subscribers: %w", err)
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// loadCandidates reads, for one channel, every account with a subscription
// (except the author) together with its notification settings, in one query.
// A member who blocked the author is never a candidate. Only limits the result to those accounts.
func loadCandidates(ctx context.Context, q db.Querier, channelID, authorID uuid.UUID, only []uuid.UUID) ([]candidate, error) {
	rows, err := q.Query(ctx, `
		SELECT u.id, u.locale, u.push_preview, u.presence, c.name, COALESCE(c.slug, ''), c.type = 'dm',
		       cr.notify_level, cr.muted_until, cn.notify_level, cn.muted_until,
		       sn.notify_level, sn.muted_until, COALESCE(sn.suppress_everyone, FALSE), NOW(), u.dm_mute_all
		FROM users u
		JOIN channels c ON c.id = $1
		LEFT JOIN channel_reads cr ON cr.user_id = u.id AND cr.channel_id = c.id
		LEFT JOIN category_notify cn ON cn.user_id = u.id AND cn.category_id = c.category_id
		LEFT JOIN server_notify sn ON sn.user_id = u.id
		WHERE u.id <> $2 AND u.id = ANY($3) AND u.disabled_at IS NULL
		  AND NOT EXISTS (SELECT 1 FROM user_blocks ub WHERE ub.blocker_id = u.id AND ub.blocked_id = $2)
		  AND EXISTS (SELECT 1 FROM push_subscriptions s WHERE s.user_id = u.id AND `+validSession+`)`, channelID, authorID, only)
	if err != nil {
		return nil, fmt.Errorf("load push candidates: %w", err)
	}
	defer rows.Close()
	var out []candidate
	for rows.Next() {
		var c candidate
		var chLevel, catLevel, srvLevel *string
		var muteAll bool
		if err := rows.Scan(&c.userID, &c.locale, &c.preview, &c.presence, &c.channelName, &c.channelSlug, &c.dm,
			&chLevel, &c.channelLevel.MutedUntil, &catLevel, &c.categoryLvl.MutedUntil,
			&srvLevel, &c.serverLvl.MutedUntil, &c.suppressAll, &c.now, &muteAll); err != nil {
			return nil, fmt.Errorf("load push candidates: %w", err)
		}
		c.channelLevel.Level, c.categoryLvl.Level, c.serverLvl.Level = levelOf(chLevel), levelOf(catLevel), levelOf(srvLevel)
		if c.dm {
			c.serverLvl, c.suppressAll = chat.NotifySetting{}, false
			c.silenced = muteAll && !c.channelLevel.Decides(c.now)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func levelOf(stored *string) chat.NotifyLevel {
	if stored == nil {
		return chat.NotifyInherit
	}
	return chat.NotifyLevel(*stored)
}

// mention reports whether msg counts as addressed to the member for the
// notification level: a mention or reply, or any message of a conversation.
// A member who suppresses @all and @here is only mentioned by name, through a
// role or as a reply (byName, see namedMentions).
func (c candidate) mention(msg chat.Message, byName map[uuid.UUID]bool) bool {
	if c.suppressAll {
		return c.dm || addressed(msg, c.userID, byName)
	}
	return c.dm || mentions(msg, c.userID)
}

// quiet reports a presence that silences every notification, like the page does.
func quiet(presence string) bool {
	return presence == "dnd" || presence == "focus"
}

// wants decides whether the message is pushed to this account: the account's
// effective level for the channel (own setting, else the category's, else the server default, else all;
// expired mutes ignored) must allow it, and do-not-disturb must be off. A mute
// still lets through a message addressed to the member (a mention by name or
// role, or a reply), but not @all or @here.
func (c candidate) wants(mention, addressed bool) bool {
	if quiet(c.presence) || c.silenced {
		return false
	}
	switch chat.ResolveNotify(c.now, c.channelLevel, c.categoryLvl, c.serverLvl) {
	case chat.NotifyAll:
		return true
	case chat.NotifyMentions:
		return mention
	case chat.NotifyMute:
		return addressed
	}
	return false
}

// canView reports whether the account may see the channel right now. The
// check uses the permission snapshot of the moment of sending, not the one of
// the subscription, so a member who lost access stops being told at once.
func canView(snap *perm.Snapshot, userID, channelID uuid.UUID) bool {
	return snap.Channel(perm.Subject{ID: userID}, channelID).Has(perm.ViewChannel)
}

// threadAudience returns the followers of the thread a message replies to. A
// nil map with ok means the message is not a thread reply and has no such
// limit; ok is false when the followers cannot be read.
func threadAudience(ctx context.Context, q db.Querier, thread *uuid.UUID) (followers map[uuid.UUID]bool, ok bool) {
	if thread == nil {
		return nil, true
	}
	followers, err := chat.ThreadFollowers(ctx, q, *thread)
	if err != nil {
		slog.Warn("push: load thread followers", "err", err)
		return nil, false
	}
	return followers, true
}

// namedMentions returns the members msg is addressed to by name or through a
// role (not through @all or @here). When the stored mentions cannot be read
// nobody counts as addressed, so a mute stays silent.
func namedMentions(ctx context.Context, q db.Querier, messageID uuid.UUID) map[uuid.UUID]bool {
	rows, err := q.Query(ctx, `SELECT user_id FROM message_mentions WHERE message_id = $1 AND kind <> 1`, messageID)
	if err != nil {
		slog.Warn("push: load mentions", "err", err)
		return nil
	}
	defer rows.Close()
	out := map[uuid.UUID]bool{}
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			slog.Warn("push: read mentions", "err", err)
			return nil
		}
		out[id] = true
	}
	return out
}
