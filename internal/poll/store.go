package poll

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/httpx"
)

var (
	errPollNotFound = httpx.ErrNotFound("poll not found")
	errPollEnded    = httpx.ErrConflict("the poll has ended")
	errAlreadyVoted = httpx.ErrConflict("you have already voted in this anonymous poll; a vote there cannot be changed")
)

// insertPoll stores the poll and its options for a message created in tx.
func insertPoll(ctx context.Context, tx pgx.Tx, messageID uuid.UUID, c clean) error {
	if _, err := tx.Exec(ctx, `
		INSERT INTO polls (message_id, multiple, anonymous, closes_at)
		VALUES ($1, $2, $3, clock_timestamp() + make_interval(secs => $4))`,
		messageID, c.Multiple, c.Anonymous, c.length.Seconds()); err != nil {
		return fmt.Errorf("insert poll: %w", err)
	}
	for i, label := range c.Options {
		if _, err := tx.Exec(ctx, `INSERT INTO poll_options (poll_id, position, label) VALUES ($1, $2, $3)`, messageID, i, label); err != nil {
			return fmt.Errorf("insert poll option: %w", err)
		}
	}
	return nil
}

// ref is where a poll lives and who made it.
type ref struct {
	message uuid.UUID
	channel uuid.UUID
	author  uuid.UUID
}

func loadRef(ctx context.Context, q db.Querier, messageID uuid.UUID) (ref, error) {
	r := ref{message: messageID}
	err := q.QueryRow(ctx, `
		SELECT m.channel_id, m.user_id FROM polls p JOIN messages m ON m.id = p.message_id WHERE p.message_id = $1`, messageID).Scan(&r.channel, &r.author)
	if errors.Is(err, pgx.ErrNoRows) {
		return r, errPollNotFound
	}
	if err != nil {
		return r, fmt.Errorf("load poll: %w", err)
	}
	return r, nil
}

// MyVotes lists the options the member voted for.
func MyVotes(ctx context.Context, q db.Querier, messageID, userID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := q.Query(ctx, `
		SELECT v.option_id FROM poll_votes v JOIN poll_options o ON o.id = v.option_id
		WHERE o.poll_id = $1 AND v.user_id = $2 ORDER BY o.position`, messageID, userID)
	if err != nil {
		return nil, fmt.Errorf("load votes: %w", err)
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return nil, fmt.Errorf("load votes: %w", err)
	}
	if ids == nil {
		ids = []uuid.UUID{}
	}
	return ids, nil
}

