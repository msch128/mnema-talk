//go:build integration

package chat

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/perm"
	"github.com/msch128/mnema-talk/internal/testutil"
)

func TestOnlyAPermanentTalkThatIsNoLobbyBecomesAStage(t *testing.T) {
	p := testutil.DB(t)
	testutil.Reset(t, p)
	ctx := context.Background()
	talk, err := CreateChannel(ctx, p, nil, "Town hall", ChannelTypeVoice, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	text, err := CreateChannel(ctx, p, nil, "general", ChannelTypeText, "", 1)
	if err != nil {
		t.Fatal(err)
	}
	lobby, err := CreateChannel(ctx, p, nil, "Create a Talk", ChannelTypeVoice, "", 2)
	if err != nil {
		t.Fatal(err)
	}
	if err := SetLobby(ctx, p, lobby.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := SetStage(ctx, p, talk.ID, true); err != nil {
		t.Fatalf("a permanent Talk becomes a Stage: %v", err)
	}
	for name, id := range map[string]uuid.UUID{"a text channel": text.ID, "a lobby": lobby.ID} {
		if err := SetStage(ctx, p, id, true); err == nil {
			t.Fatalf("%s became a Stage", name)
		}
	}
	if err := SetStage(ctx, p, uuid.New(), true); err == nil {
		t.Fatal("an unknown channel became a Stage")
	}
	// A Stage cannot become a lobby; a normal Talk can again once the Stage is lifted.
	if err := SetLobby(ctx, p, talk.ID, true); err == nil {
		t.Fatal("a Stage became a lobby")
	}
	if err := SetStage(ctx, p, talk.ID, false); err != nil {
		t.Fatal(err)
	}
	if err := SetLobby(ctx, p, talk.ID, true); err != nil {
		t.Fatalf("a normal Talk becomes a lobby: %v", err)
	}
}

func TestTheChannelListAndThePermissionStateKnowTheStage(t *testing.T) {
	p := testutil.DB(t)
	testutil.Reset(t, p)
	ctx := context.Background()
	stage, err := CreateChannel(ctx, p, nil, "Town hall", ChannelTypeVoice, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := SetStage(ctx, p, stage.ID, true); err != nil {
		t.Fatal(err)
	}
	cats, loose, err := GetServerHierarchy(ctx, p)
	if err != nil || len(cats) != 0 || len(loose) != 1 || !loose[0].IsStage {
		t.Fatalf("channel list: %+v %v", loose, err)
	}
	info, err := LoadChannel(ctx, p, stage.ID)
	if err != nil || !info.IsStage {
		t.Fatalf("channel info: %+v %v", info, err)
	}
	snap, err := perm.NewService(p).Snapshot(ctx)
	if err != nil || !snap.IsStage(stage.ID) {
		t.Fatalf("permission state: %v", err)
	}
	FillTalkFields(ctx, p, stage)
	if !stage.IsStage {
		t.Fatal("FillTalkFields forgot the Stage flag")
	}
}

func TestTheDatabaseKeepsStagesApartFromLobbiesAndTemporaryTalks(t *testing.T) {
	p := testutil.DB(t)
	testutil.Reset(t, p)
	ctx := context.Background()
	stage, err := CreateChannel(ctx, p, nil, "Town hall", ChannelTypeVoice, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := SetStage(ctx, p, stage.ID, true); err != nil {
		t.Fatal(err)
	}
	// The database refuses what the code never writes.
	if _, err := p.Exec(ctx, `UPDATE channels SET is_lobby = TRUE WHERE id = $1`, stage.ID); err == nil {
		t.Fatal("the database allowed a lobby that is a Stage")
	}
	if _, err := p.Exec(ctx, `UPDATE channels SET is_temp = TRUE WHERE id = $1`, stage.ID); err == nil {
		t.Fatal("the database allowed a temporary Talk that is a Stage")
	}
}
