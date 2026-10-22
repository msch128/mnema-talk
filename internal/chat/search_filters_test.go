package chat

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestParseSearchTime(t *testing.T) {
	if got, err := ParseSearchTime("after", ""); got != nil || err != nil {
		t.Fatalf("empty: %v %v", got, err)
	}
	got, err := ParseSearchTime("after", "2026-03-04T05:06:07+02:00")
	if err != nil || got == nil || got.UTC().Hour() != 3 {
		t.Fatalf("valid: %v %v", got, err)
	}
	for _, bad := range []string{"2026-03-04", "yesterday", "2026-13-01T00:00:00Z"} {
		if _, err := ParseSearchTime("after", bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestSearchDateWindowMustBeNonEmpty(t *testing.T) {
	a := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	b := a.Add(time.Hour)
	if _, _, err := buildSearchSQL(SearchQuery{After: &a, BeforeDate: &b, Limit: 5}); err != nil {
		t.Fatalf("valid window: %v", err)
	}
	for _, w := range [][2]*time.Time{{&b, &a}, {&a, &a}} {
		if _, _, err := buildSearchSQL(SearchQuery{After: w[0], BeforeDate: w[1], Limit: 5}); err == nil {
			t.Fatal("empty window accepted")
		}
	}
}

func TestSearchNewFiltersCountAsCriteriaAndStayParameterised(t *testing.T) {
	u := uuid.New()
	sql, args, err := buildSearchSQL(SearchQuery{Mentions: &u, Has: "video", Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sql, "message_mentions") || !strings.Contains(sql, "video/%") {
		t.Fatalf("sql: %s", sql)
	}
	if strings.Contains(sql, u.String()) || len(args) != 2 {
		t.Fatalf("mentions must be an argument: %v", args)
	}
	d := time.Now()
	for _, q := range []SearchQuery{{After: &d}, {BeforeDate: &d}} {
		q.Limit = 5
		if _, _, err := buildSearchSQL(q); err != nil {
			t.Fatalf("date-only search: %v", err)
		}
	}
	if _, _, err := buildSearchSQL(SearchQuery{Limit: 5}); err == nil {
		t.Fatal("empty search accepted")
	}
}
