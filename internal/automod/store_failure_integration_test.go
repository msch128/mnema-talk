//go:build integration

package automod

import (
	"context"
	"testing"
)

func TestAStoredRuleThatCannotBeReadFailsTheListing(t *testing.T) {
	p := newStore(t)
	ctx := context.Background()
	_, err := p.Exec(ctx, `INSERT INTO automod_rules (kind, name, action, config) VALUES ('words', 'broken', 'block', '[1]'::jsonb)`)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := ListRules(ctx, p); err == nil {
		t.Fatal("a rule with an unreadable configuration was listed")
	}
}

func TestStoreCallsReportADatabaseFailure(t *testing.T) {
	for _, table := range []string{"automod_state", "automod_rules"} {
		t.Run(table, func(t *testing.T) {
			p := newStore(t)
			ctx := context.Background()
			rule, err := CreateRule(ctx, p, wordsInput("kept", "spam"))
			if err != nil {
				t.Fatal(err)
			}
			rename := func(from, to string) {
				if _, err := p.Exec(ctx, `ALTER TABLE `+from+` RENAME TO `+to); err != nil {
					t.Fatal(err)
				}
			}
			rename(table, table+"_off")
			defer rename(table+"_off", table)

			calls := map[string]func() error{
				"list":   func() error { _, _, err := ListRules(ctx, p); return err },
				"create": func() error { _, err := CreateRule(ctx, p, wordsInput("x", "y")); return err },
				"update": func() error { _, err := UpdateRule(ctx, p, rule.ID, wordsInput("x", "y")); return err },
				"delete": func() error { _, err := DeleteRule(ctx, p, rule.ID); return err },
			}
			for name, call := range calls {
				if call() == nil {
					t.Errorf("%s succeeded without the %s table", name, table)
				}
			}
		})
	}
}
