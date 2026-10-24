package discordimport

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type channelRows struct {
	ids        []uuid.UUID
	categories []*uuid.UUID
	names      []string
	types      []string
	topics     []string
	orders     []int32
	limits     []int32
	slow       []int32
	stages     []bool
}

// insertChannels appends the channels: inside the new categories from the
// top, and the uncategorized ones after every existing uncategorized channel.
func insertChannels(ctx context.Context, tx pgx.Tx, plan *Plan, c *created) error {
	n := len(plan.Channels)
	if n == 0 {
		return nil
	}
	var base int
	if err := tx.QueryRow(ctx, `SELECT COALESCE(MAX(sort_order), -1) + 1 FROM channels WHERE category_id IS NULL AND type <> 'dm'`).Scan(&base); err != nil {
		return fmt.Errorf("read channel order: %w", err)
	}
	rows := channelRows{}
	next := map[string]int{"": base}
	for _, ch := range plan.Channels {
		id := uuid.New()
		var cat *uuid.UUID
		if cid, ok := c.catBySrc[ch.CategorySourceID]; ok && ch.CategorySourceID != "" {
			cat = &cid
		}
		rows.ids, rows.categories = append(rows.ids, id), append(rows.categories, cat)
		// A Stage is a voice channel with is_stage set; the plan calls it "stage".
		typ := ch.Type
		if typ == "stage" {
			typ = "voice"
		}
		rows.names, rows.types, rows.topics = append(rows.names, ch.Name), append(rows.types, typ), append(rows.topics, ch.Topic)
		rows.stages = append(rows.stages, ch.Type == "stage")
		rows.orders = append(rows.orders, int32(next[ch.CategorySourceID]))
		rows.limits, rows.slow = append(rows.limits, int32(ch.UserLimit)), append(rows.slow, int32(ch.SlowModeSeconds))
		next[ch.CategorySourceID]++
		c.chanBySrc[ch.SourceID] = id
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO channels (id, category_id, name, type, topic, sort_order, user_limit, slow_mode_seconds, is_stage)
		SELECT * FROM unnest($1::uuid[], $2::uuid[], $3::text[], $4::text[], $5::text[], $6::int[], $7::int[], $8::int[], $9::bool[])`,
		rows.ids, rows.categories, rows.names, rows.types, rows.topics, rows.orders, rows.limits, rows.slow, rows.stages); err != nil {
		return fmt.Errorf("insert channels: %w", err)
	}
	c.channels = rows.ids
	return nil
}

// insertOverwrites stores the overwrites of the new categories and channels,
// all pointing at the new roles or at @everyone (on the new objects only).
func insertOverwrites(ctx context.Context, tx pgx.Tx, plan *Plan, c *created) error {
	var everyone uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT id FROM roles WHERE is_everyone`).Scan(&everyone); err != nil {
		return fmt.Errorf("read @everyone: %w", err)
	}
	var chans, cats []*uuid.UUID
	var roleIDs []uuid.UUID
	var allow, deny []int64
	add := func(ch, cat *uuid.UUID, o PlanOverwrite) {
		role, ok := c.roleBySrc[o.RoleSourceID]
		if o.Everyone {
			role, ok = everyone, true
		}
		if !ok {
			return
		}
		chans, cats, roleIDs = append(chans, ch), append(cats, cat), append(roleIDs, role)
		allow, deny = append(allow, int64(o.Allow)), append(deny, int64(o.Deny))
	}
	for _, cat := range plan.Categories {
		id := c.catBySrc[cat.SourceID]
		for _, o := range cat.Overwrites {
			add(nil, &id, o)
		}
	}
	for _, ch := range plan.Channels {
		id := c.chanBySrc[ch.SourceID]
		for _, o := range ch.Overwrites {
			add(&id, nil, o)
		}
	}
	if len(roleIDs) == 0 {
		return nil
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO permission_overwrites (channel_id, category_id, role_id, allow, deny)
		SELECT * FROM unnest($1::uuid[], $2::uuid[], $3::uuid[], $4::bigint[], $5::bigint[])`,
		chans, cats, roleIDs, allow, deny); err != nil {
		return fmt.Errorf("insert overwrites: %w", err)
	}
	return nil
}
