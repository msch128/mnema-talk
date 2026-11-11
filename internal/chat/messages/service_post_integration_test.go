//go:build integration

package messages

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/audit"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/automod"
	"github.com/msch128/mnema-talk/internal/chat/chattest"
	"github.com/msch128/mnema-talk/internal/chat/core"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
)

// postWorld is a community with a member, the Owner, a bot and a second
// member who already wrote in the one channel.
type postWorld struct {
	p     *db.Pool
	rec   *events.Recorder
	perms *perm.Service
	svc   *Service
	ch    uuid.UUID
	// firstMsg is the message of other that was in the channel from the start.
	firstMsg                  uuid.UUID
	member, owner, bot, other *auth.User
}

func newPostWorld(t *testing.T) *postWorld {
	t.Helper()
	p, member := chattest.NewMember(t)
	w := &postWorld{p: p, rec: &events.Recorder{}, perms: perm.NewService(p), member: member}
	w.owner = w.account(t, "the_owner", auth.RoleAdmin, false)
	w.bot = w.account(t, "helper_bot", auth.RoleUser, true)
	w.other = w.account(t, "other_member", auth.RoleUser, false)
	w.ch = chattest.NewChannel(t, p, "general")
	w.firstMsg = chattest.NewMessage(t, p, w.ch, w.other.ID, "written before anyone posts")
	w.svc = &Service{DB: p, Perms: w.perms, Events: w.rec, Online: nil}
	return w
}

