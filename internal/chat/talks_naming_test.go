package chat

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestLowestFreeTalkNumberIsPerBaseName(t *testing.T) {
	names := []string{"foo 1", "foo 3", "bar 2", "foo 2x", "foo  2", "fooo 2"}
	if got := lowestFreeTalkNumber("foo", names); got != 2 {
		t.Fatalf("foo = %d; want the gap at 2", got)
	}
	if got := lowestFreeTalkNumber("bar", names); got != 1 {
		t.Fatalf("bar = %d; want 1", got)
	}
	if got := lowestFreeTalkNumber("new", names); got != 1 {
		t.Fatalf("a renamed lobby starts afresh, got %d", got)
	}
}

func TestLowestFreeTalkNumberTreatsTheBaseLiterally(t *testing.T) {
	if got := lowestFreeTalkNumber("a.c (x)", []string{"abc (x) 1", "a.c (x) 1"}); got != 2 {
		t.Fatalf("got %d; regex characters must not match loosely", got)
	}
}

func TestTempTalkBaseKeepsTheNumberWithinTheNameLimit(t *testing.T) {
	if got := tempTalkBase("  foo "); got != "foo" {
		t.Fatalf("got %q", got)
	}
	for _, lobby := range []string{strings.Repeat("a", 200), strings.Repeat("\u00e4", 64), strings.Repeat("\U0001F600", 64), strings.Repeat("ab ", 40)} {
		base := tempTalkBase(lobby)
		name := base + " 50"
		if utf8.RuneCountInString(name) > MaxChannelNameLen {
			t.Fatalf("%q has %d runes", name, utf8.RuneCountInString(name))
		}
		if !utf8.ValidString(base) || strings.HasSuffix(base, " ") {
			t.Fatalf("bad base %q", base)
		}
	}
	short := strings.Repeat("x", 61)
	if tempTalkBase(short) != short {
		t.Fatalf("a name that fits is kept whole")
	}
}
