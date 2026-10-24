package chat

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/db"
)

// Member is a member as the member list shows them.
type Member struct {
	auth.User
	// RoleIDs are the member's custom roles, highest first.
	RoleIDs []uuid.UUID `json:"role_ids,omitempty" binding:"optional" format:"uuid"`
	// TimeoutUntil is set while the member is timed out: they can read but
	// not post, react, speak or join Talks.
	TimeoutUntil *time.Time `json:"timeout_until,omitempty" binding:"optional" format:"date-time"`
	// IsBot is true for a bot account (see doc/bots.md). Bots hold the
	// built-in Bot role; webhook authors are never listed.
	IsBot bool `json:"is_bot,omitempty" binding:"optional"`
	// AddedBy is the member who created the bot, when known.
	AddedBy *uuid.UUID `json:"added_by,omitempty" binding:"optional" format:"uuid"`
}

// GetAllMembers lists active accounts, admins first, then by display name:
// people and bots (an enabled bot is a member; the hidden accounts of
// incoming webhooks are not). message_count is maintained by triggers
// (migration 0012), so this does not scan the messages table.
func GetAllMembers(ctx context.Context, p *db.Pool) ([]Member, error) {
	rows, err := p.Query(ctx, `
		SELECT u.id, u.username, u.display_name, u.bio, u.role, u.avatar_s3_key, `+auth.StatusSQLFor("u.")+`, u.created_at,
		       u.voice_seconds, u.message_count, b.id IS NOT NULL, b.created_by
		FROM users u LEFT JOIN bots b ON b.user_id = u.id
		WHERE `+auth.ActiveSQL("u")+` AND (NOT u.is_bot OR (b.id IS NOT NULL AND NOT b.disabled))
		ORDER BY CASE WHEN u.role = 'admin' THEN 0 ELSE 1 END, u.display_name`)
	if err != nil {
		return nil, fmt.Errorf("query members: %w", err)
	}
	defer rows.Close()

	members := make([]Member, 0)
	for rows.Next() {
		var m Member
		var avatar *string
		if err := rows.Scan(&m.ID, &m.Username, &m.DisplayName, &m.Bio, &m.Role, &avatar, &m.StatusText, &m.StatusExpiresAt, &m.CreatedAt,
			&m.VoiceSeconds, &m.MessageCount, &m.IsBot, &m.AddedBy); err != nil {
			return nil, err
		}
		m.AvatarURL = auth.AvatarURL(avatar)
		members = append(members, m)
	}
	return members, rows.Err()
}
