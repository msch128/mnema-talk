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

func TestParseSearchDay(t *testing.T) {
	if s, e, err := ParseSearchDay("", ""); s != nil || e != nil || err != nil {
		t.Fatalf("empty: %v %v %v", s, e, err)
	}
	s, e, err := ParseSearchDay("2026-10-24", "+02:00")
	if err != nil || s == nil || e == nil {
		t.Fatalf("valid: %v %v %v", s, e, err)
	}
	// 2026-10-24 00:00:00 +02:00 is 2026-10-23 22:00:00 UTC
	if s.UTC().Day() != 23 || s.UTC().Hour() != 22 {
		t.Fatalf("start UTC: %v", s.UTC())
	}
	if e.UTC().Day() != 24 || e.UTC().Hour() != 22 {
		t.Fatalf("end UTC: %v", e.UTC())
	}
	// Test integer minutes offset (120 minutes = +02:00)
	s2, e2, err := ParseSearchDay("2026-10-24", "120")
	if err != nil || !s2.Equal(*s) || !e2.Equal(*e) {
		t.Fatalf("minutes offset mismatch: %v %v %v", s2, e2, err)
	}
	// Test UTC and Z
	for _, z := range []string{"", "Z", "UTC", "+00:00"} {
		sz, ez, err := ParseSearchDay("2026-10-24", z)
		if err != nil || sz.UTC().Hour() != 0 || ez.Sub(*sz) != 24*time.Hour {
			t.Fatalf("z=%q: %v %v %v", z, sz, ez, err)
		}
	}
	// Bad inputs
	for _, bad := range []string{"2026-13-01", "not-a-date", "2026-02-31"} {
		if _, _, err := ParseSearchDay(bad, ""); err == nil {
			t.Errorf("accepted bad date %q", bad)
		}
	}
	for _, badTz := range []string{"bad-tz", "+25:00", "+02:70"} {
		if _, _, err := ParseSearchDay("2026-10-24", badTz); err == nil {
			t.Errorf("accepted bad tz %q", badTz)
		}
	}
}

func TestSearchFiltersV3SQLAndCriteria(t *testing.T) {
	d := time.Now().UTC()
	de := d.Add(24 * time.Hour)
	// Each filter counts as criteria alone
	for name, q := range map[string]SearchQuery{
		"during":   {DuringStart: &d, DuringEnd: &de, Limit: 5},
		"filename": {Filename: "report.pdf", Limit: 5},
		"author":   {AuthorType: "bot", Limit: 5},
		"embed":    {Has: "embed", Limit: 5},
	} {
		if _, _, err := buildSearchSQL(q); err != nil {
			t.Fatalf("%s alone: %v", name, err)
		}
	}

	// Filename escaping and parameterisation
	sql, args, err := buildSearchSQL(SearchQuery{Filename: "a_100%", Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sql, "NOT f.is_deleted AND f.original_filename ILIKE $1 ESCAPE '\\'") || len(args) != 2 {
		t.Fatalf("filename sql: %s args: %v", sql, args)
	}
	if args[0] != "%a\\_100\\%%" {
		t.Fatalf("filename likePattern escaped: %v", args[0])
	}

	// Author types
	for _, typ := range []string{"bot", "webhook", "member"} {
		sql, _, err := buildSearchSQL(SearchQuery{AuthorType: typ, Limit: 5})
		if err != nil {
			t.Fatalf("author=%s: %v", typ, err)
		}
		if typ == "bot" && (!strings.Contains(sql, "u.is_bot") || !strings.Contains(sql, "NOT LIKE 'wh-%'")) {
			t.Fatalf("author=bot sql: %s", sql)
		}
		if typ == "webhook" && (!strings.Contains(sql, "u.is_bot") || !strings.Contains(sql, "LIKE 'wh-%'")) {
			t.Fatalf("author=webhook sql: %s", sql)
		}
		if typ == "member" && !strings.Contains(sql, "NOT u.is_bot") {
			t.Fatalf("author=member sql: %s", sql)
		}
	}
	if _, _, err := buildSearchSQL(SearchQuery{AuthorType: "alien", Limit: 5}); err == nil {
		t.Fatal("invalid author type accepted")
	}

	// Sorts and pagination with before
	msgID := uuid.New()
	sqlNew, _, err := buildSearchSQL(SearchQuery{Text: "hi", Before: &msgID, Sort: "newest", Limit: 5})
	if err != nil || !strings.Contains(sqlNew, "ORDER BY m.created_at DESC") || !strings.Contains(sqlNew, "< (SELECT") {
		t.Fatalf("sort newest: %v, sql: %s", err, sqlNew)
	}
	sqlOld, _, err := buildSearchSQL(SearchQuery{Text: "hi", Before: &msgID, Sort: "oldest", Limit: 5})
	if err != nil || !strings.Contains(sqlOld, "ORDER BY m.created_at ASC") || !strings.Contains(sqlOld, "> (SELECT") {
		t.Fatalf("sort oldest: %v, sql: %s", err, sqlOld)
	}
	sqlRel, _, err := buildSearchSQL(SearchQuery{Text: "hi", Sort: "relevance", Limit: 5})
	if err != nil || !strings.Contains(sqlRel, "ORDER BY m.created_at DESC") {
		t.Fatalf("sort relevance: %v, sql: %s", err, sqlRel)
	}
	if _, _, err := buildSearchSQL(SearchQuery{Text: "hi", Sort: "bad", Limit: 5}); err == nil {
		t.Fatal("invalid sort accepted")
	}
}

func TestParseSearchTerms(t *testing.T) {
	cases := []struct {
		in   string
		want []searchTerm
	}{
		{"", nil},
		{"a  b", []searchTerm{{"a", false}, {"b", false}}},
		{`"short status" point`, []searchTerm{{"short status", false}, {"point", false}}},
		{"status -Release", []searchTerm{{"status", false}, {"Release", true}}},
		{`-"old status"`, []searchTerm{{"old status", true}}},
		{"- a-b", []searchTerm{{"-", false}, {"a-b", false}}},
		{`"-x"`, []searchTerm{{"-x", false}}},
		{`"open without end`, []searchTerm{{"open without end", false}}},
		{`"" x`, []searchTerm{{"x", false}}},
	}
	for _, c := range cases {
		got := parseSearchTerms(c.in)
		if len(got) != len(c.want) {
			t.Fatalf("%q: got %v want %v", c.in, got, c.want)
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Fatalf("%q: got %v want %v", c.in, got, c.want)
			}
		}
	}
}

