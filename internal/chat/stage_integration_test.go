//go:build integration

package chat

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/chat/channels"
	"github.com/msch128/mnema-talk/internal/chat/core"
	"github.com/msch128/mnema-talk/internal/chat/stage"
	"github.com/msch128/mnema-talk/internal/perm"
	"github.com/msch128/mnema-talk/internal/testutil"
)

func TestOnlyAPermanentTalkThatIsNoLobbyBecomesAStage(t *testing.T) {
	p := testutil.DB(t)
	testutil.Reset(t, p)
	ctx := context.Background()
	talk, err := channels.CreateChannelWithStage(ctx, p, nil, "Town hall", core.ChannelTypeVoice, "", 0, false)
	if err != nil {
		t.Fatal(err)
	}
	text, err := channels.CreateChannelWithStage(ctx, p, nil, "general", core.ChannelTypeText, "", 1, false)
	if err != nil {
		t.Fatal(err)
	}
	lobby, err := channels.CreateChannelWithStage(ctx, p, nil, "Create a Talk", core.ChannelTypeVoice, "", 2, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := channels.SetLobby(ctx, p, lobby.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := stage.SetStage(ctx, p, talk.ID, true); err != nil {
		t.Fatalf("a permanent Talk becomes a Stage: %v", err)
	}
	for name, id := range map[string]uuid.UUID{"a text channel": text.ID, "a lobby": lobby.ID} {
		if err := stage.SetStage(ctx, p, id, true); err == nil {
			t.Fatalf("%s became a Stage", name)
		}
	}
	if err := stage.SetStage(ctx, p, uuid.New(), true); err == nil {
		t.Fatal("an unknown channel became a Stage")
	}
	// A Stage cannot become a lobby; a normal Talk can again once the Stage is lifted.
	if err := channels.SetLobby(ctx, p, talk.ID, true); err == nil {
		t.Fatal("a Stage became a lobby")
	}
	if err := stage.SetStage(ctx, p, talk.ID, false); err != nil {
		t.Fatal(err)
	}
	if err := channels.SetLobby(ctx, p, talk.ID, true); err != nil {
		t.Fatalf("a normal Talk becomes a lobby: %v", err)
	}
}

func TestTheChannelListAndThePermissionStateKnowTheStage(t *testing.T) {
	p := testutil.DB(t)
	testutil.Reset(t, p)
	ctx := context.Background()
	talk, err := channels.CreateChannelWithStage(ctx, p, nil, "Town hall", core.ChannelTypeVoice, "", 0, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := stage.SetStage(ctx, p, talk.ID, true); err != nil {
		t.Fatal(err)
	}
	cats, loose, err := channels.GetServerHierarchy(ctx, p)
	if err != nil || len(cats) != 0 || len(loose) != 1 || !loose[0].IsStage {
		t.Fatalf("channel list: %+v %v", loose, err)
	}
	info, err := core.LoadChannel(ctx, p, talk.ID)
	if err != nil || !info.IsStage {
		t.Fatalf("channel info: %+v %v", info, err)
	}
	snap, err := perm.NewService(p).Snapshot(ctx)
	if err != nil || !snap.IsStage(talk.ID) {
		t.Fatalf("permission state: %v", err)
	}
	if err := channels.FillTalkFields(ctx, p, talk); err != nil {
		t.Fatal(err)
	}
	if !talk.IsStage {
		t.Fatal("FillTalkFields forgot the Stage flag")
	}
}

func TestTheDatabaseKeepsStagesApartFromLobbiesAndTemporaryTalks(t *testing.T) {
	p := testutil.DB(t)
	testutil.Reset(t, p)
	ctx := context.Background()
	talk, err := channels.CreateChannelWithStage(ctx, p, nil, "Town hall", core.ChannelTypeVoice, "", 0, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := stage.SetStage(ctx, p, talk.ID, true); err != nil {
		t.Fatal(err)
	}
	// The database refuses what the code never writes.
	if _, err := p.Exec(ctx, `UPDATE channels SET is_lobby = TRUE WHERE id = $1`, talk.ID); err == nil {
		t.Fatal("the database allowed a lobby that is a Stage")
	}
	if _, err := p.Exec(ctx, `UPDATE channels SET is_temp = TRUE WHERE id = $1`, talk.ID); err == nil {
		t.Fatal("the database allowed a temporary Talk that is a Stage")
	}
}
