package userdata

import "testing"

func TestAsListIsAnEmptyArrayWhenThereAreNoRows(t *testing.T) {
	got := AsList[int](nil)
	if got == nil || len(got) != 0 {
		t.Fatalf("nil rows: %#v", got)
	}
	got = AsList([]int{1, 2})
	if len(got) != 2 || got[0] != 1 || got[1] != 2 {
		t.Fatalf("%#v", got)
	}
}
