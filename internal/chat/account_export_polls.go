package chat

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/userdata"
)

func pollVoteFile(ctx context.Context, q db.Querier, user uuid.UUID, limits userdata.Limits) userdata.File {
	return userdata.File{
		Name: "poll_votes.json", Order: userdata.FilePollVotes,
		Build: func() (any, bool, error) { return exportPollVotes(ctx, q, user, limits.MaxRows) },
	}
}

// exportPollVotes lists the person's own poll votes. For anonymous polls it
// lists only that the person took part: what they chose was never stored.
func exportPollVotes(ctx context.Context, q db.Querier, user uuid.UUID, maxRows int) (any, bool, error) {
	type vote struct {
		MessageID uuid.UUID `json:"message_id" db:"message_id"`
		Option    string    `json:"option" db:"label"`
		CreatedAt time.Time `json:"created_at" db:"created_at"`
	}
	vs, cut, err := userRows[vote](ctx, q, user, maxRows, `SELECT o.poll_id AS message_id, o.label, v.created_at
		FROM poll_votes v JOIN poll_options o ON o.id = v.option_id
		JOIN polls p ON p.message_id = o.poll_id AND NOT p.anonymous
		WHERE v.user_id = $1 ORDER BY v.created_at LIMIT $2`)
	if err != nil {
		return nil, false, err
	}
	type participation struct {
		MessageID uuid.UUID `json:"message_id" db:"message_id"`
		Anonymous bool      `json:"anonymous" db:"anonymous"`
	}
	// The votes and the polls joined without a recorded choice share one bound.
	ps, psCut, err := userdata.FetchRows[participation](ctx, q, maxRows-len(vs), `SELECT poll_id AS message_id, TRUE AS anonymous
		FROM poll_voters WHERE user_id = $1 LIMIT $2`, user)
	if err != nil {
		return nil, false, err
	}
	items := userdata.AsList(vs)
	for _, p := range ps {
		items = append(items, p)
	}
	return items, cut || psCut, nil
}

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

func pollFile(ctx context.Context, q db.Querier, user uuid.UUID, channels userdata.Channels, limits userdata.Limits) userdata.File {
	return userdata.File{
		Name: "polls.json", Order: userdata.FilePolls,
		Build: func() (any, bool, error) { return exportPolls(ctx, q, user, channels, limits.MaxRows) },
	}
}

// exportPolls lists the polls the member started, with the tally of each option
// and never who voted.
func exportPolls(ctx context.Context, q db.Querier, user uuid.UUID, channels userdata.Channels, maxRows int) (any, bool, error) {
	rows, cut, err := userRows[pollRow](ctx, q, user, maxRows, `
		SELECT p.message_id, m.channel_id, c.name AS channel_name, m.content AS question, p.multiple, p.anonymous, p.created_at, p.closes_at,
		       array_agg(o.label ORDER BY o.position) AS labels,
		       array_agg(CASE WHEN p.anonymous THEN o.anon_votes ELSE (SELECT count(*)::int FROM poll_votes v WHERE v.option_id = o.id) END ORDER BY o.position) AS votes
		FROM polls p
		JOIN messages m ON m.id = p.message_id
		JOIN channels c ON c.id = m.channel_id
		JOIN poll_options o ON o.poll_id = p.message_id
		WHERE m.user_id = $1
		GROUP BY p.message_id, m.channel_id, c.name, m.content, p.multiple, p.anonymous, p.created_at, p.closes_at
		ORDER BY p.created_at LIMIT $2`)
	if err != nil {
		return nil, false, err
	}
	out := make([]any, len(rows))
	for i, r := range rows {
		poll := exportedPoll{MessageID: r.MessageID, Question: r.Question, Multiple: r.Multiple, Anonymous: r.Anonymous, CreatedAt: r.CreatedAt, ClosesAt: r.ClosesAt}
		if channels.Has(r.ChannelID) {
			poll.ChannelName = &r.Channel
		}
		for k, label := range r.Labels {
			poll.Options = append(poll.Options, exportedPollOption{Label: label, Votes: r.Votes[k]})
		}
		out[i] = poll
	}
	return out, cut, nil
}

func pinFile(ctx context.Context, q db.Querier, user uuid.UUID, channels userdata.Channels, limits userdata.Limits) userdata.File {
	return userdata.File{
		Name: "pins.json", Order: userdata.FilePins,
		Build: func() (any, bool, error) { return exportPins(ctx, q, user, channels, limits.MaxRows) },
	}
}

// exportPins lists the messages the member pinned. The text stays with its author.
func exportPins(ctx context.Context, q db.Querier, user uuid.UUID, channels userdata.Channels, maxRows int) (any, bool, error) {
	type pin struct {
		MessageID   uuid.UUID `json:"message_id" db:"message_id"`
		ChannelID   uuid.UUID `json:"channel_id" db:"channel_id"`
		ChannelName *string   `json:"channel_name" db:"channel_name"`
		PinnedAt    time.Time `json:"pinned_at" db:"pinned_at"`
		StillPinned bool      `json:"still_pinned" db:"is_pinned"`
	}
	ps, cut, err := userRows[pin](ctx, q, user, maxRows, `
		SELECT m.id AS message_id, m.channel_id, c.name AS channel_name, m.pinned_at, m.is_pinned
		FROM messages m JOIN channels c ON c.id = m.channel_id
		WHERE m.pinned_by = $1 AND m.pinned_at IS NOT NULL ORDER BY m.pinned_at LIMIT $2`)
	if err != nil {
		return nil, false, err
	}
	for i := range ps {
		if !channels.Has(ps[i].ChannelID) {
			ps[i].ChannelName = nil
		}
	}
	return userdata.AsList(ps), cut, nil
}
