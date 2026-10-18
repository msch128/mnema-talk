package gateway

import (
	"encoding/json"
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
)

// OpPresenceUpdate is Discord's PRESENCE_UPDATE opcode: the bot sets its own
// status and activity.
const OpPresenceUpdate = 3

const (
	// presencePerMinute bounds presence updates per bot (all its connections
	// together); further updates are dropped silently, as Discord does.
	presencePerMinute = 5
	// MaxActivityLen is the longest activity text, in characters.
	MaxActivityLen = 64
)

// Statuses a bot can set, with Discord's names. "offline" is accepted as an
// alias of "invisible".
const (
	StatusOnline    = "online"
	StatusIdle      = "idle"
	StatusDND       = "dnd"
	StatusInvisible = "invisible"
)

// Activity types, with Discord's numbers. Streaming (1) needs a stream URL
// and is not offered.
const (
	ActivityPlaying   = 0
	ActivityListening = 2
	ActivityWatching  = 3
	ActivityCustom    = 4
	ActivityCompeting = 5
)

// Activity is what a bot is doing, as shown next to it in the member list.
type Activity struct {
	// Name is the text, e.g. the game or the custom status.
	Name string `json:"name"`
	// Type is Discord's activity type: 0 playing, 2 listening, 3 watching,
	// 4 custom, 5 competing.
	Type int `json:"type"`
}

// PresenceUpdate is the d of PRESENCE_UPDATE (and the presence of IDENTIFY),
// in Discord's format. Since and AFK are accepted and ignored. Only the first
// activity is used.
type PresenceUpdate struct {
	Since      *int64            `json:"since"`
	Activities []ActivityRequest `json:"activities"`
	Status     string            `json:"status"`
	AFK        bool              `json:"afk"`
}

// ActivityRequest is one activity as the bot sends it. For a custom status
// (type 4) the text is State, as in Discord, or Name.
type ActivityRequest struct {
	Name  string `json:"name"`
	Type  int    `json:"type"`
	State string `json:"state"`
}

// Presence is a bot's validated status and activity.
type Presence struct {
	Status   string
	Activity *Activity
}

var errBadPresence = errors.New("invalid presence")

// parsePresence validates a PresenceUpdate.
func parsePresence(raw json.RawMessage) (Presence, error) {
	var req PresenceUpdate
	if json.Unmarshal(raw, &req) != nil {
		return Presence{}, errBadPresence
	}
	return req.validate()
}

// validate checks the status and the first activity. The activity text is
// stripped of control and formatting characters (bidi overrides included),
// whitespace is collapsed, and it is cut to MaxActivityLen; an activity left
// empty is none.
func (req PresenceUpdate) validate() (Presence, error) {
	p := Presence{Status: req.Status}
	switch req.Status {
	case StatusOnline, StatusIdle, StatusDND, StatusInvisible:
	case "offline":
		p.Status = StatusInvisible
	default:
		return Presence{}, errBadPresence
	}
	if len(req.Activities) == 0 {
		return p, nil
	}
	a := req.Activities[0]
	switch a.Type {
	case ActivityPlaying, ActivityListening, ActivityWatching, ActivityCustom, ActivityCompeting:
	default:
		return Presence{}, errBadPresence
	}
	text := a.Name
	if a.Type == ActivityCustom && a.State != "" {
		text = a.State
	}
	if text = cleanActivity(text); text != "" {
		p.Activity = &Activity{Name: text, Type: a.Type}
	}
	return p, nil
}

func cleanActivity(s string) string {
	if !utf8.ValidString(s) {
		return ""
	}
	s = strings.Map(func(r rune) rune {
		switch {
		case unicode.IsSpace(r):
			return ' '
		case unicode.IsControl(r), unicode.Is(unicode.Cf, r), unicode.Is(unicode.Co, r), r == utf8.RuneError:
			return -1
		}
		return r
	}, s)
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > MaxActivityLen {
		s = strings.TrimSpace(string(r[:MaxActivityLen]))
	}
	return s
}

// setPresence stores a bot's presence and reports it to OnStatus. A status
// set after the bot's last session ended is dropped: nothing would clear it.
func (g *Gateway) setPresence(userID uuid.UUID, p Presence) {
	g.pcb.Lock()
	defer g.pcb.Unlock()
	g.mu.Lock()
	if !g.hasSessionLocked(userID, nil) {
		g.mu.Unlock()
		return
	}
	if g.pres == nil {
		g.pres = map[uuid.UUID]Presence{}
	}
	g.pres[userID] = p
	g.mu.Unlock()
	if g.OnStatus != nil {
		g.OnStatus(userID, p.Status, p.Activity)
	}
}

// resetIdlePresence returns a bot that identified without a presence of its
// own to online, unless another connection of it is live: the status of its
// earlier, detached session is not meant for a new one.
func (g *Gateway) resetIdlePresence(s *session) {
	g.pcb.Lock()
	defer g.pcb.Unlock()
	g.mu.Lock()
	_, had := g.pres[s.userID]
	if !had || g.liveSessionLocked(s.userID, s) {
		g.mu.Unlock()
		return
	}
	delete(g.pres, s.userID)
	g.mu.Unlock()
	if g.OnStatus != nil {
		g.OnStatus(s.userID, StatusOnline, nil)
	}
}

// hasSessionLocked reports whether the bot has a session, live or detached,
// other than except.
func (g *Gateway) hasSessionLocked(userID uuid.UUID, except *session) bool {
	for _, s := range g.sessions {
		if s.userID == userID && s != except {
			return true
		}
	}
	return false
}

// liveSessionLocked reports whether the bot has a connected session other
// than except.
func (g *Gateway) liveSessionLocked(userID uuid.UUID, except *session) bool {
	for _, s := range g.sessions {
		if s.userID == userID && s != except && s.live() {
			return true
		}
	}
	return false
}
