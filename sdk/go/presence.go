package mnema

import (
	"context"
	"fmt"
)

// ActivityType is the kind of a bot's activity.
type ActivityType int

// The activity types the server accepts (streaming, 1, is refused).
const (
	ActivityPlaying   ActivityType = 0
	ActivityListening ActivityType = 2
	ActivityWatching  ActivityType = 3
	ActivityCustom    ActivityType = 4
	ActivityCompeting ActivityType = 5
)

// The statuses of a bot.
const (
	StatusOnline    = "online"
	StatusIdle      = "idle"
	StatusDND       = "dnd"
	StatusInvisible = "invisible"
)

// Presence is the bot's status and optional activity.
type Presence struct {
	// Status is online, idle, dnd or invisible (offline is an alias of
	// invisible).
	Status   string
	Activity *Activity
}

// Activity is what the bot is doing ("Playing chess"). A custom activity
// (type 4) shows State, or Name when State is empty. The server cuts the
// text to 64 characters.
type Activity struct {
	Name  string       `json:"name"`
	Type  ActivityType `json:"type"`
	State string       `json:"state,omitempty"`
}

// Validate checks the status and the activity type.
func (p Presence) Validate() error {
	switch p.Status {
	case StatusOnline, StatusIdle, StatusDND, StatusInvisible, "offline":
	default:
		return fmt.Errorf("mnema: presence status %q, want online, idle, dnd or invisible", p.Status)
	}
	if p.Activity == nil {
		return nil
	}
	switch p.Activity.Type {
	case ActivityPlaying, ActivityListening, ActivityWatching, ActivityCustom, ActivityCompeting:
		return nil
	default:
		return fmt.Errorf("mnema: activity type %d is not offered (streaming needs a URL and is refused)", p.Activity.Type)
	}
}

type presenceWire struct {
	Since      *int64     `json:"since"`
	AFK        bool       `json:"afk"`
	Status     string     `json:"status"`
	Activities []Activity `json:"activities"`
}

func (p Presence) wire() presenceWire {
	w := presenceWire{Status: p.Status, Activities: []Activity{}}
	if p.Activity != nil {
		w.Activities = append(w.Activities, *p.Activity)
	}
	return w
}

// SetPresence sets the bot's status and activity (gateway op 3). While no
// connection is live it is kept and sent with the next IDENTIFY or after the
// next RESUME. The server applies at most 5 updates per 60 seconds and drops
// further ones without an error.
func (c *Client) SetPresence(ctx context.Context, p Presence) error {
	if err := p.Validate(); err != nil {
		return err
	}
	c.mu.Lock()
	c.presence = &p
	cn := c.conn
	c.mu.Unlock()
	if cn == nil {
		return nil
	}
	return cn.send(ctx, opPresenceUpdate, p.wire())
}

func (c *Client) currentPresence() *Presence {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.presence
}
