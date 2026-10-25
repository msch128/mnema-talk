package update

import "testing"

// FuzzSemver checks parsing never panics, accepted versions round trip, and
// comparison is antisymmetric.
func FuzzSemver(f *testing.F) {
	for _, s := range []string{"1.2.3", "v1.2.3", "1.2.3-rc.1", "dev", "", "1.2", "999999.999999.999999", "1.2.3-", "1.2.3+build", " 1.2.3 ", "01.02.03"} {
		f.Add(s, "1.2.3")
	}
	f.Fuzz(func(t *testing.T, a, b string) {
		va, okA := ParseSemver(a)
		vb, okB := ParseSemver(b)
		if okA {
			again, ok := ParseSemver(va.String())
			if !ok || again != va {
				t.Fatalf("round trip of %q gave %v", a, again)
			}
		}
		if okA && okB && va.Compare(vb) != -vb.Compare(va) {
			t.Fatalf("compare not antisymmetric: %q %q", a, b)
		}
		if (!okA || !okB) && IsNewer(a, b) {
			t.Fatalf("IsNewer accepted unparseable %q %q", a, b)
		}
	})
}
