package chat

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestSearchHasPollEditAndIsPinned(t *testing.T) {
	for has, want := range map[string]string{"poll": "FROM polls", "edit": "m.is_edited"} {
		sql, _, err := buildSearchSQL(SearchQuery{Has: has, Limit: 5})
		if err != nil || !strings.Contains(sql, want) {
			t.Errorf("has=%s: %v %s", has, err, sql)
		}
	}
	sql, _, err := buildSearchSQL(SearchQuery{Is: "pinned", Limit: 5})
	if err != nil || !strings.Contains(sql, "m.is_pinned") {
		t.Errorf("is=pinned alone is a criterion: %v %s", err, sql)
	}
	if _, _, err := buildSearchSQL(SearchQuery{Is: "unread", Limit: 5}); err == nil {
		t.Error("an unknown is value was accepted")
	}
}

func TestEditHistoryViewsCountOncePerReaderAndMessageInAWindow(t *testing.T) {
	var v historyViews
	reader, other, msg := uuid.New(), uuid.New(), uuid.New()
	now := time.Now()
	if !v.first(reader, msg, now) || v.first(reader, msg, now.Add(time.Minute)) {
		t.Fatal("the second read inside the window counts again")
	}
	if !v.first(other, msg, now) {
		t.Error("another reader is a new entry")
	}
	if !v.first(reader, msg, now.Add(editViewAuditEvery+time.Second)) {
		t.Error("a read after the window is a new entry")
	}
}