func (w *postWorld) account(t *testing.T, name, role string, bot bool) *auth.User {
	t.Helper()
	u := &auth.User{Username: name, DisplayName: name, Role: role}
	err := w.p.QueryRow(context.Background(), `INSERT INTO users (username, display_name, password_hash, role, is_bot) VALUES ($1, $1, 'test-only-unused', $2, $3) RETURNING id`, name, role, bot).Scan(&u.ID)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

// actor names the three kinds of author by the word the tables use.
func (w *postWorld) actor(t *testing.T, who string) Actor {
	t.Helper()
	switch who {
	case "member":
		return MemberActor(w.member)
	case "owner":
		return MemberActor(w.owner)
	case "bot":
		return BotActor(w.bot)
	}
	t.Fatalf("unknown actor %q", who)
	return Actor{}
}

// everyoneLoses takes permissions from @everyone.
func (w *postWorld) everyoneLoses(t *testing.T, lost perm.Set) {
	t.Helper()
	if _, err := w.p.Exec(context.Background(), `UPDATE roles SET permissions = permissions & ~$1::bigint WHERE is_everyone`, int64(lost)); err != nil {
		t.Fatal(err)
	}
	w.perms.Invalidate()
}

func (w *postWorld) exec(t *testing.T, sql string, args ...any) {
	t.Helper()
	if _, err := w.p.Exec(context.Background(), sql, args...); err != nil {
		t.Fatal(err)
	}
}

func (w *postWorld) count(t *testing.T, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := w.p.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func (w *postWorld) messages(t *testing.T) int {
	return w.count(t, `SELECT COUNT(*) FROM messages WHERE channel_id = $1`, w.ch)
}

// created returns the message_create events published so far.
func (w *postWorld) created() []events.Recorded {
	var out []events.Recorded
	for _, e := range w.rec.Snapshot() {
		if e.Type == events.MessageCreate {
			out = append(out, e)
		}
	}
	return out
}

func wantStatus(t *testing.T, err error, status int) *httpx.APIError {
	t.Helper()
	api, ok := httpx.AsAPIError(err)
	if !ok || api.Status != status {
		t.Fatalf("error = %v, want status %d", err, status)
	}
	return api
}

func TestPostStoresTheMessageAndPublishesItOnce(t *testing.T) {
	for _, who := range []string{"member", "owner", "bot"} {
		t.Run(who, func(t *testing.T) {
			w := newPostWorld(t)
			actor := w.actor(t, who)
			posted, err := w.svc.Post(context.Background(), actor, PostCommand{ChannelID: w.ch, Content: "  hello there  "})
			if err != nil {
				t.Fatal(err)
			}
			msg := posted.Message
			if posted.Replayed || msg.UserID != actor.ID || msg.ChannelID != w.ch || msg.Content != "hello there" {
				t.Fatalf("posted = %+v, message = %+v", posted, msg)
			}
			if n := w.count(t, `SELECT COUNT(*) FROM messages WHERE id = $1 AND user_id = $2 AND content = 'hello there'`, msg.ID, actor.ID); n != 1 {
				t.Fatalf("stored rows = %d, want 1", n)
			}
			created := w.created()
			if len(created) != 1 || created[0].ChannelID != w.ch || created[0].Payload.(*core.MessageCreate).ID != msg.ID {
				t.Fatalf("message_create events = %+v, want one for the message in its channel", created)
			}
		})
	}
}

func TestPostRefusesBeforeAnythingIsStoredOrPublished(t *testing.T) {
	elsewhere := func(w *postWorld, t *testing.T) *uuid.UUID {
		id := chattest.NewMessage(t, w.p, chattest.NewChannel(t, w.p, "elsewhere"), w.other.ID, "in another channel")
		return &id
	}
	cases := []struct {
		name   string
		who    string
		setup  func(w *postWorld, t *testing.T, cmd *PostCommand)
		status int
		text   string
	}{
		{"a member without Send messages", "member", func(w *postWorld, t *testing.T, _ *PostCommand) { w.everyoneLoses(t, perm.SendMessages) }, 403, "missing permission: send messages"},
		{"a bot without Send messages", "bot", func(w *postWorld, t *testing.T, _ *PostCommand) { w.everyoneLoses(t, perm.SendMessages) }, 403, "missing permission: send messages"},
		{"a member who cannot see the channel", "member", func(w *postWorld, t *testing.T, _ *PostCommand) { w.everyoneLoses(t, perm.ViewChannel) }, 404, ""},
		{"attachments without Attach files", "member", func(w *postWorld, t *testing.T, cmd *PostCommand) {
			w.everyoneLoses(t, perm.AttachFiles)
			cmd.AttachmentIDs = []uuid.UUID{uuid.New()}
		}, 403, "missing permission: attach files"},
		{"a channel that does not exist", "owner", func(_ *postWorld, _ *testing.T, cmd *PostCommand) { cmd.ChannelID = uuid.New() }, 404, ""},
		{"a member's empty text", "member", func(_ *postWorld, _ *testing.T, cmd *PostCommand) { cmd.Content = "   " }, 400, "content is required"},
		{"a bot's message without text or embed", "bot", func(_ *postWorld, _ *testing.T, cmd *PostCommand) { cmd.Content = "" }, 400, "Cannot send an empty message"},
		{"the same attachment twice", "member", func(_ *postWorld, _ *testing.T, cmd *PostCommand) {
			id := uuid.New()
			cmd.AttachmentIDs = []uuid.UUID{id, id}
		}, 400, ""},
		{"an attachment that was never staged", "member", func(_ *postWorld, _ *testing.T, cmd *PostCommand) { cmd.AttachmentIDs = []uuid.UUID{uuid.New()} }, 400, ""},
		{"a reply to a message of another channel", "member", func(w *postWorld, t *testing.T, cmd *PostCommand) { cmd.ReplyToID = elsewhere(w, t) }, 400, "same conversation"},
		{"a thread under a message of another channel", "member", func(w *postWorld, t *testing.T, cmd *PostCommand) { cmd.ParentID = elsewhere(w, t) }, 400, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := newPostWorld(t)
			cmd := PostCommand{ChannelID: w.ch, Content: "hello"}
			c.setup(w, t, &cmd)
			before := w.count(t, `SELECT COUNT(*) FROM messages`)
			_, err := w.svc.Post(context.Background(), w.actor(t, c.who), cmd)
			api := wantStatus(t, err, c.status)
			if c.text != "" && !strings.Contains(api.Message, c.text) {
				t.Fatalf("message = %q, want it to contain %q", api.Message, c.text)
			}
			if after := w.count(t, `SELECT COUNT(*) FROM messages`); after != before {
				t.Fatalf("messages went from %d to %d", before, after)
			}
			if got := w.created(); len(got) != 0 {
				t.Fatalf("published %+v", got)
			}
		})
	}
}

func TestTheOwnerPostsWhereNobodyElseMay(t *testing.T) {
	w := newPostWorld(t)
	w.everyoneLoses(t, perm.SendMessages|perm.ViewChannel)
	if _, err := w.svc.Post(context.Background(), w.actor(t, "owner"), PostCommand{ChannelID: w.ch, Content: "announcement"}); err != nil {
		t.Fatal(err)
	}
}

func TestABotPostsEmbedsWithoutText(t *testing.T) {
	w := newPostWorld(t)
	cmd := PostCommand{ChannelID: w.ch, Embeds: []InEmbed{{Title: "Build passed"}}}
	posted, err := w.svc.Post(context.Background(), w.actor(t, "bot"), cmd)
	if err != nil {
		t.Fatal(err)
	}
	if got := posted.Message.Embeds; len(got) != 1 || got[0].Title != "Build passed" {
		t.Fatalf("embeds = %+v", got)
	}
	// A member's message has no embeds, whatever the command carries.
	posted, err = w.svc.Post(context.Background(), w.actor(t, "member"), PostCommand{ChannelID: w.ch, Content: "text", Embeds: cmd.Embeds})
	if err != nil || len(posted.Message.Embeds) != 0 {
		t.Fatalf("member embeds = %+v, %v", posted, err)
	}
}

func TestPostAppliesAutomodToMembers(t *testing.T) {
	w := newPostWorld(t)
	ctx := context.Background()
	engine := automod.New(w.p, w.perms, w.rec, &audit.Log{DB: w.p})
	w.svc.Automod = engine // handed in, not found through the pool
	in, err := automod.RuleInput{Kind: automod.KindWords, Name: "words", Enabled: true, Action: automod.ActionBlock, Config: automod.Settings{Words: []string{"badword"}}}.Normalize()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := automod.CreateRule(ctx, w.p, in); err != nil {
		t.Fatal(err)
	}
	engine.Invalidate()

	before := w.messages(t)
	_, err = w.svc.Post(ctx, w.actor(t, "member"), PostCommand{ChannelID: w.ch, Content: "what a BADWORD"})
	wantStatus(t, err, 400)
	if w.messages(t) != before || len(w.created()) != 0 {
		t.Fatal("a blocked message was stored or published")
	}
	// Post asks the engine for everyone; the engine itself lets the Owner through.
	if _, err := w.svc.Post(ctx, w.actor(t, "owner"), PostCommand{ChannelID: w.ch, Content: "what a BADWORD"}); err != nil {
		t.Fatalf("owner: %v", err)
	}
}

func TestSlowModeHoldsBackWhoeverMayNotManageMessages(t *testing.T) {
	cases := []struct {
		who      string
		heldBack bool
	}{
		{who: "member", heldBack: true},
		{who: "bot", heldBack: true},
		{who: "owner", heldBack: false},
	}
	for _, c := range cases {
		t.Run(c.who, func(t *testing.T) {
			w := newPostWorld(t)
			ctx := context.Background()
			w.exec(t, `UPDATE channels SET slow_mode_seconds = 60 WHERE id = $1`, w.ch)
			actor := w.actor(t, c.who)
			if _, err := w.svc.Post(ctx, actor, PostCommand{ChannelID: w.ch, Content: "first"}); err != nil {
				t.Fatal(err)
			}
			_, err := w.svc.Post(ctx, actor, PostCommand{ChannelID: w.ch, Content: "second"})
			if !c.heldBack {
				if err != nil {
					t.Fatalf("second post: %v", err)
				}
				return
			}
			if api := wantStatus(t, err, 429); api.RetryAfter <= 0 {
				t.Fatalf("no wait in %+v", api)
			}
			if got := w.created(); len(got) != 1 {
				t.Fatalf("published %d messages, want the first only", len(got))
			}
		})
	}
}

func TestPostMovesTheReadMarkerUnlessTheAuthorIsNotReading(t *testing.T) {
	for _, keep := range []bool{false, true} {
		w := newPostWorld(t)
		// The message of other is unread for member.
		posted, err := w.svc.Post(context.Background(), w.actor(t, "member"), PostCommand{ChannelID: w.ch, Content: "mine", KeepReadMarker: keep})
		if err != nil {
			t.Fatal(err)
		}
		marked := w.count(t, `SELECT COUNT(*) FROM channel_reads WHERE user_id = $1 AND channel_id = $2 AND last_read_at = $3`, w.member.ID, w.ch, posted.Message.CreatedAt)
		told := 0
		for _, e := range w.rec.Snapshot() {
			if e.Type == events.ReadState && slices.Equal(e.Recipients, []uuid.UUID{w.member.ID}) {
				told++
			}
		}
		if want := map[bool]int{false: 1, true: 0}[keep]; marked != want || told != want {
			t.Fatalf("KeepReadMarker=%v: marker rows %d, refresh events %d, want %d each", keep, marked, told, want)
		}
	}
}

// txOutbox is an outbox that notes whether the message it is asked to record
// is visible inside the transaction it gets and outside of it.
type txOutbox struct {
	p                *db.Pool
	fail             error
	recorded         []events.Event
	seenInTx, seenBy bool
}

func (o *txOutbox) Wants(uuid.UUID, string) bool { return true }

func (o *txOutbox) Record(ctx context.Context, tx pgx.Tx, _ uuid.UUID, ev events.Event) error {
	id := ev.(*core.MessageCreate).ID
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM messages WHERE id = $1)`, id).Scan(&o.seenInTx); err != nil {
		return err
	}
	if err := o.p.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM messages WHERE id = $1)`, id).Scan(&o.seenBy); err != nil {
		return err
	}
	o.recorded = append(o.recorded, ev)
	return o.fail
}

