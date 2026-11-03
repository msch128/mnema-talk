package chat

import (
	"strings"
	"testing"
)

func TestBlockedAuthorsStayOutOfCounts(t *testing.T) {
	inbox := "NOT EXISTS (SELECT 1 FROM user_blocks ub WHERE ub.blocker_id = $1 AND ub.blocked_id = m.user_id)"
	forum := "NOT EXISTS (SELECT 1 FROM user_blocks ub WHERE ub.blocker_id = $2 AND ub.blocked_id = x.user_id)"
	if authorHiddenFrom("$1", "m.user_id") != inbox {
		t.Fatalf("inbox rule = %q", authorHiddenFrom("$1", "m.user_id"))
	}
	if authorHiddenFrom("$2", "x.user_id") != forum {
		t.Fatalf("forum rule = %q", authorHiddenFrom("$2", "x.user_id"))
	}
	if !strings.Contains(inboxSQL(), inbox) {
		t.Fatal("mentions inbox no longer drops a blocked author")
	}
	if !strings.Contains(readStateSQL(), inbox) {
		t.Fatal("unread counts no longer drop a blocked author")
	}
	if !strings.Contains(unreadPostsSQL(), forum) {
		t.Fatal("forum unread list no longer drops a blocked author")
	}
	for _, author := range []string{"m.user_id", "um.user_id"} {
		if !strings.Contains(dmSelect, blockExists("$1", author)) {
			t.Fatalf("conversation list dropped the block rule for %s", author)
		}
	}
}
