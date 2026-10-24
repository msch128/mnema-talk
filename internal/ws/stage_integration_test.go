//go:build integration

package ws

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/chat"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/emojitext"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/perm"
	"github.com/msch128/mnema-talk/internal/sfu"
	"github.com/msch128/mnema-talk/internal/testutil"
)

// A Stage and its members as the database has them, with the real permission
// service: the Stage flag, a role holding Manage stage and the roles' effect
// on media all come from the stored state.
type dbStage struct {
	p        *db.Pool
	h        *Hub
	ch       *chat.ChannelInfo
	mod, amy *Client
	bob      *Client
	peers    map[*Client]*sfu.Peer
}

func newDBStage(t *testing.T) *dbStage {
	t.Helper()
	p := testutil.DB(t)
	testutil.Reset(t, p)
	ctx := context.Background()
	s := &dbStage{p: p, peers: map[*Client]*sfu.Peer{}}
	var chID uuid.UUID
	if err := p.QueryRow(ctx, `INSERT INTO channels (name, type, is_stage) VALUES ('Stage', 'voice', TRUE) RETURNING id`).Scan(&chID); err != nil {
		t.Fatal(err)
	}
	var role uuid.UUID
	if err := p.QueryRow(ctx, `INSERT INTO roles (name, position, permissions) VALUES ('Hosts', 1, $1) RETURNING id`, int64(perm.ManageStage)).Scan(&role); err != nil {
		t.Fatal(err)
	}
	s.h = NewHub(p, nil, nil, nil)
	s.h.SetPermissions(perm.NewService(p))
	t.Cleanup(s.h.Close)
	s.mod, s.amy, s.bob = s.member(t, "stage_mod"), s.member(t, "stage_amy"), s.member(t, "stage_bob")
	if _, err := p.Exec(ctx, `INSERT INTO user_roles (user_id, role_id) VALUES ($1, $2)`, s.mod.User.ID, role); err != nil {
		t.Fatal(err)
	}
	s.h.Perms.Invalidate()
	var err error
	if s.ch, err = chat.LoadChannel(ctx, p, chID); err != nil || !s.ch.IsStage {
		t.Fatalf("stage channel: %+v, %v", s.ch, err)
	}
	for _, c := range []*Client{s.mod, s.amy, s.bob} {
		s.h.joinVoice(c, s.ch)
		s.peers[c] = &sfu.Peer{}
		c.setPeer(s.peers[c])
		snap, _ := s.h.permSnapshot(chID)
		s.h.applyMediaPolicy(c, snap)
		drainTypes(t, c)
	}
	return s
}

func (s *dbStage) member(t *testing.T, name string) *Client {
	t.Helper()
	u := auth.User{ID: uuid.New(), Username: name, DisplayName: name}
	if _, err := s.p.Exec(context.Background(), `INSERT INTO users(id,username,display_name,password_hash,role) VALUES($1,$2,$2,'x','user')`, u.ID, u.Username); err != nil {
		t.Fatal(err)
	}
	c := testClient(s.h, u)
	drainTypes(t, c)
	return c
}

func TestStageFromTheDatabaseRefusesMediaUntilManageStageGrantsIt(t *testing.T) {
	s := newDBStage(t)
	all := sfu.SourceSet(0).With(sfu.SourceAudio).With(sfu.SourceScreenAudio).With(sfu.SourceScreen).With(sfu.SourceCamera)
	if s.peers[s.amy].DeniedSources() != all || s.peers[s.bob].DeniedSources() != all {
		t.Fatal("the audience of a stored Stage may send media")
	}
	if s.peers[s.mod].DeniedSources() != 0 {
		t.Fatal("the holder of Manage stage was denied")
	}
	grant := map[string]any{"user_id": s.amy.User.ID, "granted": true}
	sendEvent(s.bob, events.ClientStageTalkPower, grant) // a member without the permission
	if s.peers[s.amy].DeniedSources() != all {
		t.Fatal("a member handed out talk power without Manage stage")
	}
	sendEvent(s.mod, events.ClientStageTalkPower, grant)
	if s.peers[s.amy].DeniedSources() != 0 {
		t.Fatal("the grant did not lift the refusal")
	}
	if s.peers[s.bob].DeniedSources() != all {
		t.Fatal("a grant to one member reached another")
	}
	sendEvent(s.mod, events.ClientStageTalkPower, map[string]any{"user_id": s.amy.User.ID, "granted": false})
	if s.peers[s.amy].DeniedSources() != all {
		t.Fatal("the revoke did not restore the refusal")
	}
}

func TestStageFlagChangeInTheDatabaseReachesTheRoom(t *testing.T) {
	s := newDBStage(t)
	ctx := context.Background()
	if err := chat.SetStage(ctx, s.p, s.ch.ID, false); err != nil {
		t.Fatal(err)
	}
	s.h.Perms.Changed(nil)
	s.h.PermissionsChanged(nil)
	if s.peers[s.amy].DeniedSources() != 0 {
		t.Fatal("a normal Talk still limits the audience")
	}
	if err := chat.SetStage(ctx, s.p, s.ch.ID, true); err != nil {
		t.Fatal(err)
	}
	s.h.Perms.Changed(nil)
	s.h.PermissionsChanged(nil)
	if s.peers[s.amy].DeniedSources() == 0 {
		t.Fatal("making the Talk a Stage did not silence the audience")
	}
}

func insertCustomEmoji(t *testing.T, p *db.Pool, by uuid.UUID) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := p.Exec(context.Background(), `
		INSERT INTO custom_emoji (id, name, content_type, animated, s3_key, size_bytes, content_hash, created_by)
		VALUES ($1, $2, 'image/png', FALSE, $3, 10, $4, $5)`,
		id, "stage_"+id.String()[:8], "emoji/"+id.String(), id.String()+id.String(), by)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestLiveReactionWithACustomEmojiNeedsItToExist(t *testing.T) {
	s := newDBStage(t)
	id := insertCustomEmoji(t, s.p, s.mod.User.ID)
	token := emojitext.Token("party", id)
	reaction(t, s.amy, token)
	if got := reactions(t, s.bob); len(got) != 1 || got[0] != token {
		t.Fatalf("a member could not use a custom emoji of the community: %v", got)
	}
	// An emoji that does not exist (or no longer does) floats nowhere.
	reaction(t, s.amy, emojitext.Token("ghost", uuid.New()))
	if _, err := s.p.Exec(context.Background(), `DELETE FROM custom_emoji WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}
	reaction(t, s.amy, token)
	if got := reactions(t, s.bob); len(got) != 0 {
		t.Fatalf("a missing emoji was relayed: %v", got)
	}
}
