//go:build integration

package server

// The decisions behind the schema oracle in personal_schema_integration_test.go.
// An entry says why a column stays after an erasure or why a table or column is
// not in the export. A gap entry names the hunt finding that records it.

// erasureKeeps are the columns about a person that the erasure plan leaves.
var erasureKeeps = map[string]string{
	"users.username":              "the anonymise statement after the plan writes the tombstone name (SECURITY.md, account deletion)",
	"users.email":                 "the anonymise statement after the plan clears it (SECURITY.md, account deletion)",
	"bots.user_id":                "a bot's own account, never a person: the erasure refuses bot accounts",
	"roles.bot_user_id":           "the managed role of a bot account, never a person",
	"webhooks.bot_user_id":        "the account a webhook posts as, never a person",
	"moderation_notes.subject_id": "notes stay attached to the tombstone until a moderator deletes them (SECURITY.md:179, doc/moderation.md:469)",
	"moderation_notes.author_id":  "a note by an erased moderator shows Deleted member as its author (doc/moderation.md:472)",
}

// erasedWithTheRow are columns about a person that no erasure statement names
// itself: they go with their row, which a DELETE reaches through the listed
// NOT NULL column naming the same person (the oracle checks both).
var erasedWithTheRow = map[string]string{
	"account_tokens.email": "user_id", // the address a token was sent to, the account's own (migration 0040)
}

// erasureGaps are columns the erasure should clear and does not.
var erasureGaps = map[string]string{}

// exportOmits are tables with a column about a person that the export does
// not read.
var exportOmits = map[string]string{
	"account_tokens":         "one-time e-mail and password tokens: secrets that expire within hours",
	"bot_tokens":             "token hashes of bots the member minted for; bots are an administrator tool",
	"bots":                   "bots are an administrator tool; the member is only their creator",
	"webhooks":               "an administrator tool; the member is only its creator, the row holds a token hash",
	"outgoing_webhooks":      "an administrator tool; the member is only its creator, the row holds the signing secret",
	"discord_imports":        "an administrator tool; the member is only who ran the import",
	"invites":                "invite codes are credentials that admit other people",
	"data_exports":           "the bookkeeping of the export itself",
	"login_challenges":       "a sign-in in progress that expires within minutes",
	"login_unlock_requests":  "the operator's queue of unlock requests, keyed by name",
	"second_factor_failures": "security counters about failed codes, pruned after 24 hours",
	"permission_overwrites":  "channel access moderators set; not something the member provided",
	"moderation_notes":       "moderator data, never in the member's export (SECURITY.md:177)",
	"message_mentions":       "other members' messages that mention the member",
	"forum_post_reads":       "read positions the client derives, like the channel read markers",
	"slow_mode_posts":        "the time of the last post for the slow mode limit, overwritten on every post",
	"upload_usage":           "byte bookings of the daily upload budget; uploads.json lists the files",
	"message_nonces":         "retry ids of sent messages, pruned after a day; messages.json holds the messages",
	"events":                 "community content made with Manage events; the event stays at erasure with its creator set to empty (doc/events.md), and the export lists RSVPs, sign-ups and comments only",
	"event_series":           "community content like its events; a series holds nothing personal besides its creator, set to empty at erasure (doc/events.md)",
}

// exportGaps are tables the export should read and does not.
var exportGaps = map[string]string{}

// profileOmits are columns of users that profile.json and the other files
// leave out.
var profileOmits = map[string]string{
	"password_hash":       "a secret",
	"token_version":       "a session counter, no information about the member",
	"updated_at":          "bookkeeping of the row",
	"is_bot":              "always false for an account that can export",
	"disabled_at":         "a disabled account cannot sign in to export",
	"deleted_at":          "an erased account cannot export",
	"banned_at":           "a banned account cannot sign in to export",
	"banned_until":        "a banned account cannot sign in to export",
	"ban_reason":          "a banned account cannot sign in to export",
	"banned_by":           "the moderator, another person",
	"timeout_until":       "a moderator's decision; audit.json lists the action",
	"timeout_reason":      "a moderator's decision; audit.json lists the action",
	"timeout_by":          "the moderator, another person",
	"invite_id":           "the invite, created by another person",
	"avatar_reencoded_at": "bookkeeping of a one-time re-encoding of the avatar",
	"dm_list_collapsed":   "whether a list is folded in the sidebar: view state",
}

// profileGaps are columns of users the export should hold and does not.
var profileGaps = map[string]string{}
