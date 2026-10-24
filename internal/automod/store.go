package automod

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

var (
	errRuleNotFound = httpx.ErrNotFound("automod rule not found")
	errTooManyRules = httpx.ErrConflict(fmt.Sprintf("at most %d automod rules", MaxRules))
	errOneRaidRule  = httpx.ErrConflict("there is only one raid protection rule; edit it")
)

const ruleColumns = `id, kind, name, enabled, action, timeout_seconds, config, exempt_roles, exempt_channels, updated_at`

func scanRule(row pgx.CollectableRow) (Rule, error) {
	var r Rule
	var raw []byte
	if err := row.Scan(&r.ID, &r.Kind, &r.Name, &r.Enabled, &r.Action, &r.TimeoutSeconds, &raw, &r.ExemptRoles, &r.ExemptChannels, &r.UpdatedAt); err != nil {
		return r, err
	}
	if err := json.Unmarshal(raw, &r.Config); err != nil {
		return r, fmt.Errorf("automod rule %s: %w", r.ID, err)
	}
	if r.ExemptRoles == nil {
		r.ExemptRoles = []uuid.UUID{}
	}
	if r.ExemptChannels == nil {
		r.ExemptChannels = []uuid.UUID{}
	}
	return r, nil
}

// ListRules returns every rule, oldest first, and the version of the set.
func ListRules(ctx context.Context, q db.Querier) ([]Rule, int64, error) {
	var version int64
	if err := q.QueryRow(ctx, `SELECT version FROM automod_state`).Scan(&version); err != nil {
		return nil, 0, fmt.Errorf("automod version: %w", err)
	}
	rows, err := q.Query(ctx, `SELECT `+ruleColumns+` FROM automod_rules ORDER BY created_at, id`)
	if err != nil {
		return nil, 0, fmt.Errorf("list automod rules: %w", err)
	}
	rules, err := pgx.CollectRows(rows, scanRule)
	if err != nil {
		return nil, 0, fmt.Errorf("list automod rules: %w", err)
	}
	if rules == nil {
		rules = []Rule{}
	}
	return rules, version, nil
}

// bump records a change to the rules, so every compiled copy loads them again.
func bump(ctx context.Context, tx pgx.Tx) error {
	_, err := tx.Exec(ctx, `UPDATE automod_state SET version = version + 1`)
	return err
}

// CreateRule stores a validated rule.
func CreateRule(ctx context.Context, p *db.Pool, in RuleInput) (Rule, error) {
	var out Rule
	err := pgx.BeginFunc(ctx, p, func(tx pgx.Tx) error {
		// The state row serializes writers, so the limits below hold.
		if _, err := tx.Exec(ctx, `SELECT 1 FROM automod_state FOR UPDATE`); err != nil {
			return err
		}
		var n int
		var raid bool
		if err := tx.QueryRow(ctx, `SELECT COUNT(*), COALESCE(BOOL_OR(kind = 'raid'), FALSE) FROM automod_rules`).Scan(&n, &raid); err != nil {
			return err
		}
		if n >= MaxRules {
			return errTooManyRules
		}
		if in.Kind == KindRaid && raid {
			return errOneRaidRule
		}
		cfg, _ := json.Marshal(in.Config)
		rows, err := tx.Query(ctx, `
			INSERT INTO automod_rules (kind, name, enabled, action, timeout_seconds, config, exempt_roles, exempt_channels)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING `+ruleColumns,
			in.Kind, in.Name, in.Enabled, in.Action, in.TimeoutSeconds, cfg, in.ExemptRoles, in.ExemptChannels)
		if err != nil {
			return err
		}
		if out, err = pgx.CollectExactlyOneRow(rows, scanRule); err != nil {
			return err
		}
		return bump(ctx, tx)
	})
	return out, err
}

// UpdateRule replaces a rule; its kind cannot change.
func UpdateRule(ctx context.Context, p *db.Pool, id uuid.UUID, in RuleInput) (Rule, error) {
	var out Rule
	err := pgx.BeginFunc(ctx, p, func(tx pgx.Tx) error {
		cfg, _ := json.Marshal(in.Config)
		rows, err := tx.Query(ctx, `
			UPDATE automod_rules SET name = $3, enabled = $4, action = $5, timeout_seconds = $6, config = $7,
			       exempt_roles = $8, exempt_channels = $9, updated_at = NOW()
			WHERE id = $1 AND kind = $2 RETURNING `+ruleColumns,
			id, in.Kind, in.Name, in.Enabled, in.Action, in.TimeoutSeconds, cfg, in.ExemptRoles, in.ExemptChannels)
		if err != nil {
			return err
		}
		out, err = pgx.CollectExactlyOneRow(rows, scanRule)
		if errors.Is(err, pgx.ErrNoRows) {
			return ruleMissingOrOtherKind(ctx, tx, id)
		}
		if err != nil {
			return err
		}
		return bump(ctx, tx)
	})
	return out, err
}

func ruleMissingOrOtherKind(ctx context.Context, q db.Querier, id uuid.UUID) error {
	var exists bool
	if err := q.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM automod_rules WHERE id = $1)`, id).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return httpx.ErrInvalidInput("the kind of a rule cannot change; create a new rule")
	}
	return errRuleNotFound
}

// DeleteRule removes a rule. Queue entries it made keep its name.
func DeleteRule(ctx context.Context, p *db.Pool, id uuid.UUID) (Rule, error) {
	var out Rule
	err := pgx.BeginFunc(ctx, p, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `DELETE FROM automod_rules WHERE id = $1 RETURNING `+ruleColumns, id)
		if err != nil {
			return err
		}
		if out, err = pgx.CollectExactlyOneRow(rows, scanRule); errors.Is(err, pgx.ErrNoRows) {
			return errRuleNotFound
		} else if err != nil {
			return err
		}
		return bump(ctx, tx)
	})
	return out, err
}
