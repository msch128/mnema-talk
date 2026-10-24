package discordimport

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/audit"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
)

var (
	errImportNotFound = httpx.ErrNotFound("import not found")
	errAlreadyUndone  = httpx.ErrConflict("this import was already undone")
)

// ChannelMessages is a created channel that has messages by now.
type ChannelMessages struct {
	Name     string `json:"name"`
	Messages int    `json:"messages"`
}

// Detail is an import with what an undo would do right now.
type Detail struct {
	Record
	// Remaining are the created objects that still exist.
	Remaining Counts `json:"remaining"`
	// ChannelsWithMessages must be confirmed separately: undoing deletes their messages.
	ChannelsWithMessages []ChannelMessages `json:"channels_with_messages"`
	// KeptCategories are created categories an undo leaves in place because
	// they now hold channels that are not part of the import.
	KeptCategories []string `json:"kept_categories"`
}

type objectIDs struct{ roles, categories, channels []uuid.UUID }

type chanInfo struct {
	ID       uuid.UUID
	Name     string
	Messages int
}

func loadRecord(ctx context.Context, q db.Querier, id uuid.UUID, lock bool) (*Record, error) {
	sql := `SELECT id, template_code, template_name, created_by_name, created_at, categories, channels, roles, undone_at
		FROM discord_imports WHERE id = $1`
	if lock {
		sql += ` FOR UPDATE`
	}
	var r Record
	err := q.QueryRow(ctx, sql, id).Scan(&r.ID, &r.TemplateCode, &r.TemplateName, &r.CreatedByName, &r.CreatedAt,
		&r.Counts.Categories, &r.Counts.Channels, &r.Counts.Roles, &r.UndoneAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, errImportNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("read import: %w", err)
	}
	return &r, nil
}

