//go:build integration

package later

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/chat/channels"
	"github.com/msch128/mnema-talk/internal/chat/core"
	"github.com/msch128/mnema-talk/internal/chat/messages"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/perm"
	"github.com/msch128/mnema-talk/internal/testutil"
)

func TestMain(m *testing.M) { os.Exit(testutil.Main(m)) }

type fixture struct {
	p       *db.Pool
	user    uuid.UUID
	channel uuid.UUID
	events  *events.Recorder
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	p := testutil.DB(t)
	testutil.Reset(t, p)
	ctx := context.Background()
	var id uuid.UUID
	if err := p.QueryRow(ctx, `INSERT INTO users (username, display_name, password_hash) VALUES ('writer', 'Writer', 'test-only-unused') RETURNING id`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	c, err := channels.CreateChannelWithStage(ctx, p, nil, "general", core.ChannelTypeText, "", 0, false)
	if err != nil {
		t.Fatal(err)
	}
	return fixture{p: p, user: id, channel: c.ID, events: &events.Recorder{}}
}

func (f fixture) service(p *db.Pool) *Service {
	perms := perm.NewService(p)
	return NewService(Service{DB: p, Perms: perms, Events: f.events, Posts: &messages.Service{DB: p, Perms: perms, Events: f.events}})
}
