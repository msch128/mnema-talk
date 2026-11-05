package chat

import "github.com/google/uuid"

// VoiceRooms acts on live voice rooms (implemented by the WebSocket hub).
type VoiceRooms interface {
	// CloseVoiceChannel disconnects everyone from the channel's voice room.
	CloseVoiceChannel(channelID uuid.UUID)
}
