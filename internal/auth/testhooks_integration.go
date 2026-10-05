//go:build integration

package auth

import (
	"fmt"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

// SetPasswordCostForTests lowers the bcrypt cost of new password hashes (and
// of the timing-equaliser hash) for an integration test binary. It exists only
// with the "integration" build tag, refuses to run outside a test binary and
// must be called before any test starts (e.g. from TestMain), since it is not
// synchronised. Production always hashes with bcryptCost.
func SetPasswordCostForTests(cost int) {
	if !testing.Testing() {
		panic("auth: SetPasswordCostForTests called outside a test binary")
	}
	if cost < bcrypt.MinCost || cost > bcrypt.MaxCost {
		panic(fmt.Sprintf("auth: bcrypt cost %d out of range", cost))
	}
	passwordCost = cost
	dummyHash = newDummyHash()
}

// PasswordCost reports the bcrypt cost new hashes currently use.
func PasswordCost() int { return passwordCost }
