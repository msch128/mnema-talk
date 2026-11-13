//go:build integration

package chat

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/chat/channels"
	"github.com/msch128/mnema-talk/internal/chat/chattest"
	"github.com/msch128/mnema-talk/internal/chat/core"
	"github.com/msch128/mnema-talk/internal/chat/mentions"
	"github.com/msch128/mnema-talk/internal/chat/messages"
	"github.com/msch128/mnema-talk/internal/chat/search"
)

func TestChatSchemaDriftReportsDecodeAndQueryFailures(t *testing.T) {
	f := newChatFixture(t)
	ctx := context.Background()
	t.Run("category decoding", func(t *testing.T) {
		p := chattest.DriftPool(t, f.p, map[string]string{"categories": `SELECT gen_random_uuid() id, NULL::text name, 0 sort_order, NOW() created_at`})
		_, _, err := channels.GetServerHierarchy(ctx, p)
		chattest.RequireError(t, err, "NULL")
	})
	t.Run("category stream error", func(t *testing.T) {
		if _, err := channels.CreateCategory(ctx, f.p, "error-stream", 0); err != nil {
			t.Fatal(err)
		}
		p := chattest.DriftPool(t, f.p, map[string]string{"categories": `SELECT id,name,1/(sort_order-sort_order) sort_order,created_at FROM public.categories`})
		_, _, err := channels.GetServerHierarchy(ctx, p)
		chattest.RequireError(t, err, "division by zero")
	})
	t.Run("channel query", func(t *testing.T) {
		p := chattest.DriftPool(t, f.p, map[string]string{"channels": `SELECT id FROM public.channels`})
		_, _, err := channels.GetServerHierarchy(ctx, p)
		chattest.RequireError(t, err, "query channels")
	})
	t.Run("channel decoding", func(t *testing.T) {
		p := chattest.DriftPool(t, f.p, map[string]string{"channels": `SELECT id,number,category_id,NULL::text name,type,topic,sort_order,created_at,user_limit,slow_mode_seconds,auto_delete_hours,auto_delete_scope,dm_id,is_lobby,is_temp,leader_id,is_stage,slug FROM public.channels`})
		_, _, err := channels.GetServerHierarchy(ctx, p)
		chattest.RequireError(t, err, "NULL")
	})
	t.Run("member decoding", func(t *testing.T) {
		p := chattest.DriftPool(t, f.p, map[string]string{"users": `SELECT id,username,NULL::text display_name,bio,role,avatar_s3_key,status_text,NULL::timestamptz status_expires_at,created_at,public_voice_seconds,message_count,disabled_at,NULL::timestamptz banned_at,NULL::timestamptz banned_until,NULL::timestamptz deleted_at,is_bot FROM public.users`})
		_, err := core.GetAllMembers(ctx, p)
		chattest.RequireError(t, err, "NULL")
	})
	t.Run("mention stream", func(t *testing.T) {
		p := chattest.DriftPool(t, f.p, map[string]string{"users": `SELECT CASE WHEN public_voice_seconds >= 0 THEN (1/(public_voice_seconds-public_voice_seconds))::text::uuid ELSE id END id,username,disabled_at,NULL::timestamptz banned_at,NULL::timestamptz banned_until,NULL::timestamptz deleted_at,is_bot FROM public.users`})
		_, err := mentions.ResolveMentionSet(ctx, p, "@all", f.u.ID, nil)
		chattest.RequireError(t, err, "resolve mentions")
	})
	t.Run("message decoding", func(t *testing.T) {
		p := chattest.DriftPool(t, f.p, map[string]string{"messages": `SELECT id,number,channel_id,user_id,parent_id,NULL::text content,is_pinned,is_edited,created_at,updated_at,reply_to_id FROM public.messages`})
		if _, err := messages.GetMessage(ctx, p, f.m); err == nil {
			t.Fatal("null content accepted")
		}
		if _, _, err := search.Search(ctx, p, search.SearchQuery{ChannelID: &f.c.ID, Limit: 10}); err == nil {
			t.Fatal("null search content accepted")
		}
	})
	t.Run("attachments query", func(t *testing.T) {
		p := chattest.DriftPool(t, f.p, map[string]string{"media": `SELECT id,message_id FROM public.media`})
		msgs := []core.Message{{ID: f.m}}
		chattest.RequireError(t, messages.Enrich(ctx, p, msgs), "load attachments")
		_, _, err := search.Search(ctx, p, search.SearchQuery{Text: "hello", Limit: 10})
		chattest.RequireError(t, err, "load attachments")
	})
	t.Run("attachment decoding", func(t *testing.T) {
		p := chattest.DriftPool(t, f.p, map[string]string{"media": `SELECT gen_random_uuid() id,id message_id,NULL::text original_filename,'image/png'::text mime_type,1::bigint size_bytes,false is_deleted,NOW() created_at,0::smallint position,NULL::int width,NULL::int height FROM public.messages`})
		chattest.RequireError(t, messages.Enrich(ctx, p, []core.Message{{ID: f.m}}), "NULL")
	})
	t.Run("attachment stream", func(t *testing.T) {
		p := chattest.DriftPool(t, f.p, map[string]string{"media": `SELECT gen_random_uuid() id,id message_id,'image.png'::text original_filename,'image/png'::text mime_type,1/(number-number) size_bytes,false is_deleted,NOW() created_at,0::smallint position,NULL::int width,NULL::int height FROM public.messages`})
		chattest.RequireError(t, messages.Enrich(ctx, p, []core.Message{{ID: f.m}}), "division by zero")
	})
	t.Run("reaction query", func(t *testing.T) {
		p := chattest.DriftPool(t, f.p, map[string]string{"message_reactions": `SELECT id FROM public.messages`})
		chattest.RequireError(t, messages.Enrich(ctx, p, []core.Message{{ID: f.m}}), "load reactions")
	})
	t.Run("reaction decoding", func(t *testing.T) {
		p := chattest.DriftPool(t, f.p, map[string]string{"message_reactions": `SELECT id message_id,user_id,NULL::text emoji,created_at FROM public.messages`})
		chattest.RequireError(t, messages.Enrich(ctx, p, []core.Message{{ID: f.m}}), "NULL")
		_, err := messages.ReactionSummary(ctx, p, f.m)
		chattest.RequireError(t, err, "NULL")
	})
	t.Run("reaction stream", func(t *testing.T) {
		p := chattest.DriftPool(t, f.p, map[string]string{"message_reactions": `SELECT id message_id,user_id,(1/(number-number))::text emoji,created_at FROM public.messages`})
		chattest.RequireError(t, messages.Enrich(ctx, p, []core.Message{{ID: f.m}}), "division by zero")
	})
	t.Run("mention query", func(t *testing.T) {
		p := chattest.DriftPool(t, f.p, map[string]string{"message_mentions": `SELECT id FROM public.messages`})
		chattest.RequireError(t, messages.Enrich(ctx, p, []core.Message{{ID: f.m}}), "load mentions")
	})
	t.Run("mention decoding", func(t *testing.T) {
		p := chattest.DriftPool(t, f.p, map[string]string{"message_mentions": `SELECT id message_id,'invalid-uuid'::text user_id FROM public.messages`})
		if err := messages.Enrich(ctx, p, []core.Message{{ID: f.m}}); err == nil {
			t.Fatal("invalid mention UUID accepted")
		}
	})
	t.Run("collect key decoding", func(t *testing.T) {
		var keys []string
		chattest.RequireError(t, messages.CollectKeys(ctx, f.p, &keys, "SELECT NULL::text"), "collect media keys")
	})
	t.Run("channel group query", func(t *testing.T) {
		p := chattest.DriftPool(t, f.p, map[string]string{"channels": `SELECT id FROM public.channels`})
		tx, err := p.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(ctx)
		_, _, err = channels.LockChannelGroup(ctx, tx, nil)
		chattest.RequireError(t, err, "lock channel group")
	})
	t.Run("channel group decoding", func(t *testing.T) {
		p := chattest.DriftPool(t, f.p, map[string]string{"channels": `SELECT id,category_id,NULL::int sort_order,created_at,type FROM public.channels`})
		tx, err := p.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(ctx)
		_, _, err = channels.LockChannelGroup(ctx, tx, nil)
		chattest.RequireError(t, err, "NULL")
	})
}

