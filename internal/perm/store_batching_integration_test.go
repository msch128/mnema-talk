//go:build integration

package perm_test

import (
	"context"
	"fmt"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/perm"
	"github.com/msch128/mnema-talk/internal/testutil/stmtcount"
)

// countedService is a permission service whose database traffic is counted.
func countedService(t *testing.T, w *world) (*perm.Service, *stmtcount.Counter) {
	t.Helper()
	pool, counter := stmtcount.Pool(t, w.p.Pool)
	return perm.NewService(&db.Pool{Pool: pool}), counter
}

func (w *world) newRoles(t *testing.T, n int) []uuid.UUID {
	t.Helper()
	ids := make([]uuid.UUID, n)
	for i := range ids {
		name := fmt.Sprintf("role-%d", i)
		role, err := w.svc.CreateRole(context.Background(), w.owner, perm.RoleInput{Name: &name})
		if err != nil {
			t.Fatal(err)
		}
		ids[i] = role.ID
	}
	return ids
}

func (w *world) newMembers(t *testing.T, n int) []uuid.UUID {
	t.Helper()
	ids := make([]uuid.UUID, n)
	for i := range ids {
		err := w.p.QueryRow(context.Background(),
			`INSERT INTO users (username, display_name, password_hash, role) VALUES ($1, $1, 'x', 'user') RETURNING id`,
			fmt.Sprintf("extra-%d", i)).Scan(&ids[i])
		if err != nil {
			t.Fatal(err)
		}
	}
	return ids
}

func (w *world) heldRoles(t *testing.T, user uuid.UUID) []uuid.UUID {
	t.Helper()
	all, err := w.svc.MemberRoles(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	held := slices.Clone(all[user])
	slices.SortFunc(held, func(a, b uuid.UUID) int { return slices.Compare(a[:], b[:]) })
	return held
}

func sortedIDs(ids []uuid.UUID) []uuid.UUID {
	out := slices.Clone(ids)
	slices.SortFunc(out, func(a, b uuid.UUID) int { return slices.Compare(a[:], b[:]) })
	return out
}

func TestSetMemberRolesStoresExactlyTheRequestedSet(t *testing.T) {
	ctx := context.Background()
	w := newWorld(t)
	roles := w.newRoles(t, 6)
	steps := []struct {
		name string
		want []uuid.UUID
	}{
		{"add several at once", roles[:3]},
		{"keep some, add and remove others", []uuid.UUID{roles[1], roles[4], roles[5]}},
		{"setting the same set again changes nothing", []uuid.UUID{roles[1], roles[4], roles[5]}},
		{"a duplicated id counts once", []uuid.UUID{roles[2], roles[2]}},
		{"an empty list removes everything", nil},
	}
	for _, s := range steps {
		if err := w.svc.SetMemberRoles(ctx, w.owner, w.member.ID, s.want); err != nil {
			t.Fatalf("%s: %v", s.name, err)
		}
		if got, want := w.heldRoles(t, w.member.ID), sortedIDs(slices.Compact(sortedIDs(s.want))); !slices.Equal(got, want) {
			t.Fatalf("%s: holds %v, want %v", s.name, got, want)
		}
	}
}

func TestSetMemberRolesRejectedChangeLeavesRolesUntouched(t *testing.T) {
	ctx := context.Background()
	w := newWorld(t)
	roles := w.newRoles(t, 2)
	if err := w.svc.SetMemberRoles(ctx, w.owner, w.member.ID, roles[:1]); err != nil {
		t.Fatal(err)
	}
	wantAPI(t, "unknown role", w.svc.SetMemberRoles(ctx, w.owner, w.member.ID, []uuid.UUID{roles[1], uuid.New()}), 400)
	if got := w.heldRoles(t, w.member.ID); !slices.Equal(got, roles[:1]) {
		t.Fatalf("holds %v after a rejected change, want %v", got, roles[:1])
	}
}

func TestSetMemberRolesCostsTheSameStatementsForFewAndManyRoles(t *testing.T) {
	ctx := context.Background()
	w := newWorld(t)
	roles := w.newRoles(t, 20)
	svc, counter := countedService(t, w)
	measure := func(user uuid.UUID, ids []uuid.UUID) int {
		counter.Reset()
		if err := svc.SetMemberRoles(ctx, w.owner, user, ids); err != nil {
			t.Fatal(err)
		}
		return counter.Count()
	}
	few := measure(w.member.ID, roles[:2])
	many := measure(w.other.ID, roles)
	if few != many {
		t.Fatalf("2 roles took %d statements, 20 roles took %d", few, many)
	}
	// Dropping most of them again is just as cheap.
	if again := measure(w.other.ID, roles[:1]); again != few {
		t.Fatalf("removing 19 roles took %d statements, want %d", again, few)
	}
	t.Logf("statements per SetMemberRoles call: %d", many)
}

func (w *world) overwriteSet(t *testing.T, kind string, id uuid.UUID) map[string]perm.Overwrite {
	t.Helper()
	list, err := w.svc.Overwrites(context.Background(), kind, id)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]perm.Overwrite{}
	for _, o := range list {
		out[o.Type+":"+o.ID.String()] = o
	}
	return out
}

