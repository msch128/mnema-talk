package chat

// blockExists is the user_blocks lookup shared by inboxes, unread counts and
// the conversation list. blockerPH is the member who blocked; authorExpr is
// the message author. History search does not use it: the client collapses
// those messages, and a block stays invisible to the member who was blocked.
// A mutual block (a direct message that must not open) and push ("the
// recipient blocked the author") are different rules and do not call this.
func blockExists(blockerPH, authorExpr string) string {
	return "SELECT 1 FROM user_blocks ub WHERE ub.blocker_id = " + blockerPH + " AND ub.blocked_id = " + authorExpr
}

// authorHiddenFrom drops a row whose author blockerPH has blocked.
func authorHiddenFrom(blockerPH, authorExpr string) string {
	return "NOT EXISTS (" + blockExists(blockerPH, authorExpr) + ")"
}