func TestBuildSearchSQLPhraseAndExclusion(t *testing.T) {
	sql, args, err := buildSearchSQL(SearchQuery{Text: `"short status" -Release`})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sql, "m.content ILIKE") || !strings.Contains(sql, "m.content NOT ILIKE") {
		t.Fatalf("sql lacks include/exclude: %s", sql)
	}
	var phrase, excluded bool
	for _, a := range args {
		phrase = phrase || a == "%short status%"
		excluded = excluded || a == "%Release%"
	}
	if !phrase || !excluded {
		t.Fatalf("args %v", args)
	}
	if _, _, err := buildSearchSQL(SearchQuery{Text: "-Release"}); err == nil {
		t.Fatal("an exclusion alone must not be a search")
	}
}

// "-300" reads as hh=3 mm=00 as an ISO offset but as 300 minutes as a numeric
// one; a signed run of three digits is refused instead of guessed.
func TestSearchDayRefusesAmbiguousSignedOffsets(t *testing.T) {
	for _, tz := range []string{"-300", "+120", "+530"} {
		if _, _, err := ParseSearchDay("2026-10-24", tz); err == nil {
			t.Errorf("accepted ambiguous tz %q", tz)
		}
	}
	for tz, wantHours := range map[string]int{"-0500": -5, "-05:00": -5, "+5": 5, "-12": -12, "120": 2} {
		s, _, err := ParseSearchDay("2026-10-24", tz)
		if err != nil {
			t.Errorf("tz %q: %v", tz, err)
			continue
		}
		if got := s.Add(time.Duration(wantHours) * time.Hour).Hour(); got != 0 {
			t.Errorf("tz %q: day starts at %v UTC, want local midnight at %+d h", tz, s, wantHours)
		}
	}
}

// Every media filter leaves out deleted files, so has:image and filename: agree.
func TestSearchMediaFiltersIgnoreDeletedFiles(t *testing.T) {
	for _, q := range []SearchQuery{{Has: "file"}, {Has: "image"}, {Has: "video"}, {Filename: "cat"}} {
		q.Limit = 5
		sql, _, err := buildSearchSQL(q)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(sql, "NOT f.is_deleted") {
			t.Errorf("%+v lets deleted files match: %s", q, sql)
		}
	}
}

// Relevance has no ranking: it orders and pages like newest.
func TestSearchRelevanceSortsLikeNewest(t *testing.T) {
	before := uuid.New()
	for _, sort := range []string{"", "newest", "relevance"} {
		sql, _, err := buildSearchSQL(SearchQuery{Text: "hi", Sort: sort, Before: &before, Limit: 5})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(sql, "ORDER BY m.created_at DESC") || !strings.Contains(sql, "(m.created_at, m.id) <") {
			t.Errorf("sort %q: %s", sort, sql)
		}
	}
}
