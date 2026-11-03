package auth

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/userdata"
)

// deletedUsernamePrefix starts the placeholder name of a deleted account. No
// new account may take a name with it (see ValidateUsername).
const deletedUsernamePrefix = "deleted-"

// Account deletion keeps one row: the account itself, anonymised, so that its
// messages still have an author and threads stay readable. Everything else
// that refers to the person is removed in one transaction. Statements that
// belong to another domain live next to that domain and carry an explicit
// order; auth sorts them with its own before running any. A test walks every
// foreign key to users and fails when a table is forgotten.
//
// What happens to what the person wrote:
//   - their messages stay, authored by "Deleted member", with the text, link
//     cards and pins removed, as are the kept earlier versions of their
//     edits and forum post titles (other members' replies and quotes remain);
//   - their uploads, avatar and banner are deleted from storage and the database;
//   - their reactions, poll votes and mentions of them disappear, and so do
//     the polls they posted (the question was the message text);
//   - undelivered outgoing webhook events that carry their message text (or
//     quote it) are dropped from the queue;
//   - audit entries about them keep action, time and ids but lose names and
//     details (other people's text that merely mentions them cannot be found
//     and stays; the operator's privacy policy says so).

// scrubAudit blanks the names and details of every audit entry that is about
// the account or mentions its id. The audit trigger allows exactly this while
// mnema.audit_scrub is on.
const scrubAudit = `UPDATE audit_log SET
	actor_name  = CASE WHEN actor_id = $1 THEN '' ELSE actor_name END,
	target_name = CASE WHEN target_type = 'user' AND target_id = $1::text THEN '' ELSE target_name END,
	details     = '{}'::jsonb
	WHERE actor_id = $1 OR (target_type = 'user' AND target_id = $1::text) OR details::text LIKE '%' || $1::text || '%'`

const anonymise = `UPDATE users SET
	username = $2, display_name = 'Deleted member', bio = '', avatar_s3_key = NULL, banner_s3_key = NULL, status_text = '', status_expires_at = NULL,
	password_hash = '!deleted', email = NULL, email_verified_at = NULL, presence = 'online', locale = '',
	onboarding = '{}'::jsonb, rules_accepted_version = 0, last_seen_at = NULL, voice_seconds = 0,
	timeout_until = NULL, timeout_reason = '', timeout_by = NULL,
	banned_at = NULL, banned_until = NULL, ban_reason = '', banned_by = NULL, invite_id = NULL,
	disabled_at = NOW(), deleted_at = NOW(), token_version = token_version + 1, updated_at = NOW()
	WHERE id = $1`

// pinRef is a pinned message the erasure unpinned.
type pinRef struct{ channelID, messageID uuid.UUID }

// erased is what the caller finishes after the transaction committed: the
// storage keys of the person's files (uploads, avatar, finished exports) to
// delete from the object store, and the pins the erasure removed, whose
// channels need a pins_changed event.
type erased struct {
	keys   []string
	unpins []pinRef
}

// pinnedOf lists the pinned messages the account wrote. It runs before any
// statement clears those pins.
func pinnedOf(ctx context.Context, tx pgx.Tx, id uuid.UUID) ([]pinRef, error) {
	rows, err := tx.Query(ctx, `SELECT channel_id, id FROM messages WHERE user_id = $1 AND is_pinned ORDER BY pinned_at`, id)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (p pinRef, err error) {
		err = row.Scan(&p.channelID, &p.messageID)
		return p, err
	})
}

// eraseAccount runs the erasure for a test that already holds the transaction.
// It uses the fallback registry. deleteAccount uses the handler's registry.
func eraseAccount(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*erased, error) {
	return eraseWith(ctx, tx, id, personalFallback)
}

func (h *Handler) eraseOwnAccount(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*erased, error) {
	return eraseWith(ctx, tx, id, h.personalData())
}

func eraseWith(ctx context.Context, tx pgx.Tx, id uuid.UUID, reg *userdata.Registry) (*erased, error) {
	var name string
	err := tx.QueryRow(ctx, `SELECT username FROM users WHERE id = $1 AND NOT is_bot AND deleted_at IS NULL FOR UPDATE`, id).Scan(&name)
	if err != nil {
		return nil, err
	}
	unpins, err := pinnedOf(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	keys, err := runErase(ctx, tx, id, name, mergedEraseSteps(reg))
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, anonymise, id, deletedUsernamePrefix+shortID(id)); err != nil {
		return nil, fmt.Errorf("anonymise account: %w", err)
	}
	return &erased{keys: keys, unpins: unpins}, nil
}

func mergedEraseSteps(reg *userdata.Registry) []userdata.Step {
	steps := authEraseSteps()
	if reg != nil {
		steps = append(steps, reg.Steps()...)
	}
	userdata.SortSteps(steps)
	return steps
}

func runErase(ctx context.Context, tx pgx.Tx, id uuid.UUID, name string, steps []userdata.Step) ([]string, error) {
	var keys []string
	tomb := deletedUsernamePrefix + shortID(id)
	for _, st := range steps {
		got, err := execErase(ctx, tx, st, eraseArgs(st.Arg, id, name, tomb))
		if err != nil {
			return nil, err
		}
		keys = append(keys, got...)
	}
	return keys, nil
}

func execErase(ctx context.Context, tx pgx.Tx, st userdata.Step, args []any) ([]string, error) {
	if st.Keys {
		rows, err := tx.Query(ctx, st.SQL, args...)
		if err != nil {
			return nil, err
		}
		return pgx.CollectRows(rows, pgx.RowTo[string])
	}
	if _, err := tx.Exec(ctx, st.SQL, args...); err != nil {
		return nil, fmt.Errorf("erase %q: %w", st.SQL, err)
	}
	return nil, nil
}

func eraseArgs(kind userdata.Arg, id uuid.UUID, name, tomb string) []any {
	switch kind {
	case userdata.ArgUsername:
		return []any{name}
	case userdata.ArgIDAndName:
		return []any{id, tomb}
	case userdata.ArgNone:
		return nil
	default:
		return []any{id}
	}
}

func shortID(id uuid.UUID) string { return fmt.Sprintf("%x", id[:6]) }

// refuseDeleted makes a deleted account look like one that never existed to
// every administrative action: it cannot be enabled, given a password, banned,
// timed out or given roles again. The database enforces the same with a
// trigger, so no code path can revive a tombstone.
func refuseDeleted(ctx context.Context, q db.Querier, id uuid.UUID) error {
	var deleted bool
	err := q.QueryRow(ctx, `SELECT deleted_at IS NOT NULL FROM users WHERE id = $1`, id).Scan(&deleted)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && deleted) {
		return httpx.ErrNotFound("user not found")
	}
	return err
}
