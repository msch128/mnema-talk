package chat

import (
	"strings"
	"testing"

	"github.com/msch128/mnema-talk/internal/chat/core"
	"github.com/msch128/mnema-talk/internal/chat/dm"
	"github.com/msch128/mnema-talk/internal/chat/readstate"
	"github.com/msch128/mnema-talk/internal/chat/search"
)

func TestBlockedAuthorsStayOutOfCounts(t *testing.T) {
	inbox := "NOT EXISTS (SELECT 1 FROM user_blocks ub WHERE ub.blocker_id = $1 AND ub.blocked_id = m.user_id)"
	forum := "NOT EXISTS (SELECT 1 FROM user_blocks ub WHERE ub.blocker_id = $2 AND ub.blocked_id = x.user_id)"
	if core.AuthorHiddenFrom("$1", "m.user_id") != inbox {
		t.Fatalf("inbox rule = %q", core.AuthorHiddenFrom("$1", "m.user_id"))
	}
	if core.AuthorHiddenFrom("$2", "x.user_id") != forum {
		t.Fatalf("forum rule = %q", core.AuthorHiddenFrom("$2", "x.user_id"))
	}
	if !strings.Contains(search.InboxSQL(), inbox) {
		t.Fatal("mentions inbox no longer drops a blocked author")
	}
	if !strings.Contains(readstate.ReadStateSQL(), inbox) {
		t.Fatal("unread counts no longer drop a blocked author")
	}
	if !strings.Contains(unreadPostsSQL(), forum) {
		t.Fatal("forum unread list no longer drops a blocked author")
	}
	for _, author := range []string{"m.user_id", "um.user_id"} {
		if !strings.Contains(dm.DmSelect, core.BlockExists("$1", author)) {
			t.Fatalf("conversation list dropped the block rule for %s", author)
		}
	}
}
