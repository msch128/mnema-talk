package core

import (
	"time"

	"github.com/google/uuid"
)

// Poll is the poll attached to a message (created by /poll or the composer's
// poll button; the message's content is the question). It is the same for
// every viewer, so it travels in message_create and in message lists: counts,
// and for a poll that is not anonymous the voters of each option. A viewer's
// own votes are asked for separately (GET .../poll). An anonymous poll stores
// no choice at all: only the counters and that a member voted.
type Poll struct {
	// Multiple lets a member vote for several options.
	Multiple bool `json:"multiple"`
	// Anonymous polls never name voters: options carry counts only, and only
	// once the poll has ended (a running one reports 0 for every option).
	Anonymous bool `json:"anonymous"`
	// ClosesAt is when voting ends; ending the poll early moves it to that moment.
	ClosesAt time.Time `json:"closes_at" format:"date-time"`
	// Closed is true once ClosesAt has passed, as the server saw it.
	Closed  bool         `json:"closed"`
	Options []PollOption `json:"options"`
	// TotalVoters counts members who voted for at least one option.
	TotalVoters int `json:"total_voters"`
}

// PollOption is one answer with its tally.
type PollOption struct {
	ID    uuid.UUID `json:"id" format:"uuid"`
	Label string    `json:"label" maxLength:"55"`
	Votes int       `json:"votes"`
	// Voters is only present when the poll is not anonymous.
	Voters []uuid.UUID `json:"voters,omitempty" binding:"optional" format:"uuid"`
}
