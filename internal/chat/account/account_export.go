package account

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/userdata"
)

// exportEdits lists the earlier versions of the person's own edited messages,
// newest first. Only their own messages and their own text.
func exportEdits(ctx context.Context, q db.Querier, user uuid.UUID, max int) (any, error) {
	type version struct {
		MessageID  uuid.UUID `json:"message_id" db:"message_id"`
		Content    string    `json:"content" db:"content"`
		EditedAt   time.Time `json:"edited_at" db:"edited_at"`
		ReplacedAt time.Time `json:"replaced_at" db:"replaced_at"`
	}
	vs, err := userdata.Collect(ctx, q, pgx.RowToStructByName[version], `SELECT e.message_id, e.content, e.edited_at, e.replaced_at
		FROM message_edits e JOIN messages m ON m.id = e.message_id
		WHERE e.editor_id = $1 AND m.user_id = $1 ORDER BY e.id DESC LIMIT $2`, user, max+1)
	if err != nil {
		return nil, err
	}
	truncated := len(vs) > max
	if truncated {
		vs = vs[:max]
	}
	return map[string]any{"edits": vs, "truncated": truncated}, nil
}

func editFile(ctx context.Context, q db.Querier, user uuid.UUID, limits userdata.Limits) userdata.File {
	return userdata.File{
		Name: "edits.json", Order: userdata.FileEdits,
		Build: func() (any, bool, error) {
			v, err := exportEdits(ctx, q, user, limits.MaxEdits)
			return v, false, err
		},
	}
}

func reactionFile(ctx context.Context, q db.Querier, user uuid.UUID, limits userdata.Limits) userdata.File {
	type reaction struct {
		MessageID uuid.UUID `json:"message_id" db:"message_id"`
		Emoji     string    `json:"emoji" db:"emoji"`
		CreatedAt time.Time `json:"created_at" db:"created_at"`
	}
	return listFile("reactions.json", userdata.FileReactions, func() ([]reaction, bool, error) {
		return userRows[reaction](ctx, q, user, limits.MaxRows, `SELECT message_id, emoji, created_at
		FROM message_reactions WHERE user_id = $1 ORDER BY created_at LIMIT $2`)
	})
}

func uploadFile(ctx context.Context, q db.Querier, user uuid.UUID, limits userdata.Limits) userdata.File {
	type upload struct {
		ID        uuid.UUID  `json:"id" db:"id"`
		Filename  string     `json:"filename" db:"original_filename"`
		MimeType  string     `json:"mime_type" db:"mime_type"`
		SizeBytes int64      `json:"size_bytes" db:"size_bytes"`
		MessageID *uuid.UUID `json:"message_id,omitempty" db:"message_id"`
		Deleted   bool       `json:"deleted" db:"is_deleted"`
		CreatedAt time.Time  `json:"created_at" db:"created_at"`
		URL       string     `json:"url,omitempty" db:"-"`
	}
	return userdata.File{
		Name: "uploads.json", Order: userdata.FileUploads,
		Build: func() (any, bool, error) {
			us, cut, err := userRows[upload](ctx, q, user, limits.MaxRows, `SELECT id, original_filename, mime_type, size_bytes, message_id, is_deleted, created_at
		FROM media WHERE uploader_id = $1 AND NOT staged ORDER BY created_at, position LIMIT $2`)
			if err != nil {
				return nil, false, err
			}
			for i := range us {
				if !us[i].Deleted {
					us[i].URL = "/api/media/" + us[i].ID.String()
				}
			}
			return userdata.AsList(us), cut, nil
		},
	}
}

func threadFollowFile(ctx context.Context, q db.Querier, user uuid.UUID, limits userdata.Limits) userdata.File {
	type follow struct {
		ThreadID  uuid.UUID `json:"thread_id" db:"thread_id"`
		Following bool      `json:"following" db:"following"`
		Since     time.Time `json:"since" db:"created_at"`
	}
	return listFile("thread_follows.json", userdata.FileThreadFollows, func() ([]follow, bool, error) {
		return userRows[follow](ctx, q, user, limits.MaxRows, `SELECT thread_id, following, created_at
		FROM thread_follows WHERE user_id = $1 ORDER BY created_at, thread_id LIMIT $2`)
	})
}

func directMessageFile(ctx context.Context, q db.Querier, user uuid.UUID, limits userdata.Limits) userdata.File {
	type conversation struct {
		ID        uuid.UUID   `json:"id" db:"id"`
		Group     bool        `json:"group" db:"is_group"`
		StartedAt time.Time   `json:"started_at" db:"created_at"`
		JoinedAt  time.Time   `json:"joined_at" db:"joined_at"`
		MemberIDs []uuid.UUID `json:"member_ids" db:"member_ids"`
	}
	return listFile("direct_messages.json", userdata.FileDirectMessages, func() ([]conversation, bool, error) {
		return userRows[conversation](ctx, q, user, limits.MaxRows, `
		SELECT c.id, c.dm_key IS NULL AS is_group, c.created_at, me.joined_at,
		       (SELECT array_agg(m.user_id ORDER BY m.joined_at, m.user_id) FROM dm_members m WHERE m.channel_id = c.id) AS member_ids
		FROM dm_members me JOIN channels c ON c.id = me.channel_id
		WHERE me.user_id = $1 ORDER BY c.created_at LIMIT $2`)
	})
}

func blockFile(ctx context.Context, q db.Querier, user uuid.UUID, limits userdata.Limits) userdata.File {
	type block struct {
		UserID    uuid.UUID `json:"user_id" db:"blocked_id"`
		BlockedAt time.Time `json:"blocked_at" db:"created_at"`
	}
	return listFile("blocks.json", userdata.FileBlocks, func() ([]block, bool, error) {
		return userRows[block](ctx, q, user, limits.MaxRows, `SELECT blocked_id, created_at FROM user_blocks
		WHERE blocker_id = $1 ORDER BY created_at LIMIT $2`)
	})
}

func reportFile(ctx context.Context, q db.Querier, user uuid.UUID, limits userdata.Limits) userdata.File {
	type report struct {
		MessageID uuid.UUID `json:"message_id" db:"message_id"`
		Kind      string    `json:"kind" db:"kind"`
		Category  string    `json:"category" db:"category"`
		Note      string    `json:"note" db:"note"`
		Status    string    `json:"status" db:"status"`
		CreatedAt time.Time `json:"created_at" db:"created_at"`
	}
	return listFile("reports.json", userdata.FileReports, func() ([]report, bool, error) {
		return userRows[report](ctx, q, user, limits.MaxRows, `SELECT message_id, kind, category, note, status, created_at
		FROM message_reports WHERE reporter_id = $1 ORDER BY created_at LIMIT $2`)
	})
}

func savedFile(ctx context.Context, q db.Querier, user uuid.UUID, limits userdata.Limits) userdata.File {
	type saved struct {
		MessageID uuid.UUID `json:"message_id" db:"message_id"`
		Note      string    `json:"note" db:"note"`
		SavedAt   time.Time `json:"saved_at" db:"created_at"`
	}
	return listFile("saved.json", userdata.FileSaved, func() ([]saved, bool, error) {
		return userRows[saved](ctx, q, user, limits.MaxRows, `SELECT message_id, note, created_at
		FROM saved_messages WHERE user_id = $1 ORDER BY created_at LIMIT $2`)
	})
}
