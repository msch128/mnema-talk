package auth

import "github.com/google/uuid"

// LiveControl acts on a user's live connections (implemented by the WebSocket hub).
type LiveControl interface {
	// KickFromVoice ends the user's voice presence; false if they were not in one.
	KickFromVoice(userID uuid.UUID) bool
	// VoiceChannels lists the Talks the user is in (or lingers in).
	VoiceChannels(userID uuid.UUID) []uuid.UUID
	// KickFromChannel ends the user's presence in one Talk.
	KickFromChannel(userID, channelID uuid.UUID)
	// DisconnectUser closes all of the user's live connections.
	DisconnectUser(userID uuid.UUID)
	// DisconnectSession closes the connections opened with one session id (a
	// single sign-out); the user's other sessions stay.
	DisconnectSession(jti string)
	// SetPresence applies a newly chosen presence to the user's live status.
	SetPresence(userID uuid.UUID, presence string)
}
