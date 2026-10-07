package chat

import (
	"context"
	"fmt"

	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/db"
)

// GetAllMembers lists active accounts, admins first, then by display name.
// message_count is maintained by triggers (migration 0012), so this does not
// scan the messages table.
func GetAllMembers(ctx context.Context, p *db.Pool) ([]auth.User, error) {
	rows, err := p.Query(ctx, `
		SELECT id, username, display_name, bio, role, avatar_s3_key, status_text, created_at,
		       voice_seconds, message_count
		FROM users
		WHERE disabled_at IS NULL
		ORDER BY CASE WHEN role = 'admin' THEN 0 ELSE 1 END, display_name`)
	if err != nil {
		return nil, fmt.Errorf("query members: %w", err)
	}
	defer rows.Close()

	members := make([]auth.User, 0)
	for rows.Next() {
		var m auth.User
		var avatar *string
		if err := rows.Scan(&m.ID, &m.Username, &m.DisplayName, &m.Bio, &m.Role, &avatar, &m.StatusText, &m.CreatedAt,
			&m.VoiceSeconds, &m.MessageCount); err != nil {
			return nil, err
		}
		m.AvatarURL = auth.AvatarURL(avatar)
		members = append(members, m)
	}
	return members, rows.Err()
}
