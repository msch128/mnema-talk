package mention

import (
	"slices"
	"strings"
	"testing"
)

func TestNamesAreLowerCasedDistinctAndBounded(t *testing.T) {
	got := Names("hi @Alice, @alice. and mail a@bob.io @all @ab " + "@" + strings.Repeat("a", MaxNameLen+1) + " @" + strings.Repeat("b", MaxNameLen))
	want := []string{"alice", "all", strings.Repeat("b", MaxNameLen)}
	if !slices.Equal(got, want) {
		t.Fatalf("Names = %v, want %v", got, want)
	}
}
