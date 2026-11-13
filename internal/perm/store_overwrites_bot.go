package perm

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/httpx"
)

var errNotBotOwnRole = errors.New("overwrite target is not a bot's own role")

// AllowBotRoleInChannel adds allow to the overwrite of a bot's own managed
// role in a channel (doc/bots.md, "Adding a bot from a manifest"). It must run
// in the transaction of LockedCreatingBot: it checks nothing about the actor,
// because the bot invariant at the end of that transaction does (nothing
// elevated, nothing the actor lacks in the channel). That is also why it needs
// no Manage channels: the role is held by its bot only and cannot be given to
// anyone, so the overwrite changes nothing for any member.
//
// The caller makes sure ch is a channel the actor can view and not a direct
// message conversation or its Talk. A role that is not a bot's own is refused
// with an internal error; a temporary Talk with a conflict, as SetOverwrites
// does, and temporary Talks opened from ch mirror the change.
func AllowBotRoleInChannel(ctx context.Context, tx pgx.Tx, role, ch uuid.UUID, allow Set) error {
	if allow&^ChannelScoped != 0 {
		return httpx.ErrInvalidInput("these permissions cannot be overwritten per channel")
	}
	var own bool
	if err := tx.QueryRow(ctx, `SELECT bot_user_id IS NOT NULL FROM roles WHERE id = $1`, role).Scan(&own); err != nil {
		return fmt.Errorf("load bot role: %w", err)
	}
	if !own {
		return errNotBotOwnRole
	}
	if err := refuseTempTalk(ctx, tx, TargetChannel, ch); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO permission_overwrites (channel_id, role_id, allow, deny) VALUES ($1, $2, $3, 0)
		ON CONFLICT (channel_id, role_id) WHERE channel_id IS NOT NULL AND role_id IS NOT NULL
		DO UPDATE SET allow = permission_overwrites.allow | EXCLUDED.allow,
		              deny = permission_overwrites.deny & ~EXCLUDED.allow`,
		ch, role, int64(allow)); err != nil {
		return fmt.Errorf("allow bot role: %w", err)
	}
	return syncTempTalks(ctx, tx, ch)
}
