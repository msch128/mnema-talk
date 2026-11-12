//go:build integration

package chat

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/chat/channels"
	"github.com/msch128/mnema-talk/internal/chat/chattest"
	"github.com/msch128/mnema-talk/internal/chat/core"
	"github.com/msch128/mnema-talk/internal/chat/mentions"
	"github.com/msch128/mnema-talk/internal/chat/messages"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/perm"
)

// A rejecting trigger models an actual database write failure. Its lifetime
// is confined to this package's throwaway fixture database and every case
// checks that the transaction preserved the existing rows.
func rejectChatWrite(t *testing.T, p *db.Pool, table, operation string) {
	t.Helper()
	ctx := context.Background()
	name := "chat_reject_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := p.Exec(ctx, "CREATE FUNCTION "+name+`() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'test write rejected'; END $$`); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Exec(ctx, "CREATE TRIGGER "+name+" BEFORE "+operation+" ON "+table+" FOR EACH ROW EXECUTE FUNCTION "+name+"()"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := p.Exec(ctx, "DROP TRIGGER "+name+" ON "+table); err != nil {
			t.Error(err)
		}
		if _, err := p.Exec(ctx, "DROP FUNCTION "+name+"()"); err != nil {
			t.Error(err)
		}
	})
}

// byName is a mention set that notifies ids by name only.
func byName(ids []uuid.UUID) mentions.MentionSet {
	users := make([]mentions.MentionedUser, len(ids))
	for i, id := range ids {
		users[i] = mentions.MentionedUser{ID: id, Kind: core.MentionKindName}
	}
	return mentions.MentionSet{Users: users}
}

func TestChatTransactionalWriteFailuresRollback(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name, table, operation string
		run                    func(chatFixture) error
	}{
		{"insert message", "messages", "INSERT", func(f chatFixture) error {
			_, err := messages.InsertMessage(ctx, f.p, nil, messages.NewMessage{ChannelID: f.c.ID, UserID: f.u.ID, Content: "new"}, nil)
			return err
		}},
		{"insert mentions", "message_mentions", "INSERT", func(f chatFixture) error { return mentions.SaveMentionSet(ctx, f.p, f.m, byName([]uuid.UUID{f.u.ID})) }},
		{"record mentions during message insert", "message_mentions", "DELETE", func(f chatFixture) error {
			_, err := messages.InsertMessage(ctx, f.p, nil, messages.NewMessage{ChannelID: f.c.ID, UserID: f.u.ID, Content: "@all"}, nil)
			return err
		}},
		{"edit message", "messages", "UPDATE", func(f chatFixture) error { return messages.EditMessage(ctx, f.p, nil, f.m, f.u.ID, "new") }},
		{"delete message", "messages", "DELETE", func(f chatFixture) error {
			_, err := messages.DeleteMessageAs(ctx, f.p, f.m, f.u, f.u.IsAdmin())
			return err
		}},
		{"delete channel", "channels", "DELETE", func(f chatFixture) error { _, err := channels.DeleteChannel(ctx, f.p, f.c.ID); return err }},
		{"layout category", "categories", "UPDATE", func(f chatFixture) error {
			cat, err := channels.CreateCategory(ctx, f.p, "cat", 0)
			if err != nil {
				return err
			}
			return channels.ApplyLayout(ctx, f.p, []channels.CategoryOrder{{ID: cat.ID, SortOrder: 3}}, nil)
		}},
		{"layout channel", "channels", "UPDATE", func(f chatFixture) error {
			return channels.ApplyLayout(ctx, f.p, nil, []channels.ChannelPlacement{{ID: f.c.ID, SortOrder: 2}})
		}},
		{"duplicate shift", "channels", "UPDATE", func(f chatFixture) error {
			_, err := channels.CreateChannelWithStage(ctx, f.p, nil, "follower", core.ChannelTypeText, "", 1, false)
			if err != nil {
				return err
			}
			_, err = channels.DuplicateChannel(ctx, f.p, f.c.ID)
			return err
		}},
		{"duplicate insert", "channels", "INSERT", func(f chatFixture) error { _, err := channels.DuplicateChannel(ctx, f.p, f.c.ID); return err }},
		{"add reaction", "message_reactions", "INSERT", func(f chatFixture) error {
			_, err := messages.ToggleReactionAs(ctx, f.p, f.m, f.u.ID, "ok", true)
			return err
		}},
		{"remove reaction", "message_reactions", "DELETE", func(f chatFixture) error {
			if _, err := f.p.Exec(ctx, `INSERT INTO message_reactions(message_id,user_id,emoji)VALUES($1,$2,'ok')`, f.m, f.u.ID); err != nil {
				return err
			}
			_, err := messages.ToggleReactionAs(ctx, f.p, f.m, f.u.ID, "ok", true)
			return err
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newChatFixture(t)
			// DELETE triggers fire only when at least one mention already exists.
			if c.table == "message_mentions" && c.operation == "DELETE" {
				if err := mentions.SaveMentionSet(ctx, f.p, f.m, byName([]uuid.UUID{f.u.ID})); err != nil {
					t.Fatal(err)
				}
				// The new message has no existing mentions: a statement trigger is
				// needed to model a failing DELETE irrespective of row count.
				if _, err := f.p.Exec(ctx, `CREATE FUNCTION chat_fail_clear() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'test write rejected'; END $$`); err != nil {
					t.Fatal(err)
				}
				if _, err := f.p.Exec(ctx, `CREATE TRIGGER chat_fail_clear BEFORE DELETE ON message_mentions FOR EACH STATEMENT EXECUTE FUNCTION chat_fail_clear()`); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					_, _ = f.p.Exec(ctx, `DROP TRIGGER chat_fail_clear ON message_mentions; DROP FUNCTION chat_fail_clear()`)
				})
			} else {
				rejectChatWrite(t, f.p, c.table, c.operation)
			}
			chattest.RequireError(t, c.run(f), "test write rejected")
			msg, err := messages.GetMessage(ctx, f.p, f.m)
			if err != nil || msg.Content != "hello world" {
				t.Fatalf("rollback changed original: %v, %v", msg, err)
			}
		})
	}
	t.Run("attachment callback failure", func(t *testing.T) {
		f := newChatFixture(t)
		sentinel := errors.New("test attachment insert failed")
		id, err := messages.InsertMessage(ctx, f.p, nil, messages.NewMessage{ChannelID: f.c.ID, UserID: f.u.ID, Content: "upload"}, func(tx pgx.Tx, id uuid.UUID) error { return sentinel })
		if id != uuid.Nil || !errors.Is(err, sentinel) {
			t.Fatalf("insert result = %s, %v", id, err)
		}
		var count int
		if err := f.p.QueryRow(ctx, `SELECT COUNT(*) FROM messages`).Scan(&count); err != nil || count != 1 {
			t.Fatalf("failed upload committed a message: %d,%v", count, err)
		}
	})
}

