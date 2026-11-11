//go:build integration

package chat

import (
	"context"
	"testing"

	"net/http"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/chat/channels"
	"github.com/msch128/mnema-talk/internal/chat/core"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/testutil/pgfault"
)

// Every statement of a category deletion can fail; each failure is reported
// and leaves the category in place.
func TestDeleteCategoryReportsEveryDatabaseFailure(t *testing.T) {
	f := newChatFixture(t)
	ctx := context.Background()
	cat, err := channels.CreateCategory(ctx, f.p, "kept", 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{
		"SELECT 1 FROM categories WHERE id",
		"SELECT EXISTS (SELECT 1 FROM permission_overwrites",
		"INSERT INTO permission_overwrites",
		"DELETE FROM categories",
	} {
		pool := &db.Pool{Pool: pgfault.Pool(t, f.p.Pool, &pgfault.Tracer{Query: query})}
		if err := channels.DeleteCategory(ctx, pool, cat.ID); err == nil {
			t.Fatalf("%q failing: the deletion reported success", query)
		}
	}
	var n int
	if err := f.p.QueryRow(ctx, `SELECT COUNT(*) FROM categories WHERE id = $1`, cat.ID).Scan(&n); err != nil || n != 1 {
		t.Fatalf("category rows = %d, %v, want it kept", n, err)
	}
}

func TestChannelInADeletedCategoryIsRefusedNotAFailure(t *testing.T) {
	f := newChatFixture(t)
	ctx := context.Background()
	cat, err := channels.CreateCategory(ctx, f.p, "doomed", 0)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := f.p.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := channels.DeleteCategoryTx(ctx, tx, cat.ID); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := channels.CreateChannelWithStage(ctx, f.p, &cat.ID, "late", core.ChannelTypeText, "", 0, false)
		done <- err
	}()
	waitForBlockedStatement(t, f.p)
	select {
	case err := <-done:
		t.Fatalf("the channel was created while the category was being deleted: %v", err)
	default:
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-done; apiStatus(err) != http.StatusBadRequest {
		t.Fatalf("channel in the deleted category: %v", err)
	}
}

func TestDeletingACategoryKeepsRoleAndMemberOverwrites(t *testing.T) {
	f := newChatFixture(t)
	ctx := context.Background()
	cat, err := channels.CreateCategory(ctx, f.p, "private", 0)
	if err != nil {
		t.Fatal(err)
	}
	ch, err := channels.CreateChannelWithStage(ctx, f.p, &cat.ID, "inside", core.ChannelTypeText, "", 0, false)
	if err != nil {
		t.Fatal(err)
	}
	var role uuid.UUID
	if err := f.p.QueryRow(ctx, `SELECT id FROM roles LIMIT 1`).Scan(&role); err != nil {
		t.Fatal(err)
	}
	if _, err := f.p.Exec(ctx, `INSERT INTO permission_overwrites (category_id, role_id, allow, deny) VALUES ($1, $2, 1, 0)`, cat.ID, role); err != nil {
		t.Fatal(err)
	}
	if _, err := f.p.Exec(ctx, `INSERT INTO permission_overwrites (category_id, user_id, allow, deny) VALUES ($1, $2, 2, 0)`, cat.ID, f.u.ID); err != nil {
		t.Fatal(err)
	}
	if err := channels.DeleteCategory(ctx, f.p, cat.ID); err != nil {
		t.Fatal(err)
	}
	var roles, users int
	if err := f.p.QueryRow(ctx, `
		SELECT COUNT(*) FILTER (WHERE role_id = $2 AND allow = 1), COUNT(*) FILTER (WHERE user_id = $3 AND allow = 2)
		FROM permission_overwrites WHERE channel_id = $1`, ch.ID, role, f.u.ID).Scan(&roles, &users); err != nil {
		t.Fatal(err)
	}
	if roles != 1 || users != 1 {
		t.Fatalf("kept overwrites: roles=%d users=%d", roles, users)
	}
}
