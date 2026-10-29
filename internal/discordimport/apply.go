package discordimport

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/audit"
	"github.com/msch128/mnema-talk/internal/chat"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
)

// ConfirmWord is what the third confirmation accepts besides the server name.
const ConfirmWord = "IMPORT"

// Importer previews, applies and undoes imports.
type Importer struct {
	DB      *db.Pool
	Perms   *perm.Service
	Events  events.Publisher
	Audit   *audit.Log
	Fetcher *Fetcher
	// Objects deletes the media of channels an undo removes; nil skips it.
	Objects chat.ObjectDeleter
	// Voice empties the voice room of a removed Talk; nil skips it.
	Voice chat.VoiceRooms
	// Features reports whether this server has a feature (GET /api/health
	// features); nil means none, so stages and forums map to Talks and text.
	Features func(name string) bool

	fetcherOnce sync.Once
}

func (im *Importer) caps() Capabilities {
	if im.Features == nil {
		return Capabilities{}
	}
	return Capabilities{Forum: im.Features("channels.forum"), Stage: im.Features("talks.stage")}
}

func (im *Importer) fetcher() *Fetcher {
	im.fetcherOnce.Do(func() {
		if im.Fetcher == nil {
			im.Fetcher = NewFetcher()
		}
	})
	return im.Fetcher
}

var errNotAdmin = httpx.ErrForbidden("only the Owner and Administrators can import a server")

// inputs reads the facts a plan depends on. The roles come from snap and the
// rest through q, so the same call serves the preview (the pool) and the
// apply (inside the lock). A role_map that is not allowed fails here.
func (im *Importer) inputs(ctx context.Context, q db.Querier, snap *perm.Snapshot, sub perm.Subject, tpl *Template, roleMap map[string]string) (Inputs, error) {
	actor := snap.Global(sub)
	in := Inputs{Grantable: grantable(actor), Caps: im.caps(), Mappable: mappableRoles(snap, sub)}
	for _, r := range snap.Roles() {
		in.ExistingRoles = append(in.ExistingRoles, r.Name)
	}
	var err error
	if in.RoleMap, err = resolveRoleMap(tpl, snap, sub, roleMap); err != nil {
		return in, err
	}
	if err := q.QueryRow(ctx, `SELECT community_name FROM instance_settings`).Scan(&in.ServerName); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return in, fmt.Errorf("read community name: %w", err)
	}
	return in, nil
}

// Preview fetches the template and returns what importing it would create.
// It writes nothing.
func (im *Importer) Preview(ctx context.Context, sub perm.Subject, link string, roleMap map[string]string) (*Plan, error) {
	code, err := ParseCode(link)
	if err != nil {
		return nil, err
	}
	actor, err := im.Perms.Global(ctx, sub)
	if err != nil {
		return nil, err
	}
	if !actor.Has(perm.Administrator) {
		return nil, errNotAdmin
	}
	tpl, err := im.fetcher().Fetch(ctx, code)
	if err != nil {
		return nil, err
	}
	snap, err := im.Perms.Snapshot(ctx)
	if err != nil {
		return nil, err
	}
	in, err := im.inputs(ctx, im.DB, snap, sub, tpl, roleMap)
	if err != nil {
		return nil, err
	}
	return BuildPlan(tpl, in), nil
}

// ApplyRequest is the body of the apply call: the three confirmations and the
// hash of the preview they were given for.
type ApplyRequest struct {
	Template string `json:"template" maxLength:"300"`
	PlanHash string `json:"plan_hash" maxLength:"64"`
	// ConfirmAdditive: the administrator confirmed what is added.
	ConfirmAdditive bool `json:"confirm_additive"`
	// ConfirmRoles: the administrator confirmed the role permissions.
	ConfirmRoles bool `json:"confirm_roles"`
	// ConfirmText is the server name or IMPORT.
	ConfirmText string `json:"confirm_text" maxLength:"100"`
	// RoleMap maps Discord role ids onto existing role ids; those roles are
	// not created and keep their own permissions. It must be the map the
	// preview was made with.
	RoleMap map[string]string `json:"role_map,omitempty" binding:"optional"`
}

