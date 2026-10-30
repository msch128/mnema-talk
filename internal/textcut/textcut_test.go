package textcut

import (
	"testing"
	"unicode/utf8"
)

const (
	family = "👨‍👩‍👧‍👦" // 7 runes
	flagDE = "🇩🇪"      // 2 runes
	wave   = "👋🏽"      // 2 runes
	eAcute = "é"      // 2 runes
)

func TestRunes(t *testing.T) {
	cases := []struct {
		name string
		in   string
		max  int
		want string
	}{
		{"fits", "hello", 5, "hello"},
		{"ascii cut", "hello", 3, "hel"},
		{"zero", "hello", 0, ""},
		{"cjk", "你好世界", 2, "你好"},
		{"zwj kept whole", "ab" + family, 9, "ab" + family},
		{"zwj dropped when it does not fit", "ab" + family + "c", 5, "ab"},
		{"cut before joiner", "ab" + family, 3, "ab"},
		{"skin tone stays with its base", "a" + wave, 2, "a"},
		{"skin tone kept whole", "a" + wave, 3, "a" + wave},
		{"combining mark stays with its base", "ab" + eAcute, 3, "ab"},
		{"combining mark kept whole", "ab" + eAcute, 4, "ab" + eAcute},
		{"flag pair not split", "a" + flagDE, 2, "a"},
		{"flag pair kept", "a" + flagDE + "b", 3, "a" + flagDE},
		{"two flags cut between them", flagDE + flagDE, 2, flagDE},
		{"joiner pulls its base out with it", "ab‍cd", 3, "a"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Runes(c.in, c.max)
			if got != c.want {
				t.Fatalf("Runes(%q, %d) = %q, want %q", c.in, c.max, got, c.want)
			}
			if !utf8.ValidString(got) {
				t.Fatalf("result is not valid UTF-8: %q", got)
			}
		})
	}
}

func TestBytes(t *testing.T) {
	cases := []struct {
		name string
		in   string
		max  int
		want string
	}{
		{"fits", "héllo", 6, "héllo"},
		{"inside a rune", "héllo", 2, "h"},
		{"cjk", "你好世界", 7, "你好"},
		{"zwj not split", "ab" + family, 2 + len("👨") + 1, "ab"},
		{"zwj kept whole", "ab" + family, 2 + len(family), "ab" + family},
		{"combining mark not split", "ab" + eAcute, 3, "ab"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Bytes(c.in, c.max)
			if got != c.want {
				t.Fatalf("Bytes(%q, %d) = %q, want %q", c.in, c.max, got, c.want)
			}
		})
	}
}
