package messages

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/mnema-xyz/talk/internal/chat/core"
	"github.com/mnema-xyz/talk/internal/db"
)

// LoadPoll returns the poll of a message, or nil when it has none.
func LoadPoll(ctx context.Context, q db.Querier, messageID uuid.UUID) (*core.Poll, error) {
	polls, err := loadPollsFor(ctx, q, []uuid.UUID{messageID})
	if err != nil {
		return nil, err
	}
	return polls[messageID], nil
}

// queuePolls queues the queries that fill msgs[].Poll for the messages that
// have one: the polls, then their options.
func queuePolls(_ context.Context, b *pgx.Batch, msgs []core.Message, idx map[uuid.UUID]int, ids []uuid.UUID) {
	found := map[uuid.UUID]*core.Poll{}
	b.Queue(pollsSQL, ids).Query(labeled("load polls", func(rows pgx.Rows) error {
		if err := scanPolls(rows, found); err != nil {
			return err
		}
		for id, poll := range found {
			msgs[idx[id]].Poll = poll
		}
		return nil
	}))
	b.Queue(pollOptionsSQL, ids).Query(labeled("load poll options", func(rows pgx.Rows) error { return scanPollOptions(rows, found) }))
}

const pollsSQL = `
		SELECT message_id, multiple, anonymous, closes_at, closes_at <= clock_timestamp(),
		       CASE WHEN anonymous THEN anon_voters
			            ELSE (SELECT COUNT(DISTINCT v.user_id) FROM poll_votes v JOIN poll_options o ON o.id = v.option_id WHERE o.poll_id = polls.message_id) END
		FROM polls WHERE message_id = ANY($1)`

func loadPollsFor(ctx context.Context, q db.Querier, ids []uuid.UUID) (map[uuid.UUID]*core.Poll, error) {
	out := map[uuid.UUID]*core.Poll{}
	rows, err := q.Query(ctx, pollsSQL, ids)
	if err != nil {
		return nil, fmt.Errorf("load polls: %w", err)
	}
	defer rows.Close()
	if err := scanPolls(rows, out); err != nil {
		return nil, err
	}
	rows.Close()
	if len(out) == 0 {
		return out, nil
	}
	return out, loadPollOptions(ctx, q, out)
}

func scanPolls(rows pgx.Rows, out map[uuid.UUID]*core.Poll) error {
	for rows.Next() {
		var id uuid.UUID
		p := &core.Poll{Options: []core.PollOption{}}
		if err := rows.Scan(&id, &p.Multiple, &p.Anonymous, &p.ClosesAt, &p.Closed, &p.TotalVoters); err != nil {
			return err
		}
		out[id] = p
	}
	return rows.Err()
}

// loadPollOptions adds the options with their tallies. The voters are chosen
// in SQL and only for polls that are not anonymous, so an anonymous poll's
// voter ids never leave the database. A running anonymous poll shows no
// per-option tally either (only TotalVoters): a count that moves by one at the
// moment someone is seen voting would give away their choice in a small
// audience, so the tally appears when the poll ends, here and in the
// poll_update that carries it.
func loadPollOptions(ctx context.Context, q db.Querier, polls map[uuid.UUID]*core.Poll) error {
	ids := make([]uuid.UUID, 0, len(polls))
	for id := range polls {
		ids = append(ids, id)
	}
	rows, err := q.Query(ctx, pollOptionsSQL, ids)
	if err != nil {
		return fmt.Errorf("load poll options: %w", err)
	}
	defer rows.Close()
	return scanPollOptions(rows, polls)
}

const pollOptionsSQL = `
		SELECT o.poll_id, o.id, o.label, CASE WHEN p.anonymous AND p.closes_at > clock_timestamp() THEN 0
		            WHEN p.anonymous THEN o.anon_votes + (SELECT COUNT(*)::int FROM poll_anon_ballots b WHERE b.option_id = o.id)
		            ELSE COUNT(v.user_id)::int END,
		       CASE WHEN p.anonymous THEN NULL
		            ELSE array_agg(v.user_id ORDER BY v.created_at, v.user_id) FILTER (WHERE v.user_id IS NOT NULL) END
		FROM poll_options o
		JOIN polls p ON p.message_id = o.poll_id
		LEFT JOIN poll_votes v ON v.option_id = o.id
		WHERE o.poll_id = ANY($1)
		GROUP BY o.poll_id, o.id, o.label, o.position, o.anon_votes, p.anonymous, p.closes_at
		ORDER BY o.poll_id, o.position`

// scanPollOptions adds the options to their polls. An option of a poll that
// is not in polls (stored after the polls were read) is left out.
func scanPollOptions(rows pgx.Rows, polls map[uuid.UUID]*core.Poll) error {
	for rows.Next() {
		var pollID uuid.UUID
		var o core.PollOption
		if err := rows.Scan(&pollID, &o.ID, &o.Label, &o.Votes, &o.Voters); err != nil {
			return err
		}
		if poll, ok := polls[pollID]; ok {
			poll.Options = append(poll.Options, o)
		}
	}
	return rows.Err()
}