func loadObjects(ctx context.Context, q db.Querier, importID uuid.UUID) (objectIDs, error) {
	o := objectIDs{roles: []uuid.UUID{}, categories: []uuid.UUID{}, channels: []uuid.UUID{}}
	rows, err := q.Query(ctx, `SELECT kind, object_id FROM discord_import_objects WHERE import_id = $1`, importID)
	if err != nil {
		return o, fmt.Errorf("read import objects: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var kind string
		var id uuid.UUID
		if err := rows.Scan(&kind, &id); err != nil {
			return o, err
		}
		switch kind {
		case "role":
			o.roles = append(o.roles, id)
		case "category":
			o.categories = append(o.categories, id)
		case "channel":
			o.channels = append(o.channels, id)
		}
	}
	return o, rows.Err()
}

func existingChannels(ctx context.Context, q db.Querier, ids []uuid.UUID) ([]chanInfo, error) {
	rows, err := q.Query(ctx, `
		SELECT c.id, c.name, (SELECT COUNT(*) FROM messages m WHERE m.channel_id = c.id)
		FROM channels c WHERE c.id = ANY($1) ORDER BY c.name, c.id`, ids)
	if err != nil {
		return nil, fmt.Errorf("read created channels: %w", err)
	}
	defer rows.Close()
	var out []chanInfo
	for rows.Next() {
		var ci chanInfo
		if err := rows.Scan(&ci.ID, &ci.Name, &ci.Messages); err != nil {
			return nil, err
		}
		out = append(out, ci)
	}
	return out, rows.Err()
}

// keptCategories names the created categories that hold a channel which is
// not one of ours.
func keptCategories(ctx context.Context, q db.Querier, cats, ours []uuid.UUID) ([]string, error) {
	rows, err := q.Query(ctx, `
		SELECT DISTINCT ca.name FROM categories ca JOIN channels ch ON ch.category_id = ca.id
		WHERE ca.id = ANY($1) AND NOT (ch.id = ANY($2)) ORDER BY ca.name`, cats, ours)
	if err != nil {
		return nil, fmt.Errorf("read kept categories: %w", err)
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func countExisting(ctx context.Context, q db.Querier, table string, ids []uuid.UUID) (int, error) {
	var n int
	err := q.QueryRow(ctx, `SELECT COUNT(*) FROM `+table+` WHERE id = ANY($1)`, ids).Scan(&n)
	return n, err
}

// List returns the recorded imports, newest first.
func (im *Importer) List(ctx context.Context) ([]Record, error) {
	rows, err := im.DB.Query(ctx, `
		SELECT id, template_code, template_name, created_by_name, created_at, categories, channels, roles, undone_at
		FROM discord_imports ORDER BY created_at DESC LIMIT 100`)
	if err != nil {
		return nil, fmt.Errorf("list imports: %w", err)
	}
	defer rows.Close()
	out := []Record{}
	for rows.Next() {
		var r Record
		if err := rows.Scan(&r.ID, &r.TemplateCode, &r.TemplateName, &r.CreatedByName, &r.CreatedAt,
			&r.Counts.Categories, &r.Counts.Channels, &r.Counts.Roles, &r.UndoneAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// Detail describes one import and what undoing it would do now.
func (im *Importer) Detail(ctx context.Context, id uuid.UUID) (*Detail, error) {
	rec, err := loadRecord(ctx, im.DB, id, false)
	if err != nil {
		return nil, err
	}
	d := &Detail{Record: *rec, ChannelsWithMessages: []ChannelMessages{}, KeptCategories: []string{}}
	if rec.UndoneAt != nil {
		return d, nil
	}
	obj, err := loadObjects(ctx, im.DB, id)
	if err != nil {
		return nil, err
	}
	chans, err := existingChannels(ctx, im.DB, obj.channels)
	if err != nil {
		return nil, err
	}
	d.Remaining.Channels = len(chans)
	d.ChannelsWithMessages = withMessages(chans)
	if d.Remaining.Categories, err = countExisting(ctx, im.DB, "categories", obj.categories); err != nil {
		return nil, err
	}
	if d.Remaining.Roles, err = countExisting(ctx, im.DB, "roles", obj.roles); err != nil {
		return nil, err
	}
	d.KeptCategories, err = keptCategories(ctx, im.DB, obj.categories, obj.channels)
	return d, err
}

func withMessages(chans []chanInfo) []ChannelMessages {
	out := []ChannelMessages{}
	for _, c := range chans {
		if c.Messages > 0 {
			out = append(out, ChannelMessages{Name: c.Name, Messages: c.Messages})
		}
	}
	return out
}

// UndoRequest is the body of the undo call.
type UndoRequest struct {
	// AcceptMessageLoss is required when a created channel has messages now.
	AcceptMessageLoss bool `json:"accept_message_loss"`
}

// UndoResult says what an undo removed.
type UndoResult struct {
	Removed Counts `json:"removed"`
	// AlreadyGone are created objects that had been deleted by hand before.
	AlreadyGone Counts `json:"already_gone"`
	// KeptCategories were left in place because they hold other channels now.
	KeptCategories []string `json:"kept_categories"`
}

// Undo removes exactly the objects the import created and that still exist.
// Objects deleted by hand since are skipped; a created category that now holds
// other channels stays. Created channels with messages need AcceptMessageLoss.
func (im *Importer) Undo(ctx context.Context, sub perm.Subject, id uuid.UUID, req UndoRequest) (*UndoResult, error) {
	var res *UndoResult
	var keys []string
	var gone []uuid.UUID
	err := im.Perms.Locked(ctx, sub, func(tx pgx.Tx, snap *perm.Snapshot) error {
		if !snap.Global(sub).Has(perm.Administrator) {
			return errNotAdmin
		}
		var err error
		res, keys, gone, err = undoTx(ctx, tx, sub, id, req, im.Audit)
		return err
	})
	if err != nil {
		return nil, err
	}
	im.Perms.Changed(nil)
	if im.Voice != nil {
		for _, ch := range gone {
			im.Voice.CloseVoiceChannel(ch)
		}
	}
	if im.Objects != nil && len(keys) > 0 {
		_ = im.Objects.DeleteBatch(ctx, keys) // rows are gone; leftovers are swept as orphans
	}
	if im.Events != nil {
		im.Events.Broadcast(events.ChannelsChanged, nil)
		im.Events.Broadcast(events.RolesChanged, nil)
	}
	return res, nil
}

func undoTx(ctx context.Context, tx pgx.Tx, sub perm.Subject, id uuid.UUID, req UndoRequest, log *audit.Log) (*UndoResult, []string, []uuid.UUID, error) {
	rec, err := loadRecord(ctx, tx, id, true)
	if err != nil {
		return nil, nil, nil, err
	}
	if rec.UndoneAt != nil {
		return nil, nil, nil, errAlreadyUndone
	}
	obj, err := loadObjects(ctx, tx, id)
	if err != nil {
		return nil, nil, nil, err
	}
	chans, err := existingChannels(ctx, tx, obj.channels)
	if err != nil {
		return nil, nil, nil, err
	}
	if with := withMessages(chans); len(with) > 0 && !req.AcceptMessageLoss {
		return nil, nil, nil, httpx.ErrConflict(fmt.Sprintf("%d created channel(s) have messages now; confirm that they are deleted too", len(with)))
	}
	res := &UndoResult{KeptCategories: []string{}}
	keys, gone, err := removeChannels(ctx, tx, chans)
	if err != nil {
		return nil, nil, nil, err
	}
	res.Removed.Channels, res.AlreadyGone.Channels = len(gone), len(obj.channels)-len(gone)
	if err := removeCategories(ctx, tx, obj, res); err != nil {
		return nil, nil, nil, err
	}
	if err := removeRoles(ctx, tx, obj, res); err != nil {
		return nil, nil, nil, err
	}
	if _, err := tx.Exec(ctx, `UPDATE discord_imports SET undone_at = NOW(), undone_by = $2 WHERE id = $1`, id, sub.ID); err != nil {
		return nil, nil, nil, fmt.Errorf("mark import undone: %w", err)
	}
	err = log.RecordTx(ctx, tx, audit.Entry{
		Action: "import.discord_undo", TargetType: "import", TargetID: id.String(), TargetName: rec.TemplateName,
		Details: map[string]any{
			"template": rec.TemplateCode, "categories": res.Removed.Categories, "channels": res.Removed.Channels,
			"roles": res.Removed.Roles, "already_gone_channels": res.AlreadyGone.Channels,
			"kept_categories": len(res.KeptCategories),
		},
	})
	if err != nil {
		return nil, nil, nil, fmt.Errorf("audit undo: %w", err)
	}
	return res, keys, gone, nil
}

// removeChannels deletes the created channels that still exist and returns the
// storage keys of their attachments and the ids removed.
func removeChannels(ctx context.Context, tx pgx.Tx, chans []chanInfo) ([]string, []uuid.UUID, error) {
	ids := make([]uuid.UUID, len(chans))
	for i, c := range chans {
		ids[i] = c.ID
	}
	if len(ids) == 0 {
		return nil, nil, nil
	}
	rows, err := tx.Query(ctx, `
		SELECT s3_key FROM media WHERE message_id IN (SELECT id FROM messages WHERE channel_id = ANY($1))`, ids)
	if err != nil {
		return nil, nil, fmt.Errorf("collect media: %w", err)
	}
	keys, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, nil, fmt.Errorf("collect media: %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM channels WHERE id = ANY($1)`, ids); err != nil {
		return nil, nil, fmt.Errorf("delete channels: %w", err)
	}
	return keys, ids, nil
}

func removeCategories(ctx context.Context, tx pgx.Tx, obj objectIDs, res *UndoResult) error {
	kept, err := keptCategories(ctx, tx, obj.categories, []uuid.UUID{})
	if err != nil {
		return err
	}
	res.KeptCategories = kept
	tag, err := tx.Exec(ctx, `
		DELETE FROM categories ca WHERE ca.id = ANY($1)
		AND NOT EXISTS (SELECT 1 FROM channels ch WHERE ch.category_id = ca.id)`, obj.categories)
	if err != nil {
		return fmt.Errorf("delete categories: %w", err)
	}
	res.Removed.Categories = int(tag.RowsAffected())
	res.AlreadyGone.Categories = max(len(obj.categories)-res.Removed.Categories-len(kept), 0)
	return nil
}

// removeRoles deletes the created roles that still exist and closes the gap
// they leave, keeping the order of the others.
func removeRoles(ctx context.Context, tx pgx.Tx, obj objectIDs, res *UndoResult) error {
	if len(obj.roles) == 0 {
		return nil
	}
	tag, err := tx.Exec(ctx, `DELETE FROM roles WHERE id = ANY($1) AND NOT is_everyone AND NOT managed`, obj.roles)
	if err != nil {
		return fmt.Errorf("delete roles: %w", err)
	}
	res.Removed.Roles = int(tag.RowsAffected())
	res.AlreadyGone.Roles = len(obj.roles) - res.Removed.Roles
	if res.Removed.Roles == 0 {
		return nil
	}
	if _, err := tx.Exec(ctx, `
		UPDATE roles r SET position = n.rn
		FROM (SELECT id, ROW_NUMBER() OVER (ORDER BY position, created_at, id) AS rn FROM roles WHERE NOT is_everyone) n
		WHERE r.id = n.id AND r.position <> n.rn`); err != nil {
		return fmt.Errorf("close role gaps: %w", err)
	}
	return nil
}
