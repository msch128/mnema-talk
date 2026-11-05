package chat

import (
	"time"

	"github.com/google/uuid"
)

type Category struct {
	ID        uuid.UUID `json:"id" format:"uuid"`
	Name      string    `json:"name" maxLength:"64"`
	SortOrder int       `json:"sort_order"`
	Channels  []Channel `json:"channels"`
	CreatedAt time.Time `json:"created_at" format:"date-time"`
}
