package opwarn

import (
	"fmt"
	"testing"
	"time"
)

func TestRegistryDedupesAndSorts(t *testing.T) {
	r := New()
	r.now = func() time.Time { return time.Unix(100, 0) }
	r.Raise("b", 0)
	r.Raise("a", 3)
	r.Raise("b", 0)
	r.Raise("", 1)
	got := r.List()
	if len(got) != 2 || got[0].Code != "a" || got[1].Code != "b" {
		t.Fatalf("list = %+v", got)
	}
	if got[0].Value != 3 || got[1].Count != 2 || !got[1].Since.Equal(time.Unix(100, 0)) {
		t.Fatalf("fields = %+v", got)
	}
	r.Raise("a", 5)
	if r.List()[0].Value != 5 {
		t.Fatal("value not updated")
	}
}

func TestRegistryBoundedAndClear(t *testing.T) {
	r := New()
	for i := 0; i < MaxWarnings+10; i++ {
		r.Raise(fmt.Sprintf("c%02d", i), 0)
	}
	if n := len(r.List()); n != MaxWarnings {
		t.Fatalf("len = %d", n)
	}
	r.Raise("c00", 1) // existing codes still update at the bound
	r.Clear("c00")
	if n := len(r.List()); n != MaxWarnings-1 {
		t.Fatalf("len after clear = %d", n)
	}
}

func TestNilRegistryAndDefault(t *testing.T) {
	var r *Registry
	r.Raise("x", 0)
	r.Clear("x")
	if l := r.List(); l == nil || len(l) != 0 {
		t.Fatalf("nil list = %v", l)
	}
	Raise("default_test", 0)
	defer Default.Clear("default_test")
	if len(Default.List()) == 0 {
		t.Fatal("default empty")
	}
}

func TestCodesAreUnique(t *testing.T) {
	seen := map[string]bool{}
	for _, c := range Codes() {
		if c == "" || seen[c] {
			t.Errorf("code %q is empty or listed twice", c)
		}
		seen[c] = true
	}
}
