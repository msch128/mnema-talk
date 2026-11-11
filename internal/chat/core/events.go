package core

import "github.com/msch128/mnema-talk/internal/events"

// MessageCreate announces a new message to those who may view its channel
// (message_create). Publish a message as (*MessageCreate)(msg).
type MessageCreate Message

func (*MessageCreate) EventName() string { return events.MessageCreate }

// MessageUpdate announces a message after an edit, a pin or another change
// (message_update).
type MessageUpdate Message

func (*MessageUpdate) EventName() string { return events.MessageUpdate }
