package audit

import "slices"

// Visibility says who may read an entry.
type Visibility int

const (
	// VisAuditors: everyone who holds View audit log.
	VisAuditors Visibility = iota
	// VisChannel: holders of View audit log who can also view the entry's
	// channel at the time they read. The Owner and Administrators see all.
	VisChannel
	// VisPrivileged: the Owner and Administrators only.
	VisPrivileged
)

// Class assigns a visibility to every action matching Pattern (SQL LIKE,
// where % matches the rest of the name).
type Class struct {
	Pattern string
	Vis     Visibility
	Why     string
}

// Classes is the single table that decides who reads what: an action gets the
// class of the first row it matches. Every action the server writes has a row
// of its own (TestEveryWrittenActionHasAClassOfItsOwn finds them in the
// source); the last row, the catch-all, is the narrowest class, so an action
// that was forgotten here is hidden from everyone but the Owner and
// Administrators instead of shown to every auditor. Rows of the auditors class
// name each action on its own: an action is only wide on purpose. One kind of
// stored row is classed by the listing before this table: user.talk_kick rows
// written by older versions for a removal from one Talk (filter.go,
// legacyTalkRemoval), which today is voice.disconnect.
var Classes = slices.Concat(
	[]Class{
		{"webhook.%", VisPrivileged, "webhook addresses and tokens are credentials"},
		{"outgoing_webhook.%", VisPrivileged, "webhook addresses and secrets are credentials"},
		{"bot.%", VisPrivileged, "bots hold tokens"},
		{"system.%", VisPrivileged, "updates and server operation"},
		{"import.%", VisPrivileged, "names and permissions of possibly private channels and roles"},
		{"settings.%", VisPrivileged, "instance settings"},
		{"media.%", VisPrivileged, "storage administration"},
		{"admin.%", VisPrivileged, "unlisted admin routes are not known to be harmless"},
		{"user.password_set", VisPrivileged, "Owner-only account takeover"},
		{"user.password_reset", VisPrivileged, "Owner-only account takeover"},
		{"user.two_factor_reset", VisPrivileged, "Owner-only account recovery action"},
		{"user.lockout_clear", VisPrivileged, "Owner-only, reveals account attacks"},
		{"auth.%", VisPrivileged, "sign-ins, failures and lockouts are other members' security events"},
		{"report.%", VisPrivileged, "reports may concern a private channel or a direct message"},
		{"moderation.note_%", VisPrivileged, "moderator notes are private to the moderators"},
		{"message.%", VisChannel, "holds message text of one channel"},
		{"moderation.bulk_delete", VisChannel, "names a channel and a reason that may concern a private channel"},
		{"poll.%", VisChannel, "names a channel and quotes the question of one of its polls"},
		{"voice.%", VisChannel, "who was in which Talk"},
		{"forum.%", VisChannel, "post titles, their authors and tag names of a possibly private forum"},
		{"category.%", VisPrivileged, "names and overwrites of a possibly private category"},
		{"layout.apply", VisPrivileged, "lists channel and category placements, private ones included"},
		{"event.talk_%", VisChannel, "names and times an event held in a possibly private Talk"},
		{"channel.%", VisChannel, "names, topics and overwrites of a possibly private channel"},
	},
	auditorRows("invites are community administration",
		"invite.create", "invite.vanity_link", "invite.update", "invite.revoke"),
	auditorRows("moderation of a member, which every auditor reviews",
		"user.disable", "user.enable", "user.sessions_revoke", "user.status", "user.roles_set",
		"user.talk_kick", "moderation.timeout", "moderation.timeout_remove", "moderation.ban",
		"moderation.unban", "moderation.ban_expired", "moderation.kick"),
	auditorRows("roles are community administration; a member choosing one is no private content",
		"role.create", "role.update", "role.delete", "role.reorder", "role.self_assign", "role.self_remove"),
	auditorRows("custom emoji are shared by the whole community", "emoji.create", "emoji.rename", "emoji.delete"),
	auditorRows("automod rules and what they did to members, without message text",
		"automod.rule_create", "automod.rule_update", "automod.rule_delete", "automod.timeout", "automod.raid"),
	auditorRows("an event without a Talk is open to every member (one in a Talk is event.talk_*)",
		"event.create", "event.edit", "event.cancel", "event.comment_delete",
		"event.role_template_create", "event.role_template_update", "event.role_template_delete"),
	auditorRows("the wiki is open to every member who may view it; the log says who changed which page",
		"wiki.create", "wiki.edit", "wiki.move", "wiki.delete", "wiki.restore", "wiki.lock", "wiki.image"),
	[]Class{{"%", VisPrivileged, "an action without a row of its own is not known to be harmless"}},
)

// auditorRows are rows of the auditors class, one per action.
func auditorRows(why string, actions ...string) []Class {
	out := make([]Class, 0, len(actions))
	for _, a := range actions {
		out = append(out, Class{a, VisAuditors, why})
	}
	return out
}

// ClassOf returns the visibility of an action (the first row that matches).
func ClassOf(action string) Visibility {
	vis := VisPrivileged
	for _, c := range Classes {
		if matchLike(c.Pattern, action) {
			vis = c.Vis
			break
		}
	}
	return vis
}

// matchLike matches the simple LIKE patterns used in Classes: a literal or a
// literal prefix followed by %.
func matchLike(pattern, s string) bool {
	if pattern == "%" {
		return true
	}
	if n := len(pattern); pattern[n-1] == '%' {
		return len(s) >= n-1 && s[:n-1] == pattern[:n-1]
	}
	return pattern == s
}
