package chat

import (
	"slices"
	"testing"
)

func TestPlaceBelow(t *testing.T) {
	cases := []struct {
		name      string
		orders    []int
		src       int
		wantNew   int
		wantShift []int
	}{
		{"last", []int{0, 1, 2}, 2, 3, []int{}},
		{"middle shifts followers by one", []int{0, 1, 2, 3}, 1, 2, []int{3, 4}},
		{"gaps keep their size", []int{0, 10, 20}, 0, 1, []int{11, 21}},
		{"ties with the source move past the copy", []int{0, 0, 0}, 0, 1, []int{2, 3}},
		{"follower ties are separated", []int{0, 1, 2, 2}, 0, 1, []int{2, 3, 4}},
		{"negative orders", []int{-5, -4}, 0, -4, []int{-3}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			gotNew, gotShift := placeBelow(c.orders, c.src)
			if gotNew != c.wantNew || !slices.Equal(gotShift, c.wantShift) {
				t.Fatalf("placeBelow(%v, %d) = %d, %v; want %d, %v", c.orders, c.src, gotNew, gotShift, c.wantNew, c.wantShift)
			}
			// The resulting order must be strictly increasing from the copy on,
			// and every follower must sort after its old position.
			prev := gotNew
			for i, o := range gotShift {
				if o <= prev || o <= c.orders[c.src+1+i] {
					t.Fatalf("follower %d at %d does not sort below %d", i, o, prev)
				}
				prev = o
			}
		})
	}
}
