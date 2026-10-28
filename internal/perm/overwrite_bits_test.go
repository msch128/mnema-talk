package perm

import "testing"

func TestOverwriteBitsRejectedKeepsToChannelScopedPermissions(t *testing.T) {
	for _, c := range []struct {
		what string
		o    Overwrite
		bad  bool
	}{
		{"channel scoped", Overwrite{Allow: ViewChannel, Deny: SendMessages}, false},
		{"bit outside All", Overwrite{Allow: Set(1) << 62}, true},
		{"server wide permission", Overwrite{Deny: Administrator}, true},
		{"both allowed and denied", Overwrite{Allow: ViewChannel, Deny: ViewChannel}, true},
	} {
		if got := overwriteBitsRejected(c.o) != nil; got != c.bad {
			t.Errorf("%s: rejected = %v, want %v", c.what, got, c.bad)
		}
	}
}
