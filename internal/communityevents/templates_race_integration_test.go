//go:build integration

package communityevents

import (
	"context"
	"fmt"
	"testing"

	"github.com/msch128/mnema-talk/internal/testutil/racetest"
)

// Lane 7 of the defect hunt: "there can be at most 50 role templates" under
// creates that arrive together, one short of the cap.
func TestParallelTemplateCreatesStayWithinTheCap(t *testing.T) {
	f := newRosterFixture(t)
	ctx := context.Background()
	const callers = 8
	count := func() int {
		var n int
		if err := f.p.QueryRow(ctx, `SELECT COUNT(*) FROM event_role_templates`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	roles := []RoleInput{{Name: "Tank"}}
	res := racetest.Sweep(t, racetest.Rounds(10, 300), callers,
		func(round int) {
			if _, err := f.p.Exec(ctx, `DELETE FROM event_role_templates WHERE NOT preset`); err != nil {
				t.Fatal(err)
			}
			for i := count(); i < MaxRoleTemplates-1; i++ {
				if _, err := f.s.CreateTemplate(ctx, fmt.Sprintf("filler %d", i), roles, true); err != nil {
					t.Fatal(err)
				}
			}
		},
		func(round, i int) error {
			_, err := f.s.CreateTemplate(ctx, fmt.Sprintf("racer %d %d", round, i), roles, true)
			return err
		},
		func(round int, r racetest.Round) string {
			if n := count(); n > MaxRoleTemplates {
				return fmt.Sprintf("round %d: %d templates, %d creates succeeded", round, n, r.Succeeded())
			}
			return ""
		})
	// Expected failure until H-L7-03 is fixed: the count and the insert are
	// two statements on the pool with no lock.
	res.KnownFailure(t, "H-L7-03")
}
