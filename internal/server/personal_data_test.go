package server

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/auth/account"
	"github.com/msch128/mnema-talk/internal/auth/authn"
	"github.com/msch128/mnema-talk/internal/perm/permtest"
	"github.com/msch128/mnema-talk/internal/userdata"
)

// previousEraseSteps is the eraseSteps list from internal/auth/account_erase.go
// copied before those statements moved next to their domains. The direct-message
// channel statement is the two pieces joined, which is what ran.
var previousEraseSteps = []string{
	`DELETE FROM message_reactions WHERE user_id = $1`,
	`DELETE FROM message_mentions WHERE user_id = $1`,
	`DELETE FROM poll_votes WHERE user_id = $1`,
	`DELETE FROM poll_voters WHERE user_id = $1`,
	`DELETE FROM polls WHERE message_id IN (SELECT id FROM messages WHERE user_id = $1)`,
	`DELETE FROM message_edits WHERE editor_id = $1 OR message_id IN (SELECT id FROM messages WHERE user_id = $1)`,
	`UPDATE messages SET content = '', title = NULL, embeds = '[]'::jsonb, author_name = NULL, is_pinned = FALSE, pinned_at = NULL, pinned_by = NULL, is_edited = FALSE WHERE user_id = $1`,
	`DELETE FROM outgoing_queue WHERE convert_from(data, 'UTF8')::jsonb->>'user_id' = $1::text
		OR convert_from(data, 'UTF8')::jsonb->'reply_to'->>'user_id' = $1::text
		OR jsonb_path_exists(convert_from(data, 'UTF8')::jsonb, '$.reactions[*].users[*] ? (@ == $id)', jsonb_build_object('id', $1::text))`,
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
	`UPDATE event_series SET created_by = NULL WHERE created_by = $1`,
	`DELETE FROM event_comments WHERE user_id = $1`,
	`DELETE FROM event_comment_mutes WHERE user_id = $1`,
	`UPDATE events SET created_by = NULL WHERE created_by = $1`,
	`UPDATE wiki_revisions SET editor_id = NULL WHERE editor_id = $1`,
	`UPDATE wiki_pages SET created_by = NULL WHERE created_by = $1`,
	`UPDATE wiki_pages SET updated_by = NULL WHERE updated_by = $1`,
	`UPDATE wiki_pages SET deleted_by = NULL WHERE deleted_by = $1`,
	`UPDATE message_reports SET reporter_id = NULL, note = '' WHERE reporter_id = $1`,
	`UPDATE message_reports SET reported_user_id = NULL WHERE reported_user_id = $1`,
	`UPDATE message_reports SET resolved_by = NULL WHERE resolved_by = $1`,
	`DELETE FROM channels WHERE id IN (SELECT c.id FROM channels c WHERE c.type = 'dm'
	AND NOT EXISTS (SELECT 1 FROM dm_members m WHERE m.channel_id = c.id AND m.user_id <> $1))`,
	`DELETE FROM dm_members WHERE user_id = $1`,
	`UPDATE dm_members SET added_by = NULL WHERE added_by = $1`,
	`UPDATE channels SET dm_created_by = NULL WHERE dm_created_by = $1`,
}

var eraseStatementsBefore = []string{
	`DELETE FROM media WHERE uploader_id = $1
		AND NOT EXISTS (SELECT 1 FROM wiki_images w WHERE w.media_id = media.id
			AND EXISTS (SELECT 1 FROM wiki_revisions r WHERE strpos(r.content, '/api/media/' || media.id::text) > 0))
		RETURNING s3_key`,
	`DELETE FROM data_exports WHERE user_id = $1 AND s3_key IS NOT NULL RETURNING s3_key`,
	`DELETE FROM media WHERE message_id IN (SELECT id FROM messages WHERE channel_id IN (SELECT c.id FROM channels c WHERE c.type = 'dm'
	AND NOT EXISTS (SELECT 1 FROM dm_members m WHERE m.channel_id = c.id AND m.user_id <> $1))) RETURNING s3_key`,
	`DELETE FROM media WHERE message_id IN (SELECT id FROM messages WHERE channel_id IN (SELECT id FROM channels WHERE dm_id IN (SELECT c.id FROM channels c WHERE c.type = 'dm'
	AND NOT EXISTS (SELECT 1 FROM dm_members m WHERE m.channel_id = c.id AND m.user_id <> $1)))) RETURNING s3_key`,
	`DELETE FROM data_exports WHERE user_id = $1`,
}

