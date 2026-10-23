package push

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/chat"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/perm"
)

// candidate is an account with at least one subscription and what decides
// whether this message reaches it.
type candidate struct {
	userID       uuid.UUID
	locale       string
	preview      bool
	presence     string
	channelName  string
	channelLevel chat.NotifySetting
	categoryLvl  chat.NotifySetting
	// now is the database's clock, the one the stored mute ends use.
	now time.Time
}

// loadCandidates reads, for one channel, every account with a subscription
// (except the author) together with its notification settings, in one query.
// A non-nil only limits the result to that account.
func loadCandidates(ctx context.Context, q db.Querier, channelID, authorID uuid.UUID, only *uuid.UUID) ([]candidate, error) {
	rows, err := q.Query(ctx, `
		SELECT u.id, u.locale, u.push_preview, u.presence, c.name,
		       cr.notify_level, cr.muted_until, cn.notify_level, cn.muted_until, NOW()
		FROM users u
		JOIN channels c ON c.id = $1
		LEFT JOIN channel_reads cr ON cr.user_id = u.id AND cr.channel_id = c.id
		LEFT JOIN category_notify cn ON cn.user_id = u.id AND cn.category_id = c.category_id
		WHERE u.id <> $2 AND ($3::uuid IS NULL OR u.id = $3) AND u.disabled_at IS NULL
		  AND EXISTS (SELECT 1 FROM push_subscriptions s WHERE s.user_id = u.id AND `+validSession+`)`, channelID, authorID, only)
	if err != nil {
		return nil, fmt.Errorf("load push candidates: %w", err)
	}
	defer rows.Close()
	var out []candidate
	for rows.Next() {
		var c candidate
		var chLevel, catLevel *string
		if err := rows.Scan(&c.userID, &c.locale, &c.preview, &c.presence, &c.channelName,
			&chLevel, &c.channelLevel.MutedUntil, &catLevel, &c.categoryLvl.MutedUntil, &c.now); err != nil {
			return nil, fmt.Errorf("load push candidates: %w", err)
		}
		c.channelLevel.Level, c.categoryLvl.Level = levelOf(chLevel), levelOf(catLevel)
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

// quiet reports a presence that silences every notification, like the page does.
func quiet(presence string) bool {
	return presence == "dnd" || presence == "focus"
}

// wants decides whether the message is pushed to this account: the account's
// effective level for the channel (own setting, else the category's, else all;
// expired mutes ignored) must allow it, and do-not-disturb must be off.
func (c candidate) wants(mention bool) bool {
	if quiet(c.presence) {
		return false
	}
	switch chat.ResolveNotify(c.now, c.channelLevel, c.categoryLvl) {
	case chat.NotifyAll:
		return true
	case chat.NotifyMentions:
		return mention
	}
	return false
}

// canView reports whether the account may see the channel right now. The
// check uses the permission snapshot of the moment of sending, not the one of
// the subscription, so a member who lost access stops being told at once.
func canView(snap *perm.Snapshot, userID, channelID uuid.UUID) bool {
	return snap.Channel(perm.Subject{ID: userID}, channelID).Has(perm.ViewChannel)
}