func TestPostRecordsTheWebhookEventInTheTransactionOfTheInsert(t *testing.T) {
	w := newPostWorld(t)
	out := &txOutbox{p: w.p}
	core.AttachOutbox(w.p, out)
	t.Cleanup(func() { core.DetachOutbox(w.p, out) })

	posted, err := w.svc.Post(context.Background(), w.actor(t, "member"), PostCommand{ChannelID: w.ch, Content: "for the webhook"})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.recorded) != 1 || out.recorded[0].(*core.MessageCreate).ID != posted.Message.ID {
		t.Fatalf("recorded %+v, want the posted message once", out.recorded)
	}
	if !out.seenInTx || out.seenBy {
		t.Fatalf("message visible in the transaction: %v, outside of it: %v; want true and false", out.seenInTx, out.seenBy)
	}
	// The event that was recorded is the one that is published.
	if got := w.created(); len(got) != 1 || got[0].Payload.(*core.MessageCreate) != out.recorded[0].(*core.MessageCreate) {
		t.Fatalf("published %+v", got)
	}
}

func TestAnOutboxThatFailsUndoesTheMessage(t *testing.T) {
	w := newPostWorld(t)
	out := &txOutbox{p: w.p, fail: errors.New("outbox is full")}
	core.AttachOutbox(w.p, out)
	t.Cleanup(func() { core.DetachOutbox(w.p, out) })

	before := w.messages(t)
	if _, err := w.svc.Post(context.Background(), w.actor(t, "member"), PostCommand{ChannelID: w.ch, Content: "lost"}); !errors.Is(err, out.fail) {
		t.Fatalf("error = %v, want the outbox failure", err)
	}
	if w.messages(t) != before || len(w.created()) != 0 {
		t.Fatal("the message was stored or published although its webhook event was not recorded")
	}
}