var eraseStatementsAfter = []string{
	`UPDATE message_reports SET snapshot = jsonb_build_object(
	'message', CASE WHEN snapshot->'message'->>'user_id' = $1::text
	                THEN snapshot->'message' || jsonb_build_object('content', '', 'attachments', '[]'::jsonb, 'username', $2::text, 'display_name', 'Deleted member')
	                ELSE snapshot->'message' END,
	'context', COALESCE((SELECT jsonb_agg(CASE WHEN e->>'user_id' = $1::text
	                THEN e || jsonb_build_object('content', '', 'attachments', '[]'::jsonb, 'username', $2::text, 'display_name', 'Deleted member')
	                ELSE e END ORDER BY ord)
	                FROM jsonb_array_elements(snapshot->'context') WITH ORDINALITY AS t(e, ord)), '[]'::jsonb))
	WHERE snapshot::text LIKE '%' || $1::text || '%'`,
	`UPDATE channels SET leader_id = NULL WHERE leader_id = $1`,
	`UPDATE messages SET pinned_by = NULL WHERE pinned_by = $1`,
	`UPDATE users SET banned_by = NULL WHERE banned_by = $1`,
	`UPDATE users SET timeout_by = NULL WHERE timeout_by = $1`,
	`UPDATE custom_emoji SET created_by = NULL WHERE created_by = $1`,
	`UPDATE discord_imports SET created_by = NULL WHERE created_by = $1`,
	`UPDATE discord_imports SET undone_by = NULL WHERE undone_by = $1`,
	`DELETE FROM login_unlock_requests WHERE LOWER(username) = LOWER($1)`,
	`DELETE FROM message_nonces WHERE user_id = $1`,
	`DELETE FROM upload_usage WHERE user_id = $1`,
	`SELECT set_config('mnema.audit_scrub', 'on', true)`,
	`UPDATE audit_log SET
	actor_name  = CASE WHEN actor_id = $1 THEN '' ELSE actor_name END,
	target_name = CASE WHEN target_type = 'user' AND target_id = $1::text THEN '' ELSE target_name END,
	details     = '{}'::jsonb
	WHERE actor_id = $1 OR (target_type = 'user' AND target_id = $1::text) OR details::text LIKE '%' || $1::text || '%'`,
}

func TestAccountErasureKeepsTheOriginalStatementOrder(t *testing.T) {
	if userdata.OrderReactions != 0 || userdata.OrderScrubReports != len(previousEraseSteps) {
		t.Fatalf("order numbers shifted: reactions=%d scrub=%d steps=%d", userdata.OrderReactions, userdata.OrderScrubReports, len(previousEraseSteps))
	}
	steps := personalDataHandler().ErasePlan()
	seen := map[int]struct{}{}
	var got, original []string
	n := 0
	for _, st := range steps {
		if _, ok := seen[st.Order]; ok {
			t.Fatalf("duplicate erasure order %d", st.Order)
		}
		seen[st.Order] = struct{}{}
		got = append(got, st.SQL)
		if st.Order < 0 || st.Order >= userdata.OrderScrubReports {
			continue
		}
		if st.Order != n {
			t.Fatalf("statement %d has order %d", n, st.Order)
		}
		original = append(original, st.SQL)
		n++
	}
	equalSQL(t, original, previousEraseSteps)
	want := append(append(append([]string{}, eraseStatementsBefore...), previousEraseSteps...), eraseStatementsAfter...)
	equalSQL(t, got, want)
}

func TestArchiveFilesStayInTheOriginalOrder(t *testing.T) {
	names, err := personalDataHandler().ExportFileNames(context.Background(), uuid.Nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"profile.json",
		"settings.json",
		"edits.json",
		"reactions.json",
		"poll_votes.json",
		"uploads.json",
		"polls.json",
		"pins.json",
		"thread_follows.json",
		"push.json",
		"sessions.json",
		"emoji.json",
		"audit.json",
		"security.json",
		"direct_messages.json",
		"blocks.json",
		"reports.json",
		"saved.json",
		"reminders.json",
		"scheduled_messages.json",
		"rsvps.json",
		"event_signups.json",
		"event_comments.json",
		"wiki_revisions.json",
		"messages.json",
	}
	if len(names) != len(want) {
		t.Fatalf("got %d files %v", len(names), names)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("file %d got %s want %s (all %v)", i, names[i], want[i], names)
		}
	}
}

func personalDataHandler() *account.API {
	h := account.New(authn.NewHandler(&auth.Sessions{}, permtest.NewFixture().Service(), nil))
	(&components{account: h}).wirePersonalData()
	return h
}

func equalSQL(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d statements, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("statement %d\n got: %q\nwant: %q", i, got[i], want[i])
		}
	}
}
