package core

// Why a user is mentioned (message_mentions.kind). A user reached more than
// one way is stored once: a name wins over a role, a role over @all or @here
// (a role addresses the member, a group mention does not; see
// internal/chat/mentions).
const (
	MentionKindName  int16 = 0 // @username
	MentionKindGroup int16 = 1 // @all, @here
	MentionKindRole  int16 = 2 // @Role
)
