package auth

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// deletedUsernamePrefix starts the placeholder name of a deleted account. No
// new account may take a name with it (see ValidateUsername).
const deletedUsernamePrefix = "deleted-"

// Account deletion keeps one row: the account itself, anonymised, so that its
// messages still have an author and threads stay readable. Everything else
// that refers to the person is removed in one transaction. The step list is
// the single place to extend when a new table refers to users; a test walks
// every foreign key to users and fails when a table is forgotten here.
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
var eraseSteps = []string{
	`DELETE FROM message_reactions WHERE user_id = $1`,
	`DELETE FROM message_mentions WHERE user_id = $1`,
	`DELETE FROM poll_votes WHERE user_id = $1`,
	`DELETE FROM poll_voters WHERE user_id = $1`,
	`DELETE FROM polls WHERE message_id IN (SELECT id FROM messages WHERE user_id = $1)`,
	`DELETE FROM message_edits WHERE editor_id = $1 OR message_id IN (SELECT id FROM messages WHERE user_id = $1)`,
	`UPDATE messages SET content = '', title = NULL, embeds = '[]'::jsonb, author_name = NULL, is_pinned = FALSE, pinned_at = NULL, pinned_by = NULL, is_edited = FALSE WHERE user_id = $1`,
	`DELETE FROM outgoing_queue WHERE convert_from(data, 'UTF8')::jsonb->>'user_id' = $1::text
		OR convert_from(data, 'UTF8')::jsonb->'reply_to'->>'user_id' = $1::text`,
	`UPDATE discord_imports SET created_by_name = '' WHERE created_by = $1`,
	`DELETE FROM invites WHERE created_by = $1`,
	`DELETE FROM channel_reads WHERE user_id = $1`,
	`DELETE FROM category_notify WHERE user_id = $1`,
	`DELETE FROM server_notify WHERE user_id = $1`,
	`DELETE FROM thread_follows WHERE user_id = $1`,
	`DELETE FROM forum_post_reads WHERE user_id = $1`,
	`DELETE FROM slow_mode_posts WHERE user_id = $1`,
	`DELETE FROM user_roles WHERE user_id = $1`,
	`DELETE FROM permission_overwrites WHERE user_id = $1`,
	`DELETE FROM push_subscriptions WHERE user_id = $1`,
	`DELETE FROM user_sessions WHERE user_id = $1`,
	`DELETE FROM bot_tokens WHERE created_by = $1`,
	`UPDATE bots SET created_by = NULL WHERE created_by = $1`,
	`UPDATE webhooks SET created_by = NULL WHERE created_by = $1`,
	`UPDATE outgoing_webhooks SET created_by = NULL WHERE created_by = $1`,
	`UPDATE outgoing_webhooks SET url_set_by = NULL WHERE url_set_by = $1`,
	`DELETE FROM account_tokens WHERE user_id = $1`,
	`DELETE FROM user_totp WHERE user_id = $1`,
	`DELETE FROM recovery_codes WHERE user_id = $1`,
	`DELETE FROM login_challenges WHERE user_id = $1`,
	`DELETE FROM second_factor_failures WHERE user_id = $1`,
	`DELETE FROM user_blocks WHERE blocker_id = $1 OR blocked_id = $1`,
	`DELETE FROM saved_messages WHERE user_id = $1`,
	`DELETE FROM message_reminders WHERE user_id = $1`,
	`DELETE FROM scheduled_messages WHERE user_id = $1`,
	`DELETE FROM event_rsvps WHERE user_id = $1`,
	`DELETE FROM event_signups WHERE user_id = $1`,
	`DELETE FROM event_comments WHERE user_id = $1`,
	`DELETE FROM event_comment_mutes WHERE user_id = $1`,
	`UPDATE events SET created_by = NULL WHERE created_by = $1`,
	// Reports keep their copies of other people's messages, so the person's
	// own text is blanked in every copy (as reported message or as context),
	// and the links to their account are cut. The reports stay for the
	// moderators.
	`UPDATE message_reports SET reporter_id = NULL, note = '' WHERE reporter_id = $1`,
	`UPDATE message_reports SET reported_user_id = NULL WHERE reported_user_id = $1`,
	`UPDATE message_reports SET resolved_by = NULL WHERE resolved_by = $1`,
	// Direct messages: a conversation nobody else is in goes with its
	// messages; the others keep theirs (the erased member's words in it are
	// emptied like everywhere), without the erased member.
	`DELETE FROM channels WHERE id IN (` + soleDMs + `)`,
	`DELETE FROM dm_members WHERE user_id = $1`,
	`UPDATE dm_members SET added_by = NULL WHERE added_by = $1`,
	`UPDATE channels SET dm_created_by = NULL WHERE dm_created_by = $1`,
}

