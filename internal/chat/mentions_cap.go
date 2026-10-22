package chat

import (
	"bytes"
	"slices"
)

const (
	// MaxMentionRecipients is how many members one message notifies when its
	// author may not mention everyone (MentionPolicy.NoEveryone): named
	// users and the members of mentioned roles together. A broad mentionable
	// role would otherwise be @everyone without the permission. Whoever may
	// mention everyone is not capped, since @all reaches the whole community
	// anyway. Beyond the cap the text and role pills stay, but nobody more is
	// notified.
	MaxMentionRecipients = 50
	// maxMentionNames bounds the distinct @names read from one message for
	// everybody (about 800 would fit in a 4000-character message).
	maxMentionNames = 100
)

func capNames(names []string) []string {
	if len(names) > maxMentionNames {
		return names[:maxMentionNames]
	}
	return names
}

// capRecipients applies MaxMentionRecipients: named users first, then role
// members, the rest in a fixed order so the same message always notifies the
// same people.
func capRecipients(set MentionSet, policy MentionPolicy) MentionSet {
	if !policy.NoEveryone || len(set.Users) <= MaxMentionRecipients {
		return set
	}
	slices.SortFunc(set.Users, func(a, b MentionedUser) int {
		if a.Kind != b.Kind {
			return int(a.Kind) - int(b.Kind)
		}
		return bytes.Compare(a.ID[:], b.ID[:])
	})
	set.Users = set.Users[:MaxMentionRecipients]
	return set
}
