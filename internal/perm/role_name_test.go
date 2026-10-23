package perm

import "testing"

func TestReservedRoleNamesCannotBeDisguisedWithInvisibleCharacters(t *testing.T) {
	for _, name := range []string{"everyone", "@Everyone", "every‍one", "@he​re", "ＥＶＥＲＹＯＮＥ"} {
		if _, err := cleanRoleName(name); err == nil {
			t.Errorf("%q accepted", name)
		}
	}
	if got, err := cleanRoleName("Moderators"); err != nil || got != "Moderators" {
		t.Fatalf("got %q, %v", got, err)
	}
}