// soleDMs selects the direct message conversations in which nobody but $1 is.
const soleDMs = `SELECT c.id FROM channels c WHERE c.type = 'dm'
	AND NOT EXISTS (SELECT 1 FROM dm_members m WHERE m.channel_id = c.id AND m.user_id <> $1)`

// scrubReportCopies blanks the text, file names and author names of the
// account's messages inside the stored copies of message_reports ($1 the
// account, $2 its tombstone username).
const scrubReportCopies = `UPDATE message_reports SET snapshot = jsonb_build_object(
	'message', CASE WHEN snapshot->'message'->>'user_id' = $1::text
	                THEN snapshot->'message' || jsonb_build_object('content', '', 'attachments', '[]'::jsonb, 'username', $2::text, 'display_name', 'Deleted member')
	                ELSE snapshot->'message' END,
	'context', COALESCE((SELECT jsonb_agg(CASE WHEN e->>'user_id' = $1::text
	                THEN e || jsonb_build_object('content', '', 'attachments', '[]'::jsonb, 'username', $2::text, 'display_name', 'Deleted member')
	                ELSE e END ORDER BY ord)
	                FROM jsonb_array_elements(snapshot->'context') WITH ORDINALITY AS t(e, ord)), '[]'::jsonb))
	WHERE snapshot::text LIKE '%' || $1::text || '%'`

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

// eraseStep is one statement of the erasure and the arguments it takes.
type eraseStep struct {
	sql  string
	args func(id uuid.UUID, name string) []any
}

func byID(id uuid.UUID, _ string) []any     { return []any{id} }
func byName(_ uuid.UUID, name string) []any { return []any{name} }
func noArgs(uuid.UUID, string) []any        { return nil }

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

// pinnedOf lists the pinned messages the account wrote.
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

// eraseAccount removes the person from the database.
func eraseAccount(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*erased, error) {
	var name string
	if err := tx.QueryRow(ctx, `SELECT username FROM users WHERE id = $1 AND NOT is_bot AND deleted_at IS NULL FOR UPDATE`, id).Scan(&name); err != nil {
		return nil, err
	}
	unpins, err := pinnedOf(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	var keys []string
	for _, sql := range []string{
		`DELETE FROM media WHERE uploader_id = $1 RETURNING s3_key`,
		`DELETE FROM data_exports WHERE user_id = $1 AND s3_key IS NOT NULL RETURNING s3_key`,
		// The files of direct messages nobody else is in any more go with them.
		`DELETE FROM media WHERE message_id IN (SELECT id FROM messages WHERE channel_id IN (` + soleDMs + `)) RETURNING s3_key`,
		// So do those of the Talks that belong to such a conversation.
		`DELETE FROM media WHERE message_id IN (SELECT id FROM messages WHERE channel_id IN (SELECT id FROM channels WHERE dm_id IN (` + soleDMs + `))) RETURNING s3_key`,
	} {
		rows, err := tx.Query(ctx, sql, id)
		if err != nil {
			return nil, err
		}
		got, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			return nil, err
		}
		keys = append(keys, got...)
	}
	steps := []eraseStep{{`DELETE FROM data_exports WHERE user_id = $1`, byID}}
	for _, sql := range eraseSteps {
		steps = append(steps, eraseStep{sql, byID})
	}
	steps = append(steps,
		eraseStep{scrubReportCopies, func(id uuid.UUID, _ string) []any { return []any{id, deletedUsernamePrefix + shortID(id)} }},
		eraseStep{`DELETE FROM login_unlock_requests WHERE LOWER(username) = LOWER($1)`, byName},
		eraseStep{`SELECT set_config('mnema.audit_scrub', 'on', true)`, noArgs},
		eraseStep{scrubAudit, byID},
	)
	for _, st := range steps {
		if _, err := tx.Exec(ctx, st.sql, st.args(id, name)...); err != nil {
			return nil, fmt.Errorf("erase %q: %w", st.sql, err)
		}
	}
	if _, err := tx.Exec(ctx, anonymise, id, deletedUsernamePrefix+shortID(id)); err != nil {
		return nil, fmt.Errorf("anonymise account: %w", err)
	}
	return &erased{keys: keys, unpins: unpins}, nil
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