func TestChatLockFailures(t *testing.T) {
	f := newChatFixture(t)
	ctx := context.Background()
	p := chattest.DriftPool(t, f.p, map[string]string{"channels": `SELECT number::int id FROM public.channels`, "messages": `SELECT id FROM public.messages`})
	for _, run := range []func() error{
		func() error { _, e := channels.DeleteChannel(ctx, p, f.c.ID); return e },
		func() error { _, e := channels.DuplicateChannel(ctx, p, f.c.ID); return e },
		func() error { return messages.EditMessage(ctx, p, nil, f.m, f.u.ID, "new") },
		func() error { _, e := messages.DeleteMessageAs(ctx, p, f.m, f.u, f.u.IsAdmin()); return e },
	} {
		if err := run(); err == nil {
			t.Fatal("incompatible locked row accepted")
		}
	}
	// Hold the real advisory lock in one transaction and set a database
	// timeout on another pool. A blocked layout must fail without changes.
	tx, err := f.p.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if err := channels.LockChannelLayout(ctx, tx); err != nil {
		t.Fatal(err)
	}
	cfg := f.p.Config().Copy()
	cfg.ConnConfig.RuntimeParams["statement_timeout"] = "20ms"
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	blocked := &db.Pool{Pool: pool}
	chattest.RequireError(t, channels.ApplyLayout(ctx, blocked, nil, nil), "lock channel layout")
	_, err = channels.DuplicateChannel(ctx, blocked, f.c.ID)
	chattest.RequireError(t, err, "lock channel layout")
}

// cancelChatQuery uses pgx's instrumentation seam to model a client dropping
// a request after an earlier statement has succeeded. Every SQL statement
// still runs through pgx and PostgreSQL; cancellation is the only injected
// condition, and the tracer is installed only on a test-owned pool.
type cancelChatQuery struct {
	cancel context.CancelFunc
	match  func(string) bool
	end    bool
	once   sync.Once
}

func (c *cancelChatQuery) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if c.match(data.SQL) {
		if c.end {
			return context.WithValue(ctx, chatQueryMatchKey{}, true)
		}
		c.once.Do(c.cancel)
	}
	return ctx
}

func (c *cancelChatQuery) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryEndData) {
	if c.end && ctx.Value(chatQueryMatchKey{}) == true {
		c.once.Do(c.cancel)
	}
}

type chatQueryMatchKey struct{}

