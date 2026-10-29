//go:build integration

package chat

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/audit"
	"github.com/msch128/mnema-talk/internal/httpx"
)

func newMember(t *testing.T, f chatFixture, name string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := f.p.QueryRow(context.Background(), `INSERT INTO users (username, display_name, password_hash) VALUES ($1, $1, 'x') RETURNING id`, name).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func apiStatus(err error) int {
	var api *httpx.APIError
	if errors.As(err, &api) {
		return api.Status
	}
	return 0
}

func TestParentLookupReportsDatabaseFailuresAsServerErrors(t *testing.T) {
	f := newChatFixture(t)
	missing := uuid.New()
	if err := ValidateParent(context.Background(), f.p, f.c.ID, &missing); apiStatus(err) != http.StatusBadRequest {
		t.Fatalf("missing parent: %v", err)
	}
	if err := ValidateReplyTarget(context.Background(), f.p, f.c.ID, nil, &missing); apiStatus(err) != http.StatusBadRequest {
		t.Fatalf("missing reply target: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := ValidateParent(ctx, f.p, f.c.ID, &f.m); err == nil || apiStatus(err) == http.StatusBadRequest {
		t.Fatalf("parent lookup failure: %v", err)
	}
	if err := ValidateReplyTarget(ctx, f.p, f.c.ID, nil, &f.m); err == nil || apiStatus(err) == http.StatusBadRequest {
		t.Fatalf("reply target lookup failure: %v", err)
	}
}

func TestAuditSnapshotIsEmptyWhenTheChannelCannotBeRead(t *testing.T) {
	f := newChatFixture(t)
	broken := driftPool(t, f.p, map[string]string{"channels": `SELECT id, name FROM public.channels`})
	h := &Handler{DB: broken, Audit: &audit.Log{DB: f.p}}
	if s := h.snapshotMessage(context.Background(), f.m, false); s != nil {
		t.Fatalf("snapshot without a channel lookup: %+v", s)
	}
}

func TestBotReactionsAreSetNotToggled(t *testing.T) {
	f := newChatFixture(t)
	ctx := context.Background()
	const emoji = "👍"
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			if _, _, err := SetReactionAs(ctx, f.p, f.m, f.u.ID, emoji, true, true); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	reactions, changed, err := SetReactionAs(ctx, f.p, f.m, f.u.ID, emoji, true, true)
	if err != nil || changed || len(reactions) != 1 || reactions[0].Count != 1 {
		t.Fatalf("after concurrent adds: %+v changed=%v err=%v", reactions, changed, err)
	}
	if _, changed, err = SetReactionAs(ctx, f.p, f.m, f.u.ID, emoji, false, false); err != nil || !changed {
		t.Fatalf("taking back without Add reactions: changed=%v err=%v", changed, err)
	}
	reactions, changed, err = SetReactionAs(ctx, f.p, f.m, f.u.ID, emoji, false, true)
	if err != nil || changed || len(reactions) != 0 {
		t.Fatalf("removing twice: %+v changed=%v err=%v", reactions, changed, err)
	}
	if _, _, err = SetReactionAs(ctx, f.p, f.m, f.u.ID, emoji, true, false); apiStatus(err) != http.StatusForbidden {
		t.Fatalf("add without permission: %v", err)
	}
}

func TestSortOrderBeyondTheColumnIsRefused(t *testing.T) {
	f := newChatFixture(t)
	ctx := context.Background()
	if _, err := CreateCategory(ctx, f.p, "big", 3_000_000_000); apiStatus(err) != http.StatusBadRequest {
		t.Fatalf("category: %v", err)
	}
	if _, err := CreateChannel(ctx, f.p, nil, "big", ChannelTypeText, "", -3_000_000_000); apiStatus(err) != http.StatusBadRequest {
		t.Fatalf("channel: %v", err)
	}
	cat, err := CreateCategory(ctx, f.p, "ok", MaxSortOrder)
	if err != nil {
		t.Fatal(err)
	}
	err = ApplyLayout(ctx, f.p, []CategoryOrder{{ID: cat.ID, SortOrder: 3_000_000_000}}, nil)
	if apiStatus(err) != http.StatusBadRequest {
		t.Fatalf("category order: %v", err)
	}
	err = ApplyLayout(ctx, f.p, nil, []ChannelPlacement{{ID: f.c.ID, SortOrder: 3_000_000_000}})
	if apiStatus(err) != http.StatusBadRequest {
		t.Fatalf("channel placement: %v", err)
	}
}

func TestChannelInADeletedCategoryIsRefusedNotAFailure(t *testing.T) {
	f := newChatFixture(t)
	ctx := context.Background()
	cat, err := CreateCategory(ctx, f.p, "doomed", 0)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := f.p.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := DeleteCategoryTx(ctx, tx, cat.ID); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := CreateChannel(ctx, f.p, &cat.ID, "late", ChannelTypeText, "", 0)
		done <- err
	}()
	select {
	case err := <-done:
		t.Fatalf("the channel was created while the category was being deleted: %v", err)
	case <-time.After(300 * time.Millisecond):
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
	cat, err := CreateCategory(ctx, f.p, "private", 0)
	if err != nil {
		t.Fatal(err)
	}
	ch, err := CreateChannel(ctx, f.p, &cat.ID, "inside", ChannelTypeText, "", 0)
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
	if err := DeleteCategory(ctx, f.p, cat.ID); err != nil {
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

func TestFavouritesReportOnlyWhatChanged(t *testing.T) {
	f := newChatFixture(t)
	ctx := context.Background()
	var dms []uuid.UUID
	for _, name := range []string{"fav_a", "fav_b", "fav_c"} {
		id, _, err := CreateDM(ctx, f.p, NewDM{Creator: f.u.ID, Others: []uuid.UUID{newMember(t, f, name)}})
		if err != nil {
			t.Fatal(err)
		}
		dms = append(dms, id)
	}
	a, b, c := dms[0], dms[1], dms[2]
	changed := func(ids ...uuid.UUID) []uuid.UUID {
		t.Helper()
		got, err := SetDMFavourites(ctx, f.p, f.u.ID, ids)
		if err != nil {
			t.Fatal(err)
		}
		slices.SortFunc(got, func(x, y uuid.UUID) int { return slices.Compare(x[:], y[:]) })
		return got
	}
	sorted := func(ids ...uuid.UUID) []uuid.UUID {
		slices.SortFunc(ids, func(x, y uuid.UUID) int { return slices.Compare(x[:], y[:]) })
		return ids
	}
	if got := changed(a, b); !slices.Equal(got, sorted(a, b)) {
		t.Fatalf("first favourites: %v", got)
	}
	if got := changed(a, b); len(got) != 0 {
		t.Fatalf("same favourites again: %v", got)
	}
	if got := changed(a, b, c); !slices.Equal(got, []uuid.UUID{c}) {
		t.Fatalf("added c: %v", got)
	}
	if got := changed(b, a, c); !slices.Equal(got, sorted(a, b)) {
		t.Fatalf("swapped a and b: %v", got)
	}
	if got := changed(b, a); !slices.Equal(got, []uuid.UUID{c}) {
		t.Fatalf("dropped c: %v", got)
	}
}

func TestGroupLimitCountsAreSerializedPerAddedMember(t *testing.T) {
	f := newChatFixture(t)
	ctx := context.Background()
	bob, carol := newMember(t, f, "lock_bob"), newMember(t, f, "lock_carol")
	group, _, err := CreateDM(ctx, f.p, NewDM{Creator: f.u.ID, Others: []uuid.UUID{bob, carol}})
	if err != nil {
		t.Fatal(err)
	}
	dave := newMember(t, f, "lock_dave")
	hold, err := f.p.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = hold.Rollback(ctx) }()
	if err := lockUsers(ctx, hold, []uuid.UUID{dave}); err != nil {
		t.Fatal(err)
	}
	blocked := func(run func() error) {
		t.Helper()
		done := make(chan error, 1)
		go func() { done <- run() }()
		select {
		case err := <-done:
			t.Fatalf("finished while the member's row was locked: %v", err)
		case <-time.After(300 * time.Millisecond):
		}
		if err := hold.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	blocked(func() error {
		added, err := AddDMMember(ctx, f.p, group, f.u.ID, dave)
		if err == nil && !added {
			err = errors.New("not added")
		}
		return err
	})
	erin := newMember(t, f, "lock_erin")
	hold, err = f.p.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := lockUsers(ctx, hold, []uuid.UUID{erin}); err != nil {
		t.Fatal(err)
	}
	blocked(func() error {
		_, _, err := CreateDM(ctx, f.p, NewDM{Creator: f.u.ID, Others: []uuid.UUID{erin, bob}})
		return err
	})
}

func TestGetMemberReadsOneVisibleMember(t *testing.T) {
	f := newChatFixture(t)
	ctx := context.Background()
	m, err := GetMember(ctx, f.p, f.u.ID)
	if err != nil || m.ID != f.u.ID || m.Username != f.u.Username {
		t.Fatalf("member: %+v %v", m, err)
	}
	if _, err := GetMember(ctx, f.p, uuid.New()); !errors.Is(err, errUnknownUser) {
		t.Fatalf("unknown member: %v", err)
	}
}

// A reaction stored before the formatting-character rule stays removable by
// its owner, while adding such an emoji is refused.
func TestStoredReactionWithFormattingCharacterCanBeTakenBack(t *testing.T) {
	f := newChatFixture(t)
	ctx := context.Background()
	const old = "👍‎"
	if _, err := f.p.Exec(ctx, `INSERT INTO message_reactions (message_id, user_id, emoji) VALUES ($1, $2, $3)`, f.m, f.u.ID, old); err != nil {
		t.Fatal(err)
	}
	if _, err := validateReactionRef(old); err != nil {
		t.Fatalf("lookup rule rejects the stored emoji: %v", err)
	}
	reactions, changed, err := SetReactionAs(ctx, f.p, f.m, f.u.ID, old, false, true)
	if err != nil || !changed || len(reactions) != 0 {
		t.Fatalf("taking back: %+v changed=%v err=%v", reactions, changed, err)
	}
	if _, _, err := SetReactionAs(ctx, f.p, f.m, f.u.ID, old, true, true); apiStatus(err) != http.StatusBadRequest {
		t.Fatalf("adding it again: %v", err)
	}
	if _, err := ToggleReactionAs(ctx, f.p, f.m, f.u.ID, old, true); apiStatus(err) != http.StatusBadRequest {
		t.Fatalf("toggling it on: %v", err)
	}
}
