//go:build integration

package communityevents

import (
	"fmt"
	"net/http"
	"testing"
)

func TestAMemberWhoLostSightOfTheTalkCanStillLeaveTheirSignUp(t *testing.T) {
	f := newRosterFixture(t)
	member := f.user(t, "member", "user")
	roles := f.setRoles(t, role("Tank", 1))
	if _, err := f.call(t, member, f.h.signUp, http.MethodPut, fmt.Sprintf(`{"role_id":%q}`, roles[0].ID), nil); err != nil {
		t.Fatal(err)
	}
	f.privateTalk(t)
	if rec, err := f.call(t, member, f.h.leaveSignup, http.MethodDelete, "", nil); err != nil || rec.Code != http.StatusNoContent || rec.Body.Len() != 0 {
		t.Fatalf("leaving frees the slot and reveals nothing of the event: %v %d %s", err, rec.Code, rec.Body.String())
	}
}
