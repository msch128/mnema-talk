package auth

import (
	"context"
	"io"

	"github.com/google/uuid"
)

// The two services package auth is handed by the server: the live WebSocket
// connections and the object store.

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

// ObjectStore is the part of the object store the account flows use (uploads
// to remove after a deletion, the export archive). media.Store satisfies it.
type ObjectStore interface {
	Upload(ctx context.Context, key string, body io.Reader, mimeType string, size int64) error
	GetObjectFrom(ctx context.Context, key string, offset int64) (io.ReadCloser, error)
	Delete(ctx context.Context, key string) error
	DeleteBatch(ctx context.Context, keys []string) error
}