func TestSetOverwritesReplacesTheWholeListOnEveryTarget(t *testing.T) {
	ctx := context.Background()
	w := newWorld(t)
	roles := w.newRoles(t, 2)
	targets := map[string]uuid.UUID{perm.TargetChannel: w.channel, perm.TargetCategory: w.category}
	for kind, id := range targets {
		first := []perm.Overwrite{
			{Type: "role", ID: roles[0], Allow: perm.Speak},
			{Type: "role", ID: roles[1], Deny: perm.SendMessages},
			{Type: "member", ID: w.member.ID, Allow: perm.SendMessages, Deny: perm.Speak},
		}
		if err := w.svc.SetOverwrites(ctx, w.owner, kind, id, first); err != nil {
			t.Fatalf("%s add: %v", kind, err)
		}
		got := w.overwriteSet(t, kind, id)
		if len(got) != 3 || got["member:"+w.member.ID.String()].Deny != perm.Speak || got["role:"+roles[0].String()].Allow != perm.Speak {
			t.Fatalf("%s after add: %+v", kind, got)
		}
		second := []perm.Overwrite{
			{Type: "role", ID: roles[1], Deny: perm.SendMessages | perm.Speak},
			{Type: "member", ID: w.other.ID, Allow: perm.Speak},
			{Type: "role", ID: roles[0]},
		}
		if err := w.svc.SetOverwrites(ctx, w.owner, kind, id, second); err != nil {
			t.Fatalf("%s change: %v", kind, err)
		}
		got = w.overwriteSet(t, kind, id)
		if len(got) != 2 || got["role:"+roles[1].String()].Deny != perm.SendMessages|perm.Speak || got["member:"+w.other.ID.String()].Allow != perm.Speak {
			t.Fatalf("%s after change (empty entry dropped, old ones gone): %+v", kind, got)
		}
		if err := w.svc.SetOverwrites(ctx, w.owner, kind, id, nil); err != nil {
			t.Fatalf("%s clear: %v", kind, err)
		}
		if got = w.overwriteSet(t, kind, id); len(got) != 0 {
			t.Fatalf("%s after clearing: %+v", kind, got)
		}
	}
}

func TestSetOverwritesCostsTheSameStatementsForFewAndManyEntries(t *testing.T) {
	ctx := context.Background()
	w := newWorld(t)
	members := w.newMembers(t, 30)
	svc, counter := countedService(t, w)
	entries := func(n int) []perm.Overwrite {
		list := make([]perm.Overwrite, n)
		for i := range list {
			list[i] = perm.Overwrite{Type: "member", ID: members[i], Deny: perm.SendMessages}
		}
		return list
	}
	measure := func(n int) int {
		counter.Reset()
		if err := svc.SetOverwrites(ctx, w.owner, perm.TargetChannel, w.channel, entries(n)); err != nil {
			t.Fatal(err)
		}
		return counter.Count()
	}
	few, many := measure(1), measure(30)
	if few != many {
		t.Fatalf("1 entry took %d statements, 30 entries took %d", few, many)
	}
	if got := w.overwriteSet(t, perm.TargetChannel, w.channel); len(got) != 30 {
		t.Fatalf("stored %d overwrites, want 30", len(got))
	}
	t.Logf("statements per SetOverwrites call: %d", many)
}
