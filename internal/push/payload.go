package push

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/mdplain"
)

// Payload is what the service worker receives (after the browser decrypted
// it). Title and body are finished, localised text, so the worker needs no
// translations. Tag equals the channel id, which is also the tag of the page's
// own notification: the two replace each other instead of showing twice.
type Payload struct {
	Version   int        `json:"v"`
	Type      string     `json:"type"`
	Title     string     `json:"title"`
	Body      string     `json:"body"`
	Tag       string     `json:"tag"`
	ChannelID *uuid.UUID `json:"channel_id,omitempty"`
	MessageID *uuid.UUID `json:"message_id,omitempty"`
	ParentID  *uuid.UUID `json:"parent_id,omitempty"`
	Mention   bool       `json:"mention,omitempty"`
	Timestamp int64      `json:"ts"`
}

// Payload types.
const (
	typeMessage = "message"
	typeTest    = "test"
)

const (
	maxPreviewRunes = 140
	maxNameRunes    = 64
)

// texts are the few strings a push shows, by language (the account's
// language; German is the app's default).
var texts = map[string]map[string]string{
	"de": {
		"new": "Neue Nachricht", "mention": "Hat dich erwähnt", "attachment": "Anhang", "forward": "Weitergeleitete Nachricht",
		"dm": "Direktnachricht", "group": "%d neue Nachrichten", "poll": "Umfrage", "spoiler": "Spoiler", "test": "Push funktioniert auf diesem Gerät.",
	},
	"en": {
		"new": "New message", "mention": "Mentioned you", "attachment": "Attachment", "forward": "Forwarded message",
		"dm": "Direct message", "group": "%d new messages", "poll": "Poll", "spoiler": "Spoiler", "test": "Push works on this device.",
	},
}

func text(locale, key string) string {
	if t, ok := texts[locale]; ok {
		return t[key]
	}
	return texts["de"][key]
}

// Notice is one message to tell one member about, with everything the text needs.
type Notice struct {
	// AuthorID is who wrote the message; a member who blocked them is not told.
	AuthorID    uuid.UUID
	ChannelID   uuid.UUID
	ChannelName string
	MessageID   uuid.UUID
	ParentID    *uuid.UUID
	Sender      string
	Content     string
	HasFiles    bool
	// Forwarded is set when the message forwards another one.
	Forwarded bool
	// Poll is set when the message is a poll; its content is the question.
	Poll bool
	// DM is set for a message of a direct message conversation: the push
	// shows the sender and no channel name.
	DM      bool
	Mention bool
	// Addressed is set when the message is meant for the member itself (see addressed); it decides about a mute.
	Addressed bool
	// Count is how many messages this push stands for (1 or more).
	Count int
	At    time.Time
}

// build composes the push for one member. Preview says whether the message
// text may be included (the member's choice, and the operator's).
func build(n Notice, locale string, preview bool) Payload {
	p := Payload{
		Version: 1, Type: typeMessage, Tag: n.ChannelID.String(),
		ChannelID: &n.ChannelID, MessageID: &n.MessageID, ParentID: n.ParentID,
		Mention: n.Mention, Timestamp: n.At.UnixMilli(),
	}
	sender, channel := clean(n.Sender, maxNameRunes), clean(n.ChannelName, maxNameRunes)
	line := previewLine(n, locale, preview)
	if n.DM {
		return buildDM(p, n, sender, locale, line, preview)
	}
	if n.Count > 1 {
		p.Title = "#" + channel
		p.Body = fmt.Sprintf(text(locale, "group"), n.Count)
		if preview {
			p.Body += "\n" + sender + ": " + line
		}
		return p
	}
	p.Title = sender + " (#" + channel + ")"
	p.Body = line
	return p
}

// buildDM composes the push of a direct message: the sender's name, never a
// channel; a burst from possibly several people is titled by the kind of
// conversation.
func buildDM(p Payload, n Notice, sender, locale, line string, preview bool) Payload {
	if n.Count > 1 {
		p.Title = text(locale, "dm")
		p.Body = fmt.Sprintf(text(locale, "group"), n.Count)
		if preview {
			p.Body += "\n" + sender + ": " + line
		}
		return p
	}
	p.Title, p.Body = sender, line
	return p
}

// previewLine is the text of the latest message, or a neutral line when the
// text must not be shown.
func previewLine(n Notice, locale string, preview bool) string {
	switch {
	case !preview && n.Mention && !n.DM:
		return text(locale, "mention")
	case !preview:
		return text(locale, "new")
	}
	if line := clean(mdplain.Plain(n.Content, text(locale, "spoiler")), maxPreviewRunes); line != "" {
		if n.Poll {
			return text(locale, "poll") + ": " + line
		}
		return line
	}
	if n.HasFiles {
		return text(locale, "attachment")
	}
	if n.Forwarded {
		return text(locale, "forward")
	}
	return text(locale, "new")
}

// clean makes text safe to show in a notification: control characters become
// spaces, runs of white space collapse, and the result is cut at limit runes.
func clean(s string, limit int) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return ' '
		}
		return r
	}, s)
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) <= limit {
		return s
	}
	runes := []rune(s)
	return strings.TrimSpace(string(runes[:limit-1])) + "…"
}

// testPayload is what the test button sends.
func testPayload(locale string, at time.Time) Payload {
	return Payload{Version: 1, Type: typeTest, Title: "Mnema Talk", Body: text(locale, "test"), Tag: "test", Timestamp: at.UnixMilli()}
}

// encode serialises the payload; it always fits one push record.
func (p Payload) encode() ([]byte, error) {
	return json.Marshal(p)
}
