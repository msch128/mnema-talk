package auth

import "github.com/msch128/mnema-talk/internal/events"

// UserUpdate is the long form of user_update: a member's public profile after
// a change of name, status or avatar. The short form, an account disabled or
// enabled, is payload.UserDisabled.
type UserUpdate User

func (UserUpdate) EventName() string { return events.UserUpdate }

// MemberJoined announces a newly registered member (member_joined): their
// public profile.
type MemberJoined User

func (MemberJoined) EventName() string { return events.MemberJoined }