// confirmed checks the confirmation flags; Apply runs it before the download
// and check again under the lock.
func (r ApplyRequest) confirmed() error {
	if !r.ConfirmAdditive || !r.ConfirmRoles {
		return httpx.ErrInvalidInput("every confirmation step must be confirmed")
	}
	return nil
}

func (r ApplyRequest) check(plan *Plan) error {
	if err := r.confirmed(); err != nil {
		return err
	}
	text := strings.TrimSpace(r.ConfirmText)
	if text != ConfirmWord && (plan.ServerName == "" || !strings.EqualFold(text, plan.ServerName)) {
		return httpx.ErrInvalidInput("type the server name or " + ConfirmWord + " to confirm")
	}
	if r.PlanHash != plan.Hash {
		return httpx.ErrConflict("the template or this server changed since the preview; preview it again")
	}
	if plan.Counts == (Counts{}) {
		return httpx.ErrInvalidInput("the template contains nothing to import")
	}
	return nil
}

// Record is one import as stored.
type Record struct {
	ID            uuid.UUID  `json:"id" format:"uuid"`
	TemplateCode  string     `json:"template_code"`
	TemplateName  string     `json:"template_name"`
	CreatedByName string     `json:"created_by_name"`
	CreatedAt     time.Time  `json:"created_at" format:"date-time"`
	Counts        Counts     `json:"counts"`
	UndoneAt      *time.Time `json:"undone_at" format:"date-time" extensions:"x-nullable"`
}

// Apply runs the import in one transaction. The plan is recomputed from a
// fresh download and must match the preview the administrator confirmed.
func (im *Importer) Apply(ctx context.Context, sub perm.Subject, actorName string, req ApplyRequest) (*Record, error) {
	if err := req.confirmed(); err != nil {
		return nil, err
	}
	code, err := ParseCode(req.Template)
	if err != nil {
		return nil, err
	}
	tpl, err := im.fetcher().Fetch(ctx, code)
	if err != nil {
		return nil, err
	}
	var rec *Record
	err = im.Perms.Locked(ctx, sub, func(tx pgx.Tx, snap *perm.Snapshot) error {
		actor := snap.Global(sub)
		if !actor.Has(perm.Administrator) {
			return errNotAdmin
		}
		in, err := im.inputs(ctx, tx, snap, sub, tpl, req.RoleMap)
		if err != nil {
			return err
		}
		plan := BuildPlan(tpl, in)
		if err := req.check(plan); err != nil {
			return err
		}
		rec, err = im.insertPlan(ctx, tx, plan, sub, actorName)
		return err
	})
	if err != nil {
		return nil, err
	}
	im.Perms.Changed(nil)
	if im.Events != nil {
		im.Events.Broadcast(events.ChannelsChanged, nil)
		im.Events.Broadcast(events.RolesChanged, nil)
	}
	return rec, nil
}

// created collects the ids of everything one import inserted.
type created struct {
	roles      []uuid.UUID
	categories []uuid.UUID
	channels   []uuid.UUID
	roleBySrc  map[string]uuid.UUID
	catBySrc   map[string]uuid.UUID
	chanBySrc  map[string]uuid.UUID
}

