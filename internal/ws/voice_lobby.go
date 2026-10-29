package ws

import (
	"context"
	"errors"
	"log/slog"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/chat"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// LobbyOpener opens the temporary Talk a join of a Create-a-Talk lobby leads
// to (implemented by chat.TalkManager).
type LobbyOpener interface {
	Open(ctx context.Context, user, lobby uuid.UUID) (*chat.ChannelInfo, error)
}

// enterLobby answers a join of a lobby: it opens a temporary Talk and tells
// the connection to join it (talk_moved). The connection is never seated in
// the lobby, and the join itself is the client's ordinary voice_join of the new
// Talk, so media setup and the SFU see nothing unusual. Only a member who may
// join the lobby gets a Talk; the new Talk carries the lobby's permissions, and
// that join checks them again.
func (h *Hub) enterLobby(c *Client, lobby *chat.ChannelInfo) {
	if !h.checkJoinPermission(c, lobby.ID) {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), permTimeout)
	defer cancel()
	talk, err := h.Lobby.Open(ctx, c.User.ID, lobby.ID)
	if err != nil {
		slog.Info("talk lobby refused", "user", c.User.Username, "lobby", lobby.ID, "err", err)
		c.SendEvent(events.VoiceKicked, map[string]any{"channel_id": lobby.ID, "reason": lobbyRefusal(err)})
		return
	}
	c.SendEvent(events.TalkMoved, map[string]any{"from_channel_id": lobby.ID, "channel_id": talk.ID})
}

// lobbyRefusal is the voice_kicked reason of a lobby that opened no Talk.
func lobbyRefusal(err error) string {
	var apiErr *httpx.APIError
	if errors.As(err, &apiErr) && apiErr.Code == httpx.CodeRateLimited {
		return "talk_rate_limited"
	}
	return "talk_unavailable"
}
