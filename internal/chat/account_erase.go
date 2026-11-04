package chat

import "github.com/msch128/mnema-talk/internal/userdata"

// soleDMs selects the direct message conversations in which nobody but $1 is.
const soleDMs = `SELECT c.id FROM channels c WHERE c.type = 'dm'
	AND NOT EXISTS (SELECT 1 FROM dm_members m WHERE m.channel_id = c.id AND m.user_id <> $1)`

const deleteSoleDMChannels = `DELETE FROM channels WHERE id IN (` + soleDMs + `)`

const deleteSoleDMMedia = `DELETE FROM media WHERE message_id IN (SELECT id FROM messages WHERE channel_id IN (` + soleDMs + `)) RETURNING s3_key`

const deleteSoleDMTalkMedia = `DELETE FROM media WHERE message_id IN (SELECT id FROM messages WHERE channel_id IN (SELECT id FROM channels WHERE dm_id IN (` + soleDMs + `))) RETURNING s3_key`

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

func chatEraseSteps() []userdata.Step {
	return []userdata.Step{
		{Order: userdata.OrderMediaOwn, SQL: `DELETE FROM media WHERE uploader_id = $1 RETURNING s3_key`, Keys: true},
		{Order: userdata.OrderMediaDM, SQL: deleteSoleDMMedia, Keys: true},
		{Order: userdata.OrderMediaTalk, SQL: deleteSoleDMTalkMedia, Keys: true},
		{Order: userdata.OrderReactions, SQL: `DELETE FROM message_reactions WHERE user_id = $1`},
		{Order: userdata.OrderMentions, SQL: `DELETE FROM message_mentions WHERE user_id = $1`},
		{Order: userdata.OrderPollVotes, SQL: `DELETE FROM poll_votes WHERE user_id = $1`},
		{Order: userdata.OrderPollVoters, SQL: `DELETE FROM poll_voters WHERE user_id = $1`},
		{Order: userdata.OrderPolls, SQL: `DELETE FROM polls WHERE message_id IN (SELECT id FROM messages WHERE user_id = $1)`},
		{Order: userdata.OrderEdits, SQL: `DELETE FROM message_edits WHERE editor_id = $1 OR message_id IN (SELECT id FROM messages WHERE user_id = $1)`},
		{Order: userdata.OrderMessages, SQL: `UPDATE messages SET content = '', title = NULL, embeds = '[]'::jsonb, author_name = NULL, is_pinned = FALSE, pinned_at = NULL, pinned_by = NULL, is_edited = FALSE WHERE user_id = $1`},
		{Order: userdata.OrderThreadFollows, SQL: `DELETE FROM thread_follows WHERE user_id = $1`},
		{Order: userdata.OrderForumReads, SQL: `DELETE FROM forum_post_reads WHERE user_id = $1`},
		{Order: userdata.OrderSlowMode, SQL: `DELETE FROM slow_mode_posts WHERE user_id = $1`},
		{Order: userdata.OrderBlocks, SQL: `DELETE FROM user_blocks WHERE blocker_id = $1 OR blocked_id = $1`},
		{Order: userdata.OrderSaved, SQL: `DELETE FROM saved_messages WHERE user_id = $1`},
		{Order: userdata.OrderReportReporter, SQL: `UPDATE message_reports SET reporter_id = NULL, note = '' WHERE reporter_id = $1`},
		{Order: userdata.OrderReportReported, SQL: `UPDATE message_reports SET reported_user_id = NULL WHERE reported_user_id = $1`},
		{Order: userdata.OrderReportResolved, SQL: `UPDATE message_reports SET resolved_by = NULL WHERE resolved_by = $1`},
		{Order: userdata.OrderDMChannels, SQL: deleteSoleDMChannels},
		{Order: userdata.OrderDMMembers, SQL: `DELETE FROM dm_members WHERE user_id = $1`},
		{Order: userdata.OrderDMAddedBy, SQL: `UPDATE dm_members SET added_by = NULL WHERE added_by = $1`},
		{Order: userdata.OrderDMCreatedBy, SQL: `UPDATE channels SET dm_created_by = NULL WHERE dm_created_by = $1`},
		{Order: userdata.OrderTalkLeader, SQL: `UPDATE channels SET leader_id = NULL WHERE leader_id = $1`},
		{Order: userdata.OrderPinnedBy, SQL: `UPDATE messages SET pinned_by = NULL WHERE pinned_by = $1`},
		{Order: userdata.OrderScrubReports, SQL: scrubReportCopies, Arg: userdata.ArgIDAndName},
	}
}
