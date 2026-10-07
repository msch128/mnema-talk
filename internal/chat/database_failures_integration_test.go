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
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/events"
)

// driftPool models a database with an incompatible relation in its schema.
// It uses real PostgreSQL views so both SQL errors and pgx decoding errors
// travel through the same protocol as production, without replacing pgx.
func driftPool(t *testing.T, p *db.Pool, views map[string]string) *db.Pool {
	t.Helper()
	schema := "chat_drift_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := p.Exec(context.Background(), "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := p.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE"); err != nil {
			t.Error(err)
		}
	})
	for relation, selectSQL := range views {
		if _, err := p.Exec(context.Background(), "CREATE VIEW "+schema+"."+relation+" AS "+selectSQL); err != nil {
			t.Fatal(err)
		}
	}
	cfg := p.Config().Copy()
	cfg.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return &db.Pool{Pool: pool}
}

func TestChatSchemaDriftReportsDecodeAndQueryFailures(t *testing.T) {
	f := newChatFixture(t)
	ctx := context.Background()
	t.Run("category decoding", func(t *testing.T) {
		p := driftPool(t, f.p, map[string]string{"categories": `SELECT gen_random_uuid() id, NULL::text name, 0 sort_order, NOW() created_at`})
		_, _, err := GetServerHierarchy(ctx, p)
		requireChatError(t, err, "NULL")
	})
	t.Run("category stream error", func(t *testing.T) {
		if _, err := CreateCategory(ctx, f.p, "error-stream", 0); err != nil {
			t.Fatal(err)
		}
		p := driftPool(t, f.p, map[string]string{"categories": `SELECT id,name,1/(sort_order-sort_order) sort_order,created_at FROM public.categories`})
		_, _, err := GetServerHierarchy(ctx, p)
		requireChatError(t, err, "division by zero")
	})
	t.Run("channel query", func(t *testing.T) {
		p := driftPool(t, f.p, map[string]string{"channels": `SELECT id FROM public.channels`})
		_, _, err := GetServerHierarchy(ctx, p)
		requireChatError(t, err, "query channels")
	})
	t.Run("channel decoding", func(t *testing.T) {
		p := driftPool(t, f.p, map[string]string{"channels": `SELECT id,number,category_id,NULL::text name,type,topic,sort_order,created_at,user_limit FROM public.channels`})
		_, _, err := GetServerHierarchy(ctx, p)
		requireChatError(t, err, "NULL")
	})
	t.Run("member decoding", func(t *testing.T) {
		p := driftPool(t, f.p, map[string]string{"users": `SELECT id,username,NULL::text display_name,bio,role,avatar_s3_key,status_text,created_at,voice_seconds,message_count,disabled_at FROM public.users`})
		_, err := GetAllMembers(ctx, p)
		requireChatError(t, err, "NULL")
	})
	t.Run("mention stream", func(t *testing.T) {
		p := driftPool(t, f.p, map[string]string{"users": `SELECT CASE WHEN voice_seconds >= 0 THEN (1/(voice_seconds-voice_seconds))::text::uuid ELSE id END id,username,disabled_at FROM public.users`})
		_, err := ResolveMentions(ctx, p, "@all", f.u.ID, nil)
		requireChatError(t, err, "resolve mentions")
	})
	t.Run("message decoding", func(t *testing.T) {
		p := driftPool(t, f.p, map[string]string{"messages": `SELECT id,number,channel_id,user_id,parent_id,NULL::text content,is_pinned,is_edited,created_at,updated_at,reply_to_id FROM public.messages`})
		if _, err := GetMessage(ctx, p, f.m); err == nil {
			t.Fatal("null content accepted")
		}
		if _, _, err := Search(ctx, p, SearchQuery{ChannelID: &f.c.ID, Limit: 10}); err == nil {
			t.Fatal("null search content accepted")
		}
	})
	t.Run("attachments query", func(t *testing.T) {
		p := driftPool(t, f.p, map[string]string{"media": `SELECT id,message_id FROM public.media`})
		msgs := []Message{{ID: f.m}}
		requireChatError(t, enrich(ctx, p, msgs), "load attachments")
		_, _, err := Search(ctx, p, SearchQuery{Text: "hello", Limit: 10})
		requireChatError(t, err, "load attachments")
	})
	t.Run("attachment decoding", func(t *testing.T) {
		p := driftPool(t, f.p, map[string]string{"media": `SELECT gen_random_uuid() id,id message_id,NULL::text original_filename,'image/png'::text mime_type,1::bigint size_bytes,false is_deleted,NOW() created_at FROM public.messages`})
		requireChatError(t, enrich(ctx, p, []Message{{ID: f.m}}), "NULL")
	})
	t.Run("attachment stream", func(t *testing.T) {
		p := driftPool(t, f.p, map[string]string{"media": `SELECT gen_random_uuid() id,id message_id,'image.png'::text original_filename,'image/png'::text mime_type,1/(number-number) size_bytes,false is_deleted,NOW() created_at FROM public.messages`})
		requireChatError(t, enrich(ctx, p, []Message{{ID: f.m}}), "division by zero")
	})
	t.Run("reaction query", func(t *testing.T) {
		p := driftPool(t, f.p, map[string]string{"message_reactions": `SELECT id FROM public.messages`})
		requireChatError(t, enrich(ctx, p, []Message{{ID: f.m}}), "load reactions")
	})
	t.Run("reaction decoding", func(t *testing.T) {
		p := driftPool(t, f.p, map[string]string{"message_reactions": `SELECT id message_id,user_id,NULL::text emoji,created_at FROM public.messages`})
		requireChatError(t, enrich(ctx, p, []Message{{ID: f.m}}), "NULL")
		_, err := reactionSummary(ctx, p, f.m)
		requireChatError(t, err, "NULL")
	})
	t.Run("reaction stream", func(t *testing.T) {
		p := driftPool(t, f.p, map[string]string{"message_reactions": `SELECT id message_id,user_id,(1/(number-number))::text emoji,created_at FROM public.messages`})
		requireChatError(t, enrich(ctx, p, []Message{{ID: f.m}}), "division by zero")
	})
	t.Run("mention query", func(t *testing.T) {
		p := driftPool(t, f.p, map[string]string{"message_mentions": `SELECT id FROM public.messages`})
		requireChatError(t, enrich(ctx, p, []Message{{ID: f.m}}), "load mentions")
	})
	t.Run("mention decoding", func(t *testing.T) {
		p := driftPool(t, f.p, map[string]string{"message_mentions": `SELECT id message_id,'invalid-uuid'::text user_id FROM public.messages`})
		if err := enrich(ctx, p, []Message{{ID: f.m}}); err == nil {
			t.Fatal("invalid mention UUID accepted")
		}
	})
	t.Run("collect key decoding", func(t *testing.T) {
		var keys []string
		requireChatError(t, collectKeys(ctx, f.p, &keys, "SELECT NULL::text"), "collect media keys")
	})
	t.Run("channel group query", func(t *testing.T) {
		p := driftPool(t, f.p, map[string]string{"channels": `SELECT id FROM public.channels`})
		tx, err := p.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(ctx)
		_, _, err = lockChannelGroup(ctx, tx, nil)
		requireChatError(t, err, "lock channel group")
	})
	t.Run("channel group decoding", func(t *testing.T) {
		p := driftPool(t, f.p, map[string]string{"channels": `SELECT id,category_id,NULL::int sort_order,created_at FROM public.channels`})
		tx, err := p.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(ctx)
		_, _, err = lockChannelGroup(ctx, tx, nil)
		requireChatError(t, err, "NULL")
	})
}

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