// Vote makes optionIDs the member's votes in the poll (none withdraws them).
// The poll row is share-locked, so ending the poll waits for votes in flight
// and a vote never lands after the end; a per-member advisory lock serializes
// one member's concurrent requests, so two of them cannot leave a single-choice
// poll with two votes.
func Vote(ctx context.Context, p *db.Pool, messageID, userID uuid.UUID, optionIDs []uuid.UUID) error {
	return pgx.BeginFunc(ctx, p, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1::text || $2::text, 0))`, messageID, userID); err != nil {
			return fmt.Errorf("lock votes: %w", err)
		}
		var multiple, ended, anonymous bool
		err := tx.QueryRow(ctx, `SELECT multiple, closes_at <= clock_timestamp(), anonymous FROM polls WHERE message_id = $1 FOR SHARE`, messageID).Scan(&multiple, &ended, &anonymous)
		if errors.Is(err, pgx.ErrNoRows) {
			return errPollNotFound
		}
		if err != nil {
			return fmt.Errorf("load poll: %w", err)
		}
		if ended {
			return errPollEnded
		}
		if err := checkChoice(ctx, tx, messageID, multiple, optionIDs); err != nil {
			return err
		}
		if anonymous {
			return castAnonymousVote(ctx, tx, messageID, userID, optionIDs)
		}
		return replaceVotes(ctx, tx, messageID, userID, optionIDs)
	})
}

// castAnonymousVote counts a vote in an anonymous poll without recording what
// the member chose: poll_voters remembers that they voted (so they cannot vote
// twice), the options only get their counters raised. The statements share no
// key, and a vote cannot be changed or withdrawn, because that would need the
// choice to be known.
func castAnonymousVote(ctx context.Context, tx pgx.Tx, messageID, userID uuid.UUID, optionIDs []uuid.UUID) error {
	if len(optionIDs) == 0 {
		return httpx.ErrInvalidInput("a vote in an anonymous poll cannot be withdrawn")
	}
	tag, err := tx.Exec(ctx, `INSERT INTO poll_voters (poll_id, user_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, messageID, userID)
	if err != nil {
		return fmt.Errorf("record voter: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return errAlreadyVoted
	}
	if _, err := tx.Exec(ctx, `UPDATE poll_options SET anon_votes = anon_votes + 1 WHERE poll_id = $1 AND id = ANY($2)`, messageID, optionIDs); err != nil {
		return fmt.Errorf("count vote: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE polls SET anon_voters = anon_voters + 1 WHERE message_id = $1`, messageID); err != nil {
		return fmt.Errorf("count voter: %w", err)
	}
	return nil
}

// HasVoted tells whether the member voted in the poll. It is all an anonymous
// poll knows about a member's participation.
func HasVoted(ctx context.Context, q db.Querier, messageID, userID uuid.UUID) (bool, error) {
	var voted bool
	err := q.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM poll_voters WHERE poll_id = $1 AND user_id = $2)
		    OR EXISTS (SELECT 1 FROM poll_votes v JOIN poll_options o ON o.id = v.option_id WHERE o.poll_id = $1 AND v.user_id = $2)`,
		messageID, userID).Scan(&voted)
	if err != nil {
		return false, fmt.Errorf("load voted: %w", err)
	}
	return voted, nil
}

// checkChoice requires distinct options of this poll, one for a single-choice poll.
func checkChoice(ctx context.Context, tx pgx.Tx, messageID uuid.UUID, multiple bool, optionIDs []uuid.UUID) error {
	distinct := map[uuid.UUID]bool{}
	for _, id := range optionIDs {
		distinct[id] = true
	}
	if len(distinct) != len(optionIDs) {
		return httpx.ErrInvalidInput("an option is given twice")
	}
	if !multiple && len(optionIDs) > 1 {
		return httpx.ErrInvalidInput("this poll takes one answer")
	}
	var found int
	if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM poll_options WHERE poll_id = $1 AND id = ANY($2)`, messageID, optionIDs).Scan(&found); err != nil {
		return fmt.Errorf("check options: %w", err)
	}
	if found != len(optionIDs) {
		return httpx.ErrInvalidInput("unknown option")
	}
	return nil
}

func replaceVotes(ctx context.Context, tx pgx.Tx, messageID, userID uuid.UUID, optionIDs []uuid.UUID) error {
	if _, err := tx.Exec(ctx, `
		DELETE FROM poll_votes WHERE user_id = $2 AND option_id IN (SELECT id FROM poll_options WHERE poll_id = $1)`, messageID, userID); err != nil {
		return fmt.Errorf("clear votes: %w", err)
	}
	if len(optionIDs) == 0 {
		return nil
	}
	if _, err := tx.Exec(ctx, `INSERT INTO poll_votes (option_id, user_id) SELECT unnest($1::uuid[]), $2`, optionIDs, userID); err != nil {
		return fmt.Errorf("store votes: %w", err)
	}
	return nil
}

// end closes the poll now. It reports false when it had ended already.
func end(ctx context.Context, q db.Querier, messageID uuid.UUID) (bool, error) {
	tag, err := q.Exec(ctx, `UPDATE polls SET closes_at = clock_timestamp() WHERE message_id = $1 AND closes_at > clock_timestamp()`, messageID)
	if err != nil {
		return false, fmt.Errorf("end poll: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}
