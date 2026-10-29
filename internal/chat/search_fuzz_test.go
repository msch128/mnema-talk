package chat

import (
	"strings"
	"testing"

	"github.com/google/uuid"
)

// FuzzBuildSearchSQL checks that whatever the text and the filters are, the
// builder never panics and the SQL text depends only on the shape of the
// query (how many terms, which are excluded, which filters are set), never on
// user bytes: those
// travel as arguments.
func FuzzBuildSearchSQL(f *testing.F) {
	for _, s := range []string{"", "hello", "a b c d e f g h i j", "100%", "_", `\`, "'; DROP TABLE messages; --", "$1 $2", "\x00", "\u00e9 \u00fc \u00df", "💥 emoji", strings.Repeat("x ", 300)} {
		f.Add(s, "", "")
	}
	for _, has := range []string{"file", "image", "video", "link", "poll", "edit", "FILE", "x'--", "file;"} {
		f.Add("t", has, "")
	}
	f.Add("t", "", "pinned")
	f.Add("t", "", "pinned' OR '1'='1")
	f.Fuzz(func(t *testing.T, text, has, is string) {
		id := uuid.New()
		q := SearchQuery{Text: text, Has: has, Is: is, ChannelID: &id, Limit: 10, Visible: []uuid.UUID{id}}
		sql, args, err := buildSearchSQL(q)
		if err != nil {
			if sql != "" || args != nil {
				t.Fatalf("error with output: %v", err)
			}
			return
		}
		if (has != "" && !validHas(has)) || (is != "" && is != "pinned") {
			t.Fatalf("filter %q/%q accepted", has, is)
		}
		// The shape is the parsed terms: how many, and which are excluded
		// (-term). Their text is replaced by a fixed word.
		terms := parseSearchTerms(text)
		if len(terms) > maxSearchTerms {
			terms = terms[:maxSearchTerms]
		}
		canon := make([]string, len(terms))
		for i, term := range terms {
			canon[i] = "a"
			if term.Exclude {
				canon[i] = "-a"
			}
		}
		q.Text = strings.Join(canon, " ")
		want, wantArgs, err := buildSearchSQL(q)
		if err != nil {
			t.Fatalf("canonical query rejected: %v", err)
		}
		if sql != want || len(args) != len(wantArgs) {
			t.Fatalf("SQL depends on user text:\n%s\n---\n%s", sql, want)
		}
	})
}

func validHas(has string) bool {
	switch has {
	case "file", "image", "video", "link", "poll", "edit":
		return true
	}
	return false
}

// FuzzSearchWindow checks the search time parser and the window check never
// panic and agree with each other.
func FuzzSearchWindow(f *testing.F) {
	for _, s := range []string{"", "2026-03-04T05:06:07Z", "2026-03-04T05:06:07+02:00", "2026-13-01T00:00:00Z", "0000-01-01T00:00:00Z", "9999-12-31T23:59:59.999999999Z", "yesterday", "\x00"} {
		f.Add(s, s)
	}
	f.Fuzz(func(t *testing.T, a, b string) {
		after, errA := ParseSearchTime("after", a)
		before, errB := ParseSearchTime("before_date", b)
		if (a == "") != (after == nil && errA == nil) {
			t.Fatalf("empty input handled wrongly: %q", a)
		}
		if errA != nil || errB != nil || after == nil || before == nil {
			return
		}
		err := validateSearchDates(SearchQuery{After: after, BeforeDate: before})
		if after.Before(*before) != (err == nil) {
			t.Fatalf("window check disagrees for %v %v", after, before)
		}
	})
}

// FuzzLikePattern checks that wildcard characters in a term are always escaped.
func FuzzLikePattern(f *testing.F) {
	for _, s := range []string{"", "%", "_", `\`, `\%`, "a%b_c", "%%__\\\\"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, term string) {
		p := likePattern(term)
		if !strings.HasPrefix(p, "%") || !strings.HasSuffix(p, "%") || len(p) < 2 {
			t.Fatalf("pattern %q lacks its wildcards", p)
		}
		if got, ok := unescapeLike(p[1 : len(p)-1]); !ok || got != term {
			t.Fatalf("pattern %q does not round trip to %q", p, term)
		}
	})
}

// unescapeLike undoes the escaping; ok is false for an unescaped wildcard or a dangling escape.
func unescapeLike(s string) (string, bool) {
	var out strings.Builder
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\\':
			i++
			if i >= len(s) {
				return "", false
			}
		case '%', '_':
			return "", false
		}
		out.WriteByte(s[i])
	}
	return out.String(), true
}

// FuzzParseMentions checks the mention scanner never panics and only returns
// well-formed, unique, lower-case names.
func FuzzParseMentions(f *testing.F) {
	for _, s := range []string{"", "@all", "hi @alice.", "@a", "@@@@bob", "mail@host.com", "@" + strings.Repeat("x", 100), "@\u00dcn\u00ef", "x@yyy @yyy"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, content string) {
		seen := map[string]bool{}
		for _, n := range ParseMentions(content) {
			if len(n) < 2 || len(n) > 32 || n != strings.ToLower(n) || seen[n] || strings.HasSuffix(n, ".") || strings.HasSuffix(n, "-") {
				t.Fatalf("bad mention %q from %q", n, content)
			}
			seen[n] = true
		}
	})
}
