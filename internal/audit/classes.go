package audit

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

// Classes is the single table that decides who reads what. Every action the
// server records must fall under one row; the rest of the actions default to
// VisAuditors, which is the last row. A test checks that each recorded action
// is covered by a row on purpose.
var Classes = []Class{
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
	{"category.%", VisPrivileged, "names and overwrites of a possibly private category"},
	{"layout.apply", VisPrivileged, "lists channel and category placements, private ones included"},
	{"channel.%", VisChannel, "names, topics and overwrites of a possibly private channel"},
	{"%", VisAuditors, "administration and moderation"},
}

// patterns lists the patterns of the rows with the given visibility.
func patterns(v Visibility) []string {
	var out []string
	for _, c := range Classes {
		if c.Vis == v && c.Pattern != "%" {
			out = append(out, c.Pattern)
		}
	}
	return out
}

// ClassOf returns the visibility of an action (the first row that matches).
func ClassOf(action string) Visibility {
	vis := VisAuditors
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