func TestChatMissingRelationsFailTransactions(t *testing.T) {
	f := newChatFixture(t)
	ctx := context.Background()
	t.Run("collect media keys", func(t *testing.T) {
		p := chattest.DriftPool(t, f.p, map[string]string{"media": `SELECT id FROM public.media`})
		_, err := messages.DeleteMessageAs(ctx, p, f.m, f.u, f.u.IsAdmin())
		chattest.RequireError(t, err, "collect media keys")
		_, err = channels.DeleteChannel(ctx, p, f.c.ID)
		chattest.RequireError(t, err, "collect media keys")
	})
	t.Run("count reaction query", func(t *testing.T) {
		// This updatable view still enforces the table's real foreign key.
		p := chattest.DriftPool(t, f.p, map[string]string{"message_reactions": `SELECT message_id,user_id,emoji FROM public.message_reactions`})
		// Adding to this simple view reaches the real table and its FK.
		_, err := messages.ToggleReactionAs(ctx, p, f.m, uuid.New(), "ok", true)
		chattest.RequireError(t, err, "add reaction")
	})
	t.Run("reaction lock", func(t *testing.T) {
		p := chattest.DriftPool(t, f.p, map[string]string{"messages": `SELECT number::int id FROM public.messages`})
		_, err := messages.ToggleReactionAs(ctx, p, f.m, f.u.ID, "ok", true)
		chattest.RequireError(t, err, "lock message")
	})
}
