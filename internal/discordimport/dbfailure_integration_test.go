//go:build integration

package discordimport

import (
	"context"
	"fmt"
	"testing"
)

// breakTable makes a table unreachable until the returned function runs, to
// see how the import behaves when the database fails halfway.
func (w *world) breakTable(name string) (restore func()) {
	w.t.Helper()
	ctx := context.Background()
	_, err := w.p.Exec(ctx, `ALTER TABLE `+name+` RENAME TO `+name+`_off`)
	w.must(err)
	var once bool
	restore = func() {
		if once {
			return
		}
		once = true
		_, err := w.p.Exec(ctx, `ALTER TABLE `+name+`_off RENAME TO `+name)
		w.must(err)
	}
	w.t.Cleanup(restore)
	return restore
}

func TestADatabaseFailureWhileImportingLeavesNothingBehind(t *testing.T) {
	tables := []string{"roles", "categories", "channels", "permission_overwrites", "discord_imports", "discord_import_objects", "audit_log", "instance_settings"}
	for _, table := range tables {
		t.Run(table, func(t *testing.T) {
			w := newWorld(t)
			ctx := context.Background()
			plan := w.preview(w.admin)
			before := w.tables()

			restore := w.breakTable(table)
			_, err := w.im.Apply(ctx, w.admin, "adm", applyReq(plan))
			_, previewErr := w.im.Preview(ctx, w.admin, "AbCdEf123456")
			restore()

			if err == nil {
				t.Fatal("the import reported success")
			}
			if table == "instance_settings" && previewErr == nil {
				t.Error("the preview reported success")
			}
			w.svc.Invalidate()
			if after := w.tables(); fmt.Sprint(after) != fmt.Sprint(before) {
				t.Fatalf("a failed import left data behind: %v -> %v", before, after)
			}
		})
	}
}

func TestADatabaseFailureWhileUndoingLeavesTheImportInPlace(t *testing.T) {
	tables := []string{"discord_imports", "discord_import_objects", "messages", "media", "categories", "channels", "roles", "user_roles", "audit_log"}
	for _, table := range tables {
		t.Run(table, func(t *testing.T) {
			w := newWorld(t)
			ctx := context.Background()
			imp, err := w.im.Apply(ctx, w.admin, "adm", applyReq(w.preview(w.admin)))
			w.must(err)
			before := w.tables()

			restore := w.breakTable(table)
			_, undoErr := w.im.Undo(ctx, w.admin, imp.ID, UndoRequest{})
			_, _ = w.im.Detail(ctx, imp.ID)
			_, _ = w.im.List(ctx)
			restore()

			if undoErr == nil {
				t.Fatal("the undo reported success")
			}
			w.svc.Invalidate()
			if after := w.tables(); fmt.Sprint(after) != fmt.Sprint(before) {
				t.Fatalf("a failed undo changed data: %v -> %v", before, after)
			}
		})
	}
}
