package auth

// An account can act (sign in, keep a session, open a socket) only when it is
// neither disabled nor banned. The two states are separate columns so that
// lifting one never lifts the other: disabled_at is set by Disable, banned_at
// by Ban, and a ban whose banned_until has passed no longer counts even if the
// expiry job has not cleared it yet. Every query that decides whether an
// account may act builds its condition from here.

// ActiveSQL is the condition "this account may act"; alias qualifies the
// users table ("" for none).
func ActiveSQL(alias string) string {
	return "NOT " + InactiveSQL(alias)
}

// InactiveSQL is the condition "this account is disabled or banned".
func InactiveSQL(alias string) string {
	p := alias
	if p != "" {
		p += "."
	}
	return "(" + p + "disabled_at IS NOT NULL OR " + p + "deleted_at IS NOT NULL OR (" + p + "banned_at IS NOT NULL AND (" + p + "banned_until IS NULL OR " + p + "banned_until > NOW())))"
}
