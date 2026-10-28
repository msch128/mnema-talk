package moderation

import (
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
)

func TestNoteDecorate(t *testing.T) {
	me := &auth.User{ID: uuid.New()}
	cases := []struct {
		name       string
		author     uuid.UUID
		authorName string
		admin      bool
		mine, del  bool
		wantName   string
	}{
		{"own note", me.ID, "Me", false, true, true, "Me"},
		{"foreign note", uuid.New(), "Other", false, false, false, "Other"},
		{"administrator deletes foreign note", uuid.New(), "Other", true, false, true, "Other"},
		{"deleted author", uuid.New(), "", false, false, false, deletedAuthorName},
	}
	for _, c := range cases {
		n := Note{AuthorID: c.author, AuthorName: c.authorName}
		n.decorate(me, c.admin)
		if n.Mine != c.mine || n.CanDelete != c.del || n.AuthorName != c.wantName {
			t.Errorf("%s: got mine=%v delete=%v name=%q", c.name, n.Mine, n.CanDelete, n.AuthorName)
		}
	}
}
