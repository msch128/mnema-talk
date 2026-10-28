package auth

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/db"
)

// The datasets about what the member made or set up with the 1.0 features:
// their polls, the messages they pinned, their push devices and the emoji they
// uploaded. Like every dataset of an export they hold only the person's own
// data: no voter of a poll, no text of somebody else's message, no push keys.

type exportedPollOption struct {
	Label string `json:"label"`
	Votes int    `json:"votes"`
}

type exportedPoll struct {
	MessageID   uuid.UUID            `json:"message_id"`
	ChannelName *string              `json:"channel_name"`
	Question    string               `json:"question"`
	Multiple    bool                 `json:"multiple"`
	Anonymous   bool                 `json:"anonymous"`
	CreatedAt   time.Time            `json:"created_at"`
	ClosesAt    time.Time            `json:"closes_at"`
	Options     []exportedPollOption `json:"options"`
}

type pollRow struct {
	MessageID uuid.UUID `db:"message_id"`
	ChannelID uuid.UUID `db:"channel_id"`
	Channel   string    `db:"channel_name"`
	Question  string    `db:"question"`
	Multiple  bool      `db:"multiple"`
	Anonymous bool      `db:"anonymous"`
	CreatedAt time.Time `db:"created_at"`
	ClosesAt  time.Time `db:"closes_at"`
	Labels    []string  `db:"labels"`
	Votes     []int     `db:"votes"`
}

// exportPolls lists the polls the member started with their settings and the
// tally of each option (the counts, never who voted: also not in a poll that
// is not anonymous).
func exportPolls(ctx context.Context, q db.Querier, user uuid.UUID, viewable viewSet) (capped, error) {
	rows, cut, err := fetchRows[pollRow](ctx, q, exportMaxRows, `
		SELECT p.message_id, m.channel_id, c.name AS channel_name, m.content AS question, p.multiple, p.anonymous, p.created_at, p.closes_at,
		       array_agg(o.label ORDER BY o.position) AS labels,
		       array_agg(CASE WHEN p.anonymous THEN o.anon_votes ELSE (SELECT count(*)::int FROM poll_votes v WHERE v.option_id = o.id) END ORDER BY o.position) AS votes
		FROM polls p
		JOIN messages m ON m.id = p.message_id
		JOIN channels c ON c.id = m.channel_id
		JOIN poll_options o ON o.poll_id = p.message_id
		WHERE m.user_id = $1
		GROUP BY p.message_id, m.channel_id, c.name, m.content, p.multiple, p.anonymous, p.created_at, p.closes_at
		ORDER BY p.created_at LIMIT $2`, user)
	if err != nil {
		return capped{}, err
	}
	out := make([]any, len(rows))
	for i, r := range rows {
		poll := exportedPoll{MessageID: r.MessageID, Question: r.Question, Multiple: r.Multiple, Anonymous: r.Anonymous, CreatedAt: r.CreatedAt, ClosesAt: r.ClosesAt}
		if viewable.has(r.ChannelID) {
			poll.ChannelName = &r.Channel
		}
		for k, label := range r.Labels {
			poll.Options = append(poll.Options, exportedPollOption{Label: label, Votes: r.Votes[k]})
		}
		out[i] = poll
	}
	return capped{value: out, truncated: cut}, nil
}

