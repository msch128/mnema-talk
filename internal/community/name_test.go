package community

import (
	"context"
	"testing"
)

func TestFallbackUsesTheHost(t *testing.T) {
	cases := map[string]string{
		"https://chat.example.org":      "chat.example.org",
		"https://chat.example.org:8443": "chat.example.org",
		"":                              Product,
		"not a url":                     Product,
	}
	for in, want := range cases {
		if got := Fallback(in); got != want {
			t.Errorf("Fallback(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNilNamesReportsTheProduct(t *testing.T) {
	var n *Names
	if got := n.Get(context.Background()); got != Product {
		t.Fatalf("got %q", got)
	}
	if name, icon := n.Cached(); name != Product || icon != "" {
		t.Fatalf("cached: %q, %q", name, icon)
	}
	n.Forget()
}

func TestWithoutDatabaseTheFallbackShows(t *testing.T) {
	n := New(nil, "https://talk.example.org")
	if got := n.Get(context.Background()); got != "talk.example.org" {
		t.Fatalf("got %q", got)
	}
}

func TestPublicURLIsTrimmedAndNilSafe(t *testing.T) {
	var n *Names
	if got := n.PublicURL(); got != "" {
		t.Fatalf("nil Names: got %q", got)
	}
	if got := New(nil, " https://chat.example.org/ ").PublicURL(); got != "https://chat.example.org" {
		t.Fatalf("got %q", got)
	}
}