func TestTheCallersStepRunsInTheTransactionOfTheInsert(t *testing.T) {
	w := newPostWorld(t)
	superseded := errors.New("superseded")
	before := w.messages(t)
	_, err := w.svc.Post(context.Background(), w.actor(t, "member"), PostCommand{
		ChannelID: w.ch, Content: "scheduled", InTx: func(pgx.Tx, uuid.UUID) error { return superseded },
	})
	if !errors.Is(err, superseded) || w.messages(t) != before || len(w.created()) != 0 {
		t.Fatalf("error = %v, messages %d -> %d, published %d", err, before, w.messages(t), len(w.created()))
	}
}

func TestCheckedContentIsStoredAsItIs(t *testing.T) {
	w := newPostWorld(t)
	posted, err := w.svc.Post(context.Background(), w.actor(t, "member"), PostCommand{ChannelID: w.ch, Content: " kept as stored ", ContentChecked: true})
	if err != nil || posted.Message.Content != " kept as stored " {
		t.Fatalf("posted = %+v, %v", posted, err)
	}
}

func TestASecondPostWithTheSameNonceReturnsTheFirstMessage(t *testing.T) {
	w := newPostWorld(t)
	ctx := context.Background()
	nonce := uuid.New()
	cmd := PostCommand{ChannelID: w.ch, Content: "once", Nonce: &nonce}
	first, err := w.svc.Post(ctx, w.actor(t, "member"), cmd)
	if err != nil || first.Replayed || first.Message.Nonce == nil || *first.Message.Nonce != nonce {
		t.Fatalf("first = %+v, %v", first, err)
	}
	// The retry is answered even after the member lost Send messages.
	w.everyoneLoses(t, perm.SendMessages)
	again, err := w.svc.Post(ctx, w.actor(t, "member"), cmd)
	if err != nil || !again.Replayed || again.Message.ID != first.Message.ID {
		t.Fatalf("retry = %+v, %v", again, err)
	}
	if got := w.created(); len(got) != 1 {
		t.Fatalf("published %d times, want once", len(got))
	}
	// Not to somebody who can no longer see the channel.
	w.everyoneLoses(t, perm.ViewChannel)
	_, err = w.svc.Post(ctx, w.actor(t, "member"), cmd)
	wantStatus(t, err, 404)
}

