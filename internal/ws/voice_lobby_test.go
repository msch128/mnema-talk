package ws

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/chat"
	"github.com/msch128/mnema-talk/internal/httpx"
)

type fakeLobby struct {
	opened []uuid.UUID
	talk   uuid.UUID
	err    error
}

func (f *fakeLobby) Open(_ context.Context, _, lobby uuid.UUID) (*chat.ChannelInfo, error) {
	if f.err != nil {
		return nil, f.err
	}
	f.opened = append(f.opened, lobby)
	return &chat.ChannelInfo{ID: f.talk, Type: chat.ChannelTypeVoice}, nil
}

func lobbyRoom() *chat.ChannelInfo {
	return &chat.ChannelInfo{ID: uuid.New(), Type: chat.ChannelTypeVoice, IsLobby: true}
}

func TestJoiningALobbyOpensATalkAndNeverSeatsTheMemberInTheLobby(t *testing.T) {
	h := NewHub(nil, nil, nil, nil)
	t.Cleanup(h.Close)
	lobby := &fakeLobby{talk: uuid.New()}
	h.Lobby = lobby
	c := testClient(h, auth.User{ID: uuid.New(), Username: "ann"})
	drainTypes(t, c)
	room := lobbyRoom()

	h.enterLobby(c, room)
	if len(lobby.opened) != 1 || lobby.opened[0] != room.ID {
		t.Fatalf("opened %v", lobby.opened)
	}
	if inRoom(h, room.ID, c.User.ID) {
		t.Fatal("the member must never sit in the lobby")
	}
	if got := drainTypes(t, c); count(got, "talk_moved") != 1 || count(got, "voice_kicked") != 0 {
		t.Fatalf("events: %v", got)
	}
	// The client then joins the new Talk like any other.
	h.joinVoice(c, &chat.ChannelInfo{ID: lobby.talk, Type: chat.ChannelTypeVoice})
	if !inRoom(h, lobby.talk, c.User.ID) || inRoom(h, room.ID, c.User.ID) {
		t.Fatal("the member is in the new Talk and only there")
	}
}

func TestALobbyThatOpensNoTalkTellsTheMember(t *testing.T) {
	h := NewHub(nil, nil, nil, nil)
	t.Cleanup(h.Close)
	h.Lobby = &fakeLobby{err: httpx.ErrRateLimited()}
	c := testClient(h, auth.User{ID: uuid.New(), Username: "ben"})
	drainTypes(t, c)
	room := lobbyRoom()
	h.enterLobby(c, room)
	if got := drainTypes(t, c); count(got, "voice_kicked") != 1 || count(got, "talk_moved") != 0 {
		t.Fatalf("events: %v", got)
	}
	if inRoom(h, room.ID, c.User.ID) {
		t.Fatal("the member must never sit in the lobby")
	}
}
