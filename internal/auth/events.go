package auth

import "github.com/msch128/mnema-talk/internal/events"

// UserUpdate is the long form of user_update: a member's public profile after
// a change of name, status or avatar. The short form, an account disabled or
// enabled, is wire.UserDisabled.
type UserUpdate User

func (UserUpdate) EventName() string { return events.UserUpdate }

// MemberJoined announces a newly registered member (member_joined): their
// public profile.
type MemberJoined User

func (MemberJoined) EventName() string { return events.MemberJoined }

// UserUpdateOf returns the profile of a long-form user_update, whether it was
// published as a value or as a pointer. Both are an events.Event (the methods
// have value receivers), so a consumer that asserted one form alone would
// silently miss the other.
func UserUpdateOf(ev events.Event) (User, bool) {
	switch u := ev.(type) {
	case UserUpdate:
		return User(u), true
	case *UserUpdate:
		return User(*u), true
	}
	return User{}, false
}

// MemberJoinedOf returns the profile of a member_joined, published as a value
// or as a pointer (see UserUpdateOf).
func MemberJoinedOf(ev events.Event) (User, bool) {
	switch u := ev.(type) {
	case MemberJoined:
		return User(u), true
	case *MemberJoined:
		return User(*u), true
	}
	return User{}, false
}
