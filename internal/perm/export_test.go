package perm

import "time"

// Unexported checks the external tests (package perm_test) exercise directly.
var (
	CheckGrant     = checkGrant
	CheckBotGrowth = checkBotGrowth
)

// SetClock replaces the time source of timeout checks and returns the
// function that restores it.
func SetClock(fn func() time.Time) (restore func()) {
	clock = fn
	return func() { clock = time.Now }
}