func (im *Importer) insertPlan(ctx context.Context, tx pgx.Tx, plan *Plan, sub perm.Subject, actorName string) (*Record, error) {
	if err := chat.LockChannelLayout(ctx, tx); err != nil {
		return nil, err
	}
	c := &created{roleBySrc: map[string]uuid.UUID{}, catBySrc: map[string]uuid.UUID{}, chanBySrc: map[string]uuid.UUID{}}
	steps := []func(context.Context, pgx.Tx, *Plan, *created) error{insertRoles, insertCategories, insertChannels, insertOverwrites}
	for _, step := range steps {
		if err := step(ctx, tx, plan, c); err != nil {
			return nil, err
		}
	}
	rec, err := recordImport(ctx, tx, plan, c, sub, actorName)
	if err != nil {
		return nil, err
	}
	err = im.Audit.RecordTx(ctx, tx, audit.Entry{
		Action: "import.discord", TargetType: "import", TargetID: rec.ID.String(), TargetName: plan.TemplateName,
		Details: map[string]any{
			"template": plan.Code, "categories": plan.Counts.Categories, "channels": plan.Counts.Channels,
			"roles": plan.Counts.Roles, "mapped_roles": len(plan.MappedRoles), "administrator_dropped": plan.AdministratorDropped,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("audit import: %w", err)
	}
	return rec, nil
}

// insertRoles creates the roles below every existing role: the existing ones
// move up by the number of new roles, so their order is untouched.
func insertRoles(ctx context.Context, tx pgx.Tx, plan *Plan, c *created) error {
	n := len(plan.Roles)
	if n == 0 {
		return nil
	}
	var existing int
	if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM roles`).Scan(&existing); err != nil {
		return fmt.Errorf("count roles: %w", err)
	}
	if existing+n > perm.MaxRoles {
		return httpx.ErrConflict(fmt.Sprintf("too many roles: this server has %d and the import adds %d (at most %d)", existing, n, perm.MaxRoles))
	}
	if _, err := tx.Exec(ctx, `UPDATE roles SET position = position + $1 WHERE NOT is_everyone`, n); err != nil {
		return fmt.Errorf("make room for roles: %w", err)
	}
	ids, names, colors, positions, perms, hoists, mentionables := make([]uuid.UUID, n), make([]string, n), make([]int32, n), make([]int32, n), make([]int64, n), make([]bool, n), make([]bool, n)
	for i, r := range plan.Roles {
		ids[i], names[i], colors[i], positions[i] = uuid.New(), r.Name, int32(r.Color), int32(n-i)
		perms[i], hoists[i], mentionables[i] = int64(r.Permissions), r.Hoist, r.Mentionable
		c.roleBySrc[r.SourceID] = ids[i]
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO roles (id, name, color, position, permissions, hoist, mentionable)
		SELECT * FROM unnest($1::uuid[], $2::text[], $3::int[], $4::int[], $5::bigint[], $6::bool[], $7::bool[])`,
		ids, names, colors, positions, perms, hoists, mentionables)
	if err != nil {
		return fmt.Errorf("insert roles: %w", err)
	}
	c.roles = ids
	return nil
}

// insertCategories appends the categories after the existing ones.
func insertCategories(ctx context.Context, tx pgx.Tx, plan *Plan, c *created) error {
	n := len(plan.Categories)
	if n == 0 {
		return nil
	}
	var base int
	if err := tx.QueryRow(ctx, `SELECT COALESCE(MAX(sort_order), -1) + 1 FROM categories`).Scan(&base); err != nil {
		return fmt.Errorf("read category order: %w", err)
	}
	ids, names, orders := make([]uuid.UUID, n), make([]string, n), make([]int32, n)
	for i, cat := range plan.Categories {
		ids[i], names[i], orders[i] = uuid.New(), cat.Name, int32(base+i)
		c.catBySrc[cat.SourceID] = ids[i]
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO categories (id, name, sort_order)
		SELECT * FROM unnest($1::uuid[], $2::text[], $3::int[])`, ids, names, orders); err != nil {
		return fmt.Errorf("insert categories: %w", err)
	}
	c.categories = ids
	return nil
}

// recordImport stores the import and the exact ids it created.
func recordImport(ctx context.Context, tx pgx.Tx, plan *Plan, c *created, sub perm.Subject, actorName string) (*Record, error) {
	rec := &Record{TemplateCode: plan.Code, TemplateName: plan.TemplateName, CreatedByName: actorName, Counts: plan.Counts}
	err := tx.QueryRow(ctx, `
		INSERT INTO discord_imports (template_code, template_name, created_by, created_by_name, categories, channels, roles)
		VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id, created_at`,
		plan.Code, plan.TemplateName, sub.ID, actorName, plan.Counts.Categories, plan.Counts.Channels, plan.Counts.Roles).
		Scan(&rec.ID, &rec.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("record import: %w", err)
	}
	var kinds []string
	var objects []uuid.UUID
	for kind, ids := range map[string][]uuid.UUID{"role": c.roles, "category": c.categories, "channel": c.channels} {
		for _, id := range ids {
			kinds, objects = append(kinds, kind), append(objects, id)
		}
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO discord_import_objects (import_id, kind, object_id)
		SELECT $1, k, o FROM unnest($2::text[], $3::uuid[]) AS t(k, o)`, rec.ID, kinds, objects); err != nil {
		return nil, fmt.Errorf("record import objects: %w", err)
	}
	return rec, nil
}
