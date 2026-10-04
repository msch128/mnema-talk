package chat

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/db"
)

type Member struct {
	ID          uuid.UUID  `json:"id"`
	Username    string     `json:"username"`
	DisplayName string     `json:"display_name"`
	Bio         string     `json:"bio"`
	Role        string     `json:"role"`
	AvatarURL   *string    `json:"avatar_url,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}

// GetAllMembers retrieves all community members ordered by role and name
func GetAllMembers(ctx context.Context, p *db.Pool) ([]Member, error) {
	rows, err := p.Query(ctx, `
		SELECT id, username, display_name, COALESCE(bio, ''), role, avatar_s3_key, created_at 
		FROM users 
		ORDER BY 
			CASE WHEN role = 'admin' THEN 0 ELSE 1 END,
			display_name ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to query members: %w", err)
	}
	defer rows.Close()

	members := make([]Member, 0)
	for rows.Next() {
		var m Member
		var avatarKey *string
		if err := rows.Scan(&m.ID, &m.Username, &m.DisplayName, &m.Bio, &m.Role, &avatarKey, &m.CreatedAt); err != nil {
			return nil, err
		}
		if avatarKey != nil && *avatarKey != "" {
			url := fmt.Sprintf("/api/media/%s", *avatarKey)
			m.AvatarURL = &url
		}
		members = append(members, m)
	}

	return members, nil
}
