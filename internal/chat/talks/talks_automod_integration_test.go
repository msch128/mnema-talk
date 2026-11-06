//go:build integration

package talks

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/msch128/mnema-talk/internal/audit"
	"github.com/msch128/mnema-talk/internal/automod"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
)

func TestALeaderCannotRenameATalkToWhatAutomodBlocks(t *testing.T) {
	w := newTalksWorld(t)
	ctx := context.Background()
	engine := automod.New(w.p, perm.NewService(w.p), w.rec, &audit.Log{DB: w.p})
	automod.Attach(w.p, engine)
	t.Cleanup(func() { automod.Detach(w.p, engine) })
	in, err := automod.RuleInput{Kind: automod.KindWords, Name: "words", Enabled: true, Action: automod.ActionBlock, Config: automod.Settings{Words: []string{"badword"}}}.Normalize()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := automod.CreateRule(ctx, w.p, in); err != nil {
		t.Fatal(err)
	}
	engine.Invalidate()

	talk, err := w.mgr.Open(ctx, w.alice.ID, w.lobby.ID)
	if err != nil {
		t.Fatal(err)
	}
	params := map[string]string{"id": talk.ID.String()}
	err = w.api().updateTalk(httptest.NewRecorder(), talkRequest(w.alice, `{"name":"my BADWORD room"}`, params))
	if api, ok := httpx.AsAPIError(err); !ok || api.Status != 400 {
		t.Fatalf("renaming to a blocked word: %v, want 400", err)
	}
	var name string
	if err := w.p.QueryRow(ctx, `SELECT name FROM channels WHERE id = $1`, talk.ID).Scan(&name); err != nil || name != "Create a Talk 1" {
		t.Fatalf("name after the refused rename = %q, %v; want %q", name, err, "Create a Talk 1")
	}
	if err := w.api().updateTalk(httptest.NewRecorder(), talkRequest(w.alice, `{"name":"Study room"}`, params)); err != nil {
		t.Fatalf("a clean name: %v", err)
	}
}
