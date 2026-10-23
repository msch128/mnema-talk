package chat

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/db"
)

// Poll is the poll attached to a message (created by /poll or the composer's
// poll button; the message's content is the question). It is the same for
// every viewer, so it travels in message_create and in message lists: counts,
// and for a poll that is not anonymous the voters of each option. A viewer's
// own votes are asked for separately (GET .../poll), which is how an
// anonymous poll shows a member their own choice and nobody else's.
type Poll struct {
	// Multiple lets a member vote for several options.
	Multiple bool `json:"multiple"`
	// Anonymous polls never name voters: options carry counts only.
	Anonymous bool `json:"anonymous"`
	// ClosesAt is when voting ends; ending the poll early moves it to that moment.
	ClosesAt time.Time `json:"closes_at" format:"date-time"`
	// Closed is true once ClosesAt has passed, as the server saw it.
	Closed  bool         `json:"closed"`
	Options []PollOption `json:"options"`
	// TotalVoters counts members who voted for at least one option.
	TotalVoters int `json:"total_voters"`
}

// PollOption is one answer with its tally.
type PollOption struct {
	ID    uuid.UUID `json:"id" format:"uuid"`
	Label string    `json:"label" maxLength:"55"`
	Votes int       `json:"votes"`
	// Voters is only present when the poll is not anonymous.
	Voters []uuid.UUID `json:"voters,omitempty" binding:"optional" format:"uuid"`
}

// LoadPoll returns the poll of a message, or nil when it has none.
func LoadPoll(ctx context.Context, q db.Querier, messageID uuid.UUID) (*Poll, error) {
	polls, err := loadPollsFor(ctx, q, []uuid.UUID{messageID})
	if err != nil {
		return nil, err
	}
	return polls[messageID], nil
}

// loadPolls fills msgs[].Poll for the messages that have one.
func loadPolls(ctx context.Context, p *db.Pool, msgs []Message, idx map[uuid.UUID]int, ids []uuid.UUID) error {
	polls, err := loadPollsFor(ctx, p, ids)
	if err != nil {
		return err
	}
	for id, poll := range polls {
		msgs[idx[id]].Poll = poll
	}
	return nil
}

func loadPollsFor(ctx context.Context, q db.Querier, ids []uuid.UUID) (map[uuid.UUID]*Poll, error) {
	out := map[uuid.UUID]*Poll{}
	rows, err := q.Query(ctx, `
		SELECT message_id, multiple, anonymous, closes_at, closes_at <= clock_timestamp(),
		       (SELECT COUNT(DISTINCT v.user_id) FROM poll_votes v JOIN poll_options o ON o.id = v.option_id WHERE o.poll_id = polls.message_id)
		FROM polls WHERE message_id = ANY($1)`, ids)
	if err != nil {
		return nil, fmt.Errorf("load polls: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		p := &Poll{Options: []PollOption{}}
		if err := rows.Scan(&id, &p.Multiple, &p.Anonymous, &p.ClosesAt, &p.Closed, &p.TotalVoters); err != nil {
			return nil, err
		}
		out[id] = p
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	if len(out) == 0 {
		return out, nil
	}
	return out, loadPollOptions(ctx, q, out)
}

// loadPollOptions adds the options with their tallies. The voters are chosen
// in SQL and only for polls that are not anonymous, so an anonymous poll's
// voter ids never leave the database.
func loadPollOptions(ctx context.Context, q db.Querier, polls map[uuid.UUID]*Poll) error {
	ids := make([]uuid.UUID, 0, len(polls))
	for id := range polls {
		ids = append(ids, id)
	}
	rows, err := q.Query(ctx, `
		SELECT o.poll_id, o.id, o.label, COUNT(v.user_id),
		       CASE WHEN p.anonymous THEN NULL
		            ELSE array_agg(v.user_id ORDER BY v.created_at, v.user_id) FILTER (WHERE v.user_id IS NOT NULL) END
		FROM poll_options o
		JOIN polls p ON p.message_id = o.poll_id
		LEFT JOIN poll_votes v ON v.option_id = o.id
		WHERE o.poll_id = ANY($1)
		GROUP BY o.poll_id, o.id, o.label, o.position, p.anonymous
		ORDER BY o.poll_id, o.position`, ids)
	if err != nil {
		return fmt.Errorf("load poll options: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var pollID uuid.UUID
		var o PollOption
		if err := rows.Scan(&pollID, &o.ID, &o.Label, &o.Votes, &o.Voters); err != nil {
			return err
		}
		polls[pollID].Options = append(polls[pollID].Options, o)
	}
	return rows.Err()
}