func cancelQueryPool(t *testing.T, p *db.Pool, match func(string) bool, end bool) (*db.Pool, context.Context) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	cfg := p.Config().Copy()
	cfg.ConnConfig.Tracer = &cancelChatQuery{cancel: cancel, match: match, end: end}
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return &db.Pool{Pool: pool}, ctx
}

func TestChatCancellationBetweenDatabaseStages(t *testing.T) {
	f := newChatFixture(t)
	t.Run("duplicate group", func(t *testing.T) {
		p, ctx := cancelQueryPool(t, f.p, func(sql string) bool { return strings.Contains(sql, "WHERE category_id IS NOT DISTINCT") }, false)
		_, err := channels.DuplicateChannel(ctx, p, f.c.ID)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("group lock cancellation=%v", err)
		}
	})
	t.Run("duplicate permissions", func(t *testing.T) {
		p, ctx := cancelQueryPool(t, f.p, func(sql string) bool { return strings.Contains(sql, "INSERT INTO permission_overwrites") }, false)
		_, err := channels.DuplicateChannel(ctx, p, f.c.ID)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("permission copy cancellation=%v", err)
		}
	})
	t.Run("around newer half", func(t *testing.T) {
		p, ctx := cancelQueryPool(t, f.p, func(sql string) bool { return strings.Contains(sql, "AND (m.created_at, m.id) >") }, false)
		_, err := messages.GetChannelMessages(ctx, p, f.c.ID, core.HistoryQuery{Limit: 10, Around: &f.m})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("newer half cancellation=%v", err)
		}
	})
	t.Run("reaction count", func(t *testing.T) {
		p, ctx := cancelQueryPool(t, f.p, func(sql string) bool { return strings.Contains(sql, ") FILTER (WHERE user_id = $2)") }, false)
		_, err := messages.ToggleReactionAs(ctx, p, f.m, f.u.ID, "ok", true)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("reaction count cancellation=%v", err)
		}
	})
	t.Run("thread reply page", func(t *testing.T) {
		p, ctx := cancelQueryPool(t, f.p, func(sql string) bool { return strings.Contains(sql, "WHERE m.parent_id = $1") }, false)
		h := &Handler{DB: p, Perms: perm.NewService(f.p), Events: &events.Recorder{}}
		r := chattest.Request(f.u, "", map[string]string{"messageID": f.m.String()}, "")
		r = r.WithContext(auth.WithUser(copyChatRouteContext(ctx, r), f.u))
		if err := h.messages().GetThread(httptest.NewRecorder(), r); !errors.Is(err, context.Canceled) {
			t.Fatalf("thread page cancellation=%v", err)
		}
	})
	// The edited message is read in the edit's transaction (H-L7-05), so a
	// client that leaves right after the commit cannot turn a made change
	// into an error answer: the edit answers and is published.
	t.Run("cancellation after committed edit", func(t *testing.T) {
		p, ctx := cancelQueryPool(t, f.p, func(sql string) bool { return sql == "commit" }, true)
		recorder := &events.Recorder{}
		h := &Handler{DB: p, Perms: perm.NewService(f.p), Events: recorder}
		r := chattest.Request(f.u, `{"content":"edited before disconnect"}`, map[string]string{"channelID": f.c.ID.String(), "messageID": f.m.String()}, "")
		r = r.WithContext(auth.WithUser(copyChatRouteContext(ctx, r), f.u))
		if err := h.messages().ServeEditMessage(httptest.NewRecorder(), r); err != nil {
			t.Fatalf("committed edit answered %v", err)
		}
		msg, err := messages.GetMessage(context.Background(), f.p, f.m)
		if err != nil || msg.Content != "edited before disconnect" {
			t.Fatalf("committed edit lost: %v,%v", msg, err)
		}
		if len(recorder.Snapshot()) != 1 {
			t.Fatalf("published %d events for the committed edit, want its update", len(recorder.Snapshot()))
		}
	})
}

func copyChatRouteContext(ctx context.Context, r *http.Request) context.Context {
	return context.WithValue(ctx, chi.RouteCtxKey, chi.RouteContext(r.Context()))
}

func TestChatHandlerInsertFailureDoesNotPublish(t *testing.T) {
	f := newChatFixture(t)
	rejectChatWrite(t, f.p, "messages", "INSERT")
	recorder := &events.Recorder{}
	h := &Handler{DB: f.p, Perms: perm.NewService(f.p), Events: recorder}
	err := h.messages().ServeCreateMessage(httptest.NewRecorder(), chattest.Request(f.u, `{"content":"new"}`, map[string]string{"channelID": f.c.ID.String()}, ""))
	chattest.RequireError(t, err, "test write rejected")
	if len(recorder.Snapshot()) != 0 {
		t.Fatal("failed message creation published an event")
	}
}
