package communityevents

import (
	"time"

	"github.com/google/uuid"
)

// RecurrenceRequest makes a new event the first of a series.
type RecurrenceRequest struct {
	Frequency string `json:"frequency" enums:"weekly,biweekly,monthly"`
	// Timezone is the IANA zone whose wall-clock time the occurrences keep (the browser's zone); UTC when omitted.
	Timezone string `json:"timezone,omitempty" maxLength:"64" binding:"optional"`
	// Until: no occurrence starts after this time. Without it the series runs for at most 200 occurrences.
	Until *time.Time `json:"until,omitempty" format:"date-time" binding:"optional" extensions:"x-nullable"`
}

// SeriesInfo says which series an event belongs to.
type SeriesInfo struct {
	ID        uuid.UUID  `json:"id" format:"uuid"`
	Frequency string     `json:"frequency" enums:"weekly,biweekly,monthly"`
	Timezone  string     `json:"timezone"`
	Until     *time.Time `json:"until,omitempty" format:"date-time" binding:"optional" extensions:"x-nullable"`
	// Index is the occurrence's number in the series (0 is the first).
	Index int `json:"index"`
	// Ended: the series creates no more occurrences.
	Ended bool `json:"ended"`
}
