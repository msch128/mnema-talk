package perm

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// BotChannelPerms are the only permissions a bot's access choice may grant:
// what a bot uses in channels. Never Administrator or anything of the admin
// area; those stay a deliberate grant by hand (doc/bots.md).
const BotChannelPerms = ViewChannel | SendMessages | AttachFiles | AddReactions | ManageMessages | MentionEveryone

// BotAccess is the "Access" choice of a bot (doc/bots.md, "Give a bot access
// to every channel"): the permissions Allow, either in the listed channels
// only (All false) or server-wide on its own role (All true).
type BotAccess struct {
	// Role is the bot's own managed role.
	Role uuid.UUID
	All  bool
	// Allow is the whole set the bot should have, within BotChannelPerms.
	Allow Set
	// Channels are the channels of an overwrite for the own role: the
	// bot's channels when All is false, the private channels it may also see
	// when All is true.
	Channels []uuid.UUID
}

// ApplyBotAccess stores the choice on the bot's own role. It must run in a
// transaction of Locked or LockedCreatingBot: it checks nothing about the
// actor, because the bot invariant at the end of that transaction does
// (nothing elevated, nothing the actor lacks in any channel). The caller
// makes sure every channel is one the actor can view and not a conversation.
//
// All: the own role holds Allow server-wide (the rest of its channel
// permissions are replaced) and the bot is marked, so a channel that plain
// members cannot see stays hidden from it unless an overwrite of the role
// allows it. Not All: the server-wide grants go, the own role's allow
// overwrites become exactly Channels with Allow, and the mark goes.
func ApplyBotAccess(ctx context.Context, tx pgx.Tx, a BotAccess) error {
	if a.Allow&^BotChannelPerms != 0 {
		return httpx.ErrInvalidInput("a bot's access can only grant channel permissions: view, send, attach, react, manage messages, mention everyone")
	}
	if err := requireBotOwnRole(ctx, tx, a.Role); err != nil {
		return err
	}
	server := Set(0)
	if a.All {
		server = a.Allow
	}
	if _, err := tx.Exec(ctx, `UPDATE roles SET permissions = (permissions & ~$2::bigint) | $3::bigint, updated_at = NOW() WHERE id = $1`,
		a.Role, int64(BotChannelPerms), int64(server)); err != nil {
		return fmt.Errorf("set bot role permissions: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE bots SET access_all = $2 WHERE user_id = (SELECT bot_user_id FROM roles WHERE id = $1)`, a.Role, a.All); err != nil {
		return fmt.Errorf("mark bot access: %w", err)
	}
	if err := clearBotOverwrites(ctx, tx, a); err != nil {
		return err
	}
	if a.Allow == 0 {
		return nil
	}
	for _, ch := range a.Channels {
		if err := AllowBotRoleInChannel(ctx, tx, a.Role, ch, a.Allow); err != nil {
			return err
		}
	}
	return nil
}

// clearBotOverwrites takes the bot permissions out of the allow side of
// the own role's overwrites in every channel (the listed ones get them again
// with the new set), and removes the
// overwrites that end up empty. Denies stay: they are the admin's.
func clearBotOverwrites(ctx context.Context, tx pgx.Tx, a BotAccess) error {
	role := a.Role
	rows, err := tx.Query(ctx, `
		UPDATE permission_overwrites SET allow = allow & ~$2::bigint
		WHERE role_id = $1 AND channel_id IS NOT NULL AND allow & $2::bigint <> 0
		RETURNING channel_id`, role, int64(BotChannelPerms))
	if err != nil {
		return fmt.Errorf("drop bot overwrites: %w", err)
	}
	changed, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return fmt.Errorf("drop bot overwrites: %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM permission_overwrites WHERE role_id = $1 AND channel_id IS NOT NULL AND allow = 0 AND deny = 0`, role); err != nil {
		return fmt.Errorf("drop empty bot overwrites: %w", err)
	}
	for _, ch := range changed {
		if err := syncTempTalks(ctx, tx, ch); err != nil {
			return err
		}
	}
	return nil
}

// SetBotAccess changes the access of an existing bot (PUT
// /api/admin/bots/{id}/access). It needs Manage bots, the authority over the
// bot that every bot change needs, and passes the bot invariant: only the
// Owner can cause a bot to hold anything elevated, and nobody gives a bot a
// permission they lack in a channel.
func (s *Service) SetBotAccess(ctx context.Context, actor Subject, botUser uuid.UUID, a BotAccess) error {
	return s.mutate(ctx, actor, ManageBots, func(tx pgx.Tx, snap *Snapshot) ([]uuid.UUID, error) {
		if err := snap.CanActAs(actor, Subject{ID: botUser}); err != nil {
			return nil, err
		}
		if err := CheckBotChannels(snap, actor, a.Channels); err != nil {
			return nil, err
		}
		if err := tx.QueryRow(ctx, `SELECT id FROM roles WHERE bot_user_id = $1`, botUser).Scan(&a.Role); err != nil {
			return nil, fmt.Errorf("load bot role: %w", err)
		}
		if err := ApplyBotAccess(ctx, tx, a); err != nil {
			return nil, err
		}
		return []uuid.UUID{botUser}, nil
	})
}

// CheckBotChannels makes sure every channel is a channel of the community the
// actor can view; anything else looks like a hidden channel.
func CheckBotChannels(snap *Snapshot, actor Subject, channels []uuid.UUID) error {
	for _, ch := range channels {
		if !snap.KnowsChannel(ch) || snap.IsPrivate(ch) || !snap.Channel(actor, ch).Has(ViewChannel) {
			return ErrChannelHidden
		}
	}
	return nil
}

// HasAllChannelAccess reports whether the bot was given access to every
// channel (see BotAccess).
func (s *Snapshot) HasAllChannelAccess(bot uuid.UUID) bool { return s.botsAll[bot] }