func TestMentionsFollowTheKindOfAuthor(t *testing.T) {
	everyone := &AllowedMentions{Parse: []string{"everyone"}}
	cases := []struct {
		name     string
		who      string
		content  string
		allowed  *AllowedMentions
		notifies []string
	}{
		{"a member names a member", "member", "hi @other_member", nil, []string{"other"}},
		{"the Owner mentions everyone", "owner", "@all hear this", nil, []string{"member", "other"}},
		{"a bot names a member", "bot", "hi @other_member", nil, []string{"other"}},
		{"a bot's @all notifies nobody unless it asks", "bot", "@all hear this", nil, nil},
		{"allowed_mentions without users silences a name", "bot", "hi @other_member", everyone, nil},
		// A member's mentions come from their permissions; the field is the bot API's.
		{"allowed_mentions does nothing for a member", "member", "hi @other_member", everyone, []string{"other"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := newPostWorld(t)
			ids := map[string]uuid.UUID{"member": w.member.ID, "other": w.other.ID}
			posted, err := w.svc.Post(context.Background(), w.actor(t, c.who), PostCommand{ChannelID: w.ch, Content: c.content, AllowedMentions: c.allowed})
			if err != nil {
				t.Fatal(err)
			}
			var want []uuid.UUID
			for _, name := range c.notifies {
				want = append(want, ids[name])
			}
			got := slices.Clone(posted.Message.Mentions)
			sortIDs(got)
			sortIDs(want)
			if !slices.Equal(got, want) {
				t.Fatalf("mentions = %v, want %v (%v)", got, want, c.notifies)
			}
		})
	}
}

func sortIDs(ids []uuid.UUID) {
	slices.SortFunc(ids, func(a, b uuid.UUID) int { return slices.Compare(a[:], b[:]) })
}

func TestRefusalNamesTheGateWithoutPosting(t *testing.T) {
	w := newPostWorld(t)
	ctx := context.Background()
	if err := w.svc.Refusal(ctx, w.actor(t, "bot"), w.ch); err != nil {
		t.Fatal(err)
	}
	wantStatus(t, w.svc.Refusal(ctx, w.actor(t, "bot"), uuid.New()), 404)
	w.everyoneLoses(t, perm.SendMessages)
	wantStatus(t, w.svc.Refusal(ctx, w.actor(t, "bot"), w.ch), 403)
}
