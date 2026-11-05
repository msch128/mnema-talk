package core

// Why a user is mentioned (message_mentions.kind). A lower kind wins when a
// user is reached more than one way.
const (
	MentionKindName  int16 = 0 // @username
	MentionKindGroup int16 = 1 // @all, @here
	MentionKindRole  int16 = 2 // @Role
)
