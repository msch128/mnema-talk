//go:build integration

package discordimport

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

func TestUndoKeepsRolesTheCallerCanNoLongerManage(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	rec, err := w.im.Apply(ctx, w.admin, "adm", applyReq(w.preview(w.admin)))
	w.must(err)
	// Someone moved an imported role above the administrator's highest role.
	_, err = w.p.Exec(ctx, `UPDATE roles SET position = 1000 WHERE name = 'Moderator'`)
	w.must(err)
	w.svc.Invalidate()

	res, err := w.im.Undo(ctx, w.admin, rec.ID, UndoRequest{})
	w.must(err)
	if res.KeptRoles != 1 || res.Removed.Roles != 2 {
		t.Fatalf("result = %+v", res)
	}
	var left int
	w.must(w.p.QueryRow(ctx, `SELECT COUNT(*) FROM roles WHERE name = 'Moderator'`).Scan(&left))
	if left != 1 {
		t.Fatal("the role above the caller was deleted")
	}
	// A partial undo leaves the import open for someone who outranks the role.
	undoneAt := func() (set bool) {
		w.must(w.p.QueryRow(ctx, `SELECT undone_at IS NOT NULL FROM discord_imports WHERE id = $1`, rec.ID).Scan(&set))
		return set
	}
	if undoneAt() {
		t.Fatal("import was closed although a role was kept")
	}
	res, err = w.im.Undo(ctx, w.owner, rec.ID, UndoRequest{})
	w.must(err)
	if res.KeptRoles != 0 {
		t.Fatalf("owner undo = %+v", res)
	}
	w.must(w.p.QueryRow(ctx, `SELECT COUNT(*) FROM roles WHERE name = 'Moderator'`).Scan(&left))
	if left != 0 {
		t.Fatal("the owner did not remove the kept role")
	}
	if !undoneAt() {
		t.Fatal("import still open after the full undo")
	}
	if _, err := w.im.Undo(ctx, w.owner, rec.ID, UndoRequest{}); !errors.Is(err, errAlreadyUndone) {
		t.Fatalf("third undo error = %v, want errAlreadyUndone", err)
	}

	// The Owner outranks everything and can still remove it.
	rec2, err := w.im.Apply(ctx, w.owner, "owner", applyReq(w.preview(w.owner)))
	w.must(err)
	_, err = w.p.Exec(ctx, `UPDATE roles SET position = 2000 WHERE id IN (SELECT object_id FROM discord_import_objects WHERE import_id = $1 AND kind = 'role') AND name LIKE 'Admin%'`, rec2.ID)
	w.must(err)
	w.svc.Invalidate()
	if res, err := w.im.Undo(ctx, w.owner, rec2.ID, UndoRequest{}); err != nil || res.KeptRoles != 0 {
		t.Fatalf("owner undo = %+v, %v", res, err)
	}
}

type recordedNotifier struct{ got [][]uuid.UUID }

func (n *recordedNotifier) PermissionsChanged(ids []uuid.UUID) { n.got = append(n.got, ids) }

func TestUndoNotifiesTheHoldersOfRemovedRoles(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	rec, err := w.im.Apply(ctx, w.admin, "adm", applyReq(w.preview(w.admin)))
	w.must(err)
	// Only roles are left to undo, so only their holders are affected.
	_, err = w.p.Exec(ctx, `DELETE FROM channels WHERE id IN (SELECT object_id FROM discord_import_objects WHERE kind = 'channel')`)
	w.must(err)
	_, err = w.p.Exec(ctx, `DELETE FROM categories WHERE id IN (SELECT object_id FROM discord_import_objects WHERE kind = 'category')`)
	w.must(err)
	_, err = w.p.Exec(ctx, `INSERT INTO user_roles (user_id, role_id) SELECT $1, id FROM roles WHERE name = 'Member'`, w.mod.ID)
	w.must(err)
	n := &recordedNotifier{}
	w.svc.SetNotifier(n)

	if _, err := w.im.Undo(ctx, w.admin, rec.ID, UndoRequest{}); err != nil {
		t.Fatal(err)
	}
	if len(n.got) != 1 || len(n.got[0]) != 1 || n.got[0][0] != w.mod.ID {
		t.Fatalf("notified %v, want only the holder %s", n.got, w.mod.ID)
	}
}