func TestChatTransactionalWriteFailuresRollback(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name, table, operation string
		run                    func(chatFixture) error
	}{
		{"insert message", "messages", "INSERT", func(f chatFixture) error {
			_, err := InsertMessage(ctx, f.p, nil, NewMessage{ChannelID: f.c.ID, UserID: f.u.ID, Content: "new"}, nil)
			return err
		}},
		{"insert mentions", "message_mentions", "INSERT", func(f chatFixture) error { return SaveMentions(ctx, f.p, f.m, []uuid.UUID{f.u.ID}) }},
		{"record mentions during message insert", "message_mentions", "DELETE", func(f chatFixture) error {
			_, err := InsertMessage(ctx, f.p, nil, NewMessage{ChannelID: f.c.ID, UserID: f.u.ID, Content: "@all"}, nil)
			return err
		}},
		{"edit message", "messages", "UPDATE", func(f chatFixture) error { return EditMessage(ctx, f.p, nil, f.m, f.u.ID, "new") }},
		{"delete message", "messages", "DELETE", func(f chatFixture) error { _, err := DeleteMessage(ctx, f.p, f.m, f.u); return err }},
		{"delete channel", "channels", "DELETE", func(f chatFixture) error { _, err := DeleteChannel(ctx, f.p, f.c.ID); return err }},
		{"layout category", "categories", "UPDATE", func(f chatFixture) error {
			cat, err := CreateCategory(ctx, f.p, "cat", 0)
			if err != nil {
				return err
			}
			return ApplyLayout(ctx, f.p, []CategoryOrder{{ID: cat.ID, SortOrder: 3}}, nil)
		}},
		{"layout channel", "channels", "UPDATE", func(f chatFixture) error {
			return ApplyLayout(ctx, f.p, nil, []ChannelPlacement{{ID: f.c.ID, SortOrder: 2}})
		}},
		{"duplicate shift", "channels", "UPDATE", func(f chatFixture) error {
			_, err := CreateChannel(ctx, f.p, nil, "follower", ChannelTypeText, "", 1)
			if err != nil {
				return err
			}
			_, err = DuplicateChannel(ctx, f.p, f.c.ID)
			return err
		}},
		{"duplicate insert", "channels", "INSERT", func(f chatFixture) error { _, err := DuplicateChannel(ctx, f.p, f.c.ID); return err }},
		{"add reaction", "message_reactions", "INSERT", func(f chatFixture) error { _, err := ToggleReaction(ctx, f.p, f.m, f.u.ID, "ok"); return err }},
		{"remove reaction", "message_reactions", "DELETE", func(f chatFixture) error {
			if _, err := f.p.Exec(ctx, `INSERT INTO message_reactions(message_id,user_id,emoji)VALUES($1,$2,'ok')`, f.m, f.u.ID); err != nil {
				return err
			}
			_, err := ToggleReaction(ctx, f.p, f.m, f.u.ID, "ok")
			return err
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newChatFixture(t)
			// DELETE triggers fire only when at least one mention already exists.
			if c.table == "message_mentions" && c.operation == "DELETE" {
				if err := SaveMentions(ctx, f.p, f.m, []uuid.UUID{f.u.ID}); err != nil {
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
			requireChatError(t, c.run(f), "test write rejected")
			msg, err := GetMessage(ctx, f.p, f.m)
			if err != nil || msg.Content != "hello world" {
				t.Fatalf("rollback changed original: %v, %v", msg, err)
			}
		})
	}
	t.Run("attachment callback failure", func(t *testing.T) {
		f := newChatFixture(t)
		sentinel := errors.New("test attachment insert failed")
		id, err := InsertMessage(ctx, f.p, nil, NewMessage{ChannelID: f.c.ID, UserID: f.u.ID, Content: "upload"}, func(tx pgx.Tx, id uuid.UUID) error { return sentinel })
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
	p := driftPool(t, f.p, map[string]string{"channels": `SELECT number::int id FROM public.channels`, "messages": `SELECT id FROM public.messages`})
	for _, run := range []func() error{
		func() error { _, e := DeleteChannel(ctx, p, f.c.ID); return e },
		func() error { _, e := DuplicateChannel(ctx, p, f.c.ID); return e },
		func() error { return EditMessage(ctx, p, nil, f.m, f.u.ID, "new") },
		func() error { _, e := DeleteMessage(ctx, p, f.m, f.u); return e },
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
	if err := lockChannelLayout(ctx, tx); err != nil {
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
	requireChatError(t, ApplyLayout(ctx, blocked, nil, nil), "lock channel layout")
	_, err = DuplicateChannel(ctx, blocked, f.c.ID)
	requireChatError(t, err, "lock channel layout")
}

func TestChatMissingRelationsFailTransactions(t *testing.T) {
	f := newChatFixture(t)
	ctx := context.Background()
	t.Run("collect media keys", func(t *testing.T) {
		p := driftPool(t, f.p, map[string]string{"media": `SELECT id FROM public.media`})
		_, err := DeleteMessage(ctx, p, f.m, f.u)
		requireChatError(t, err, "collect media keys")
		_, err = DeleteChannel(ctx, p, f.c.ID)
		requireChatError(t, err, "collect media keys")
	})
	t.Run("count reaction query", func(t *testing.T) {
		// This updatable view still enforces the table's real foreign key.
		p := driftPool(t, f.p, map[string]string{"message_reactions": `SELECT message_id,user_id,emoji FROM public.message_reactions`})
		// Adding to this simple view reaches the real table and its FK.
		_, err := ToggleReaction(ctx, p, f.m, uuid.New(), "ok")
		requireChatError(t, err, "add reaction")
	})
	t.Run("reaction lock", func(t *testing.T) {
		p := driftPool(t, f.p, map[string]string{"messages": `SELECT number::int id FROM public.messages`})
		_, err := ToggleReaction(ctx, p, f.m, f.u.ID, "ok")
		requireChatError(t, err, "lock message")
	})
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
		_, err := DuplicateChannel(ctx, p, f.c.ID)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("group lock cancellation=%v", err)
		}
	})
	t.Run("around newer half", func(t *testing.T) {
		p, ctx := cancelQueryPool(t, f.p, func(sql string) bool { return strings.Contains(sql, "AND (m.created_at, m.id) >") }, false)
		_, err := GetChannelMessages(ctx, p, f.c.ID, HistoryQuery{Limit: 10, Around: &f.m})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("newer half cancellation=%v", err)
		}
	})
	t.Run("reaction count", func(t *testing.T) {
		p, ctx := cancelQueryPool(t, f.p, func(sql string) bool { return strings.Contains(sql, "COUNT(*) FILTER (WHERE user_id") }, false)
		_, err := ToggleReaction(ctx, p, f.m, f.u.ID, "ok")
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("reaction count cancellation=%v", err)
		}
	})
	t.Run("thread reply page", func(t *testing.T) {
		p, ctx := cancelQueryPool(t, f.p, func(sql string) bool { return strings.Contains(sql, "WHERE m.parent_id = $1") }, false)
		h := &Handler{DB: p, Events: &events.Recorder{}}
		r := chatRequest(f.u, "", map[string]string{"messageID": f.m.String()}, "")
		r = r.WithContext(copyChatRouteContext(ctx, r))
		if err := h.getThread(httptest.NewRecorder(), r); !errors.Is(err, context.Canceled) {
			t.Fatalf("thread page cancellation=%v", err)
		}
	})
	t.Run("load after committed edit", func(t *testing.T) {
		p, ctx := cancelQueryPool(t, f.p, func(sql string) bool { return sql == "commit" }, true)
		recorder := &events.Recorder{}
		h := &Handler{DB: p, Events: recorder}
		r := chatRequest(f.u, `{"content":"edited before disconnect"}`, map[string]string{"channelID": f.c.ID.String(), "messageID": f.m.String()}, "")
		r = r.WithContext(auth.WithUser(copyChatRouteContext(ctx, r), f.u))
		if err := h.editMessage(httptest.NewRecorder(), r); !errors.Is(err, context.Canceled) {
			t.Fatalf("post-edit cancellation=%v", err)
		}
		msg, err := GetMessage(context.Background(), f.p, f.m)
		if err != nil || msg.Content != "edited before disconnect" {
			t.Fatalf("committed edit lost: %v,%v", msg, err)
		}
		if len(recorder.Snapshot()) != 0 {
			t.Fatal("unsuccessful response published update")
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
	h := &Handler{DB: f.p, Events: recorder}
	err := h.createMessage(httptest.NewRecorder(), chatRequest(f.u, `{"content":"new"}`, map[string]string{"channelID": f.c.ID.String()}, ""))
	requireChatError(t, err, "test write rejected")
	if len(recorder.Snapshot()) != 0 {
		t.Fatal("failed message creation published an event")
	}
}
