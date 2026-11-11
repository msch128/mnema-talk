package account

import "github.com/msch128/mnema-talk/internal/userdata"

// authEraseSteps are the erasure statements that stay with the account:
// profile exports, invites, sessions, password, two-factor, audit and the
// queues auth owns. Other domains add theirs with the same order numbers.
//
// A queued webhook event goes when it is the member's (user_id), quotes the
// member (reply_to) or lists the member among those who reacted
// (reactions[].users, in a reaction update and in a message that carries its
// reactions): the reactions themselves are deleted, so a delivery after the
// erasure must not tell of them.
func authEraseSteps() []userdata.Step {
	return []userdata.Step{
		{Order: userdata.OrderExportKeys, SQL: `DELETE FROM data_exports WHERE user_id = $1 AND s3_key IS NOT NULL RETURNING s3_key`, Keys: true},
		{Order: userdata.OrderExportRows, SQL: `DELETE FROM data_exports WHERE user_id = $1`},
		{Order: userdata.OrderOutgoingQueue, SQL: "DELETE FROM outgoing_queue WHERE convert_from(data, 'UTF8')::jsonb->>'user_id' = $1::text\n\t\tOR convert_from(data, 'UTF8')::jsonb->'reply_to'->>'user_id' = $1::text\n\t\tOR jsonb_path_exists(convert_from(data, 'UTF8')::jsonb, '$.reactions[*].users[*] ? (@ == $id)', jsonb_build_object('id', $1::text))"},
		{Order: userdata.OrderBannedBy, SQL: `UPDATE users SET banned_by = NULL WHERE banned_by = $1`},
		{Order: userdata.OrderTimeoutBy, SQL: `UPDATE users SET timeout_by = NULL WHERE timeout_by = $1`},
		{Order: userdata.OrderInvites, SQL: `DELETE FROM invites WHERE created_by = $1`},
		{Order: userdata.OrderChannelReads, SQL: `DELETE FROM channel_reads WHERE user_id = $1`},
		{Order: userdata.OrderCategoryNotify, SQL: `DELETE FROM category_notify WHERE user_id = $1`},
		{Order: userdata.OrderServerNotify, SQL: `DELETE FROM server_notify WHERE user_id = $1`},
		{Order: userdata.OrderUserRoles, SQL: `DELETE FROM user_roles WHERE user_id = $1`},
		{Order: userdata.OrderOverwrites, SQL: `DELETE FROM permission_overwrites WHERE user_id = $1`},
		{Order: userdata.OrderSessions, SQL: `DELETE FROM user_sessions WHERE user_id = $1`},
		{Order: userdata.OrderAccountTokens, SQL: `DELETE FROM account_tokens WHERE user_id = $1`},
		{Order: userdata.OrderTOTP, SQL: `DELETE FROM user_totp WHERE user_id = $1`},
		{Order: userdata.OrderRecoveryCodes, SQL: `DELETE FROM recovery_codes WHERE user_id = $1`},
		{Order: userdata.OrderLoginChallenges, SQL: `DELETE FROM login_challenges WHERE user_id = $1`},
		{Order: userdata.OrderSecondFactor, SQL: `DELETE FROM second_factor_failures WHERE user_id = $1`},
		{Order: userdata.OrderLoginUnlock, SQL: `DELETE FROM login_unlock_requests WHERE LOWER(username) = LOWER($1)`, Arg: userdata.ArgUsername},
		{Order: userdata.OrderAuditFlag, SQL: `SELECT set_config('mnema.audit_scrub', 'on', true)`, Arg: userdata.ArgNone},
		{Order: userdata.OrderScrubAudit, SQL: scrubAudit},
	}
}