// exportPins lists the messages the member pinned: which message, where and
// when, and whether it is still pinned. The text stays with its author.
func exportPins(ctx context.Context, q db.Querier, user uuid.UUID, viewable viewSet) (capped, error) {
	type pin struct {
		MessageID   uuid.UUID `json:"message_id" db:"message_id"`
		ChannelID   uuid.UUID `json:"channel_id" db:"channel_id"`
		ChannelName *string   `json:"channel_name" db:"channel_name"`
		PinnedAt    time.Time `json:"pinned_at" db:"pinned_at"`
		StillPinned bool      `json:"still_pinned" db:"is_pinned"`
	}
	ps, cut, err := fetchRows[pin](ctx, q, exportMaxRows, `
		SELECT m.id AS message_id, m.channel_id, c.name AS channel_name, m.pinned_at, m.is_pinned
		FROM messages m JOIN channels c ON c.id = m.channel_id
		WHERE m.pinned_by = $1 AND m.pinned_at IS NOT NULL ORDER BY m.pinned_at LIMIT $2`, user)
	if err != nil {
		return capped{}, err
	}
	for i := range ps {
		if !viewable.has(ps[i].ChannelID) {
			ps[i].ChannelName = nil
		}
	}
	return listOf(ps, cut), nil
}

// exportPush describes the member's push setup: the choice about message text
// and the devices (label, when added, last delivery). Never the endpoint or
// the keys: they are secrets of the device.
func exportPush(ctx context.Context, q db.Querier, user uuid.UUID) (capped, error) {
	type device struct {
		Label         string     `json:"label" db:"label"`
		CreatedAt     time.Time  `json:"created_at" db:"created_at"`
		LastSuccessAt *time.Time `json:"last_success_at" db:"last_success_at"`
	}
	devices, cut, err := fetchRows[device](ctx, q, exportMaxRows, `
		SELECT label, created_at, last_success_at FROM push_subscriptions WHERE user_id = $1 ORDER BY created_at LIMIT $2`, user)
	if err != nil {
		return capped{}, err
	}
	var showText bool
	if err := q.QueryRow(ctx, `SELECT push_preview FROM users WHERE id = $1`, user).Scan(&showText); err != nil {
		return capped{}, err
	}
	return capped{value: map[string]any{"show_message_text": showText, "devices": devices}, truncated: cut}, nil
}

type exportedEmoji struct {
	ID          uuid.UUID `json:"id" db:"id"`
	Name        string    `json:"name" db:"name"`
	ContentType string    `json:"content_type" db:"content_type"`
	Animated    bool      `json:"animated" db:"animated"`
	SizeBytes   int       `json:"size_bytes" db:"size_bytes"`
	CreatedAt   time.Time `json:"created_at" db:"created_at"`
	File        string    `json:"file,omitempty" db:"-"`
}

type emojiRow struct {
	exportedEmoji
	Key string `db:"s3_key"`
}

// exportEmoji lists the custom emoji the member uploaded and adds their image
// files to the archive under emoji/. An image that is gone from storage is
// listed without a file; any other failure fails the export.
func (h *Handler) exportEmoji(ctx context.Context, z *zipFiles, user uuid.UUID) (capped, error) {
	rows, cut, err := fetchRows[emojiRow](ctx, h.Sessions.DB, exportMaxRows, `
		SELECT id, name, content_type, animated, size_bytes, created_at, s3_key
		FROM custom_emoji WHERE created_by = $1 ORDER BY created_at LIMIT $2`, user)
	if err != nil {
		return capped{}, err
	}
	out := make([]exportedEmoji, len(rows))
	for i, r := range rows {
		e := r.exportedEmoji
		body, found, err := h.openExportObject(ctx, r.Key)
		if err != nil {
			return capped{}, err
		}
		if found {
			if e.File, err = copyEmojiImage(z.zw, e, body); err != nil {
				return capped{}, err
			}
		}
		out[i] = e
	}
	return listOf(out, cut), nil
}

// copyEmojiImage streams one emoji file into the archive and closes it.
func copyEmojiImage(zw *zip.Writer, e exportedEmoji, body io.ReadCloser) (string, error) {
	defer body.Close()
	ext := "png"
	if e.ContentType == "image/gif" {
		ext = "gif"
	}
	name := fmt.Sprintf("emoji/%s.%s", e.Name, ext)
	w, err := zw.Create(name)
	if err != nil {
		return "", err
	}
	_, err = io.Copy(w, body)
	return name, err
}
