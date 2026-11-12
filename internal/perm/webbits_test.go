package perm

import "testing"

// The web helpers (web/src/shared/lib/permissions.ts hasPermission and
// setPermission) use JavaScript's bitwise operators, which work on signed
// 32-bit integers: a permission on bit 31 would never test as held
// ((set & bit) is negative) and one above it would vanish, without an error,
// while TestWebMirrorsTheCatalog stays green. A new permission past bit 30
// needs those helpers moved to BigInt or Math arithmetic first
// (web/src/shared/lib/permissions.test.ts pins what they do today).
func TestEveryPermissionFitsTheWebHelpersThirtyOneBits(t *testing.T) {
	var all Set
	for _, i := range Catalog {
		if i.Bit >= 1<<31 || i.Bit == 0 || i.Bit&(i.Bit-1) != 0 {
			t.Errorf("%s is bit %d: the web helpers handle single bits 0 to 30 only", i.Key, i.Bit)
		}
		all |= i.Bit
	}
	for _, s := range []struct {
		name string
		set  Set
	}{{"DefaultEveryone", DefaultEveryone}, {"TimeoutMask", TimeoutMask}, {"AdminArea", AdminArea}, {"SelfAssignBlocked", SelfAssignBlocked}, {"every permission", all}} {
		if s.set >= 1<<31 {
			t.Errorf("%s = %d does not fit the web helpers", s.name, s.set)
		}
	}
}
