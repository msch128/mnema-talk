package communityevents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// MaxRoleTemplates is the most role templates a server keeps.
const MaxRoleTemplates = 50

const templateColumns = `id, name, roles, enabled, preset`

func scanTemplate(row pgx.Row) (RoleTemplate, error) {
	var t RoleTemplate
	var roles []byte
	if err := row.Scan(&t.ID, &t.Name, &roles, &t.Enabled, &t.Preset); err != nil {
		return t, err
	}
	if err := json.Unmarshal(roles, &t.Roles); err != nil {
		return t, fmt.Errorf("decode role template: %w", err)
	}
	if t.Roles == nil {
		t.Roles = []RoleInput{}
	}
	return t, nil
}

// ListTemplates returns the role templates; without all only the enabled ones.
func (s *Store) ListTemplates(ctx context.Context, all bool) ([]RoleTemplate, error) {
	rows, err := s.DB.Query(ctx, `SELECT `+templateColumns+` FROM event_role_templates WHERE enabled OR $1 ORDER BY preset, name`, all)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []RoleTemplate{}
	for rows.Next() {
		t, err := scanTemplate(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func cleanTemplate(name string, roles []RoleInput) (string, []RoleInput, error) {
	n, err := httpx.CleanText("name", name, 60, true)
	if err != nil {
		return "", nil, err
	}
	clean, err := validateRoles(stripIDs(roles))
	return n, clean, err
}

func stripIDs(in []RoleInput) []RoleInput {
	out := make([]RoleInput, len(in))
	for i, r := range in {
		r.ID = nil
		out[i] = r
	}
	return out
}

// CreateTemplate adds an enabled template. The count and the insert run in
// one transaction under a transaction-scoped advisory lock on the template
// set, so creates that arrive together cannot pass MaxRoleTemplates; the lock
// is held for those two statements and released at commit.
func (s *Store) CreateTemplate(ctx context.Context, name string, roles []RoleInput, enabled bool) (RoleTemplate, error) {
	n, clean, err := cleanTemplate(name, roles)
	if err != nil {
		return RoleTemplate{}, err
	}
	raw, _ := json.Marshal(clean)
	var t RoleTemplate
	err = s.inTx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('mnema.event_role_templates'))`); err != nil {
			return fmt.Errorf("lock role templates: %w", err)
		}
		var count int
		if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM event_role_templates`).Scan(&count); err != nil {
			return err
		}
		if count >= MaxRoleTemplates {
			return httpx.ErrConflict(fmt.Sprintf("there can be at most %d role templates", MaxRoleTemplates))
		}
		t, err = scanTemplate(tx.QueryRow(ctx, `INSERT INTO event_role_templates (name, roles, enabled) VALUES ($1, $2, $3) RETURNING `+templateColumns, n, raw, enabled))
		return err
	})
	if db.IsUniqueViolation(err) {
		return t, httpx.ErrConflict("a template called " + n + " exists")
	}
	return t, err
}

// UpdateTemplate changes the fields that are set.
func (s *Store) UpdateTemplate(ctx context.Context, id uuid.UUID, req RoleTemplateRequest) (RoleTemplate, error) {
	cur, err := scanTemplate(s.DB.QueryRow(ctx, `SELECT `+templateColumns+` FROM event_role_templates WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return cur, httpx.ErrNotFound("template not found")
	}
	if err != nil {
		return cur, err
	}
	name, roles := cur.Name, cur.Roles
	if req.Name != nil {
		name = *req.Name
	}
	if req.Roles != nil {
		roles = *req.Roles
	}
	if req.Preset(cur) {
		return cur, httpx.ErrConflict("a preset can be enabled or disabled, not changed")
	}
	n, clean, err := cleanTemplate(name, roles)
	if err != nil {
		return cur, err
	}
	enabled := cur.Enabled
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	raw, _ := json.Marshal(clean)
	t, err := scanTemplate(s.DB.QueryRow(ctx, `UPDATE event_role_templates SET name = $2, roles = $3, enabled = $4 WHERE id = $1 RETURNING `+templateColumns, id, n, raw, enabled))
	if db.IsUniqueViolation(err) {
		return t, httpx.ErrConflict("a template called " + n + " exists")
	}
	return t, err
}

// Preset reports whether the request tries to change a shipped preset's content.
func (r RoleTemplateRequest) Preset(cur RoleTemplate) bool {
	return cur.Preset && (r.Name != nil || r.Roles != nil)
}

// DeleteTemplate removes a template that is not a preset.
func (s *Store) DeleteTemplate(ctx context.Context, id uuid.UUID) error {
	tag, err := s.DB.Exec(ctx, `DELETE FROM event_role_templates WHERE id = $1 AND NOT preset`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() > 0 {
		return nil
	}
	var preset bool
	err = s.DB.QueryRow(ctx, `SELECT preset FROM event_role_templates WHERE id = $1`, id).Scan(&preset)
	if errors.Is(err, pgx.ErrNoRows) {
		return httpx.ErrNotFound("template not found")
	}
	if err != nil {
		return err
	}
	return httpx.ErrConflict("a preset can be disabled, not deleted")
}
