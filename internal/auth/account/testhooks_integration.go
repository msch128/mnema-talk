//go:build integration

package account

import (
	"testing"

	"github.com/msch128/mnema-talk/internal/userdata"
)

// SetPersonalDataFallback installs the domain registry used by integration
// tests that erase or export an account without the server. It refuses to run
// outside a test binary.
func SetPersonalDataFallback(r *userdata.Registry) {
	if !testing.Testing() {
		panic("account: SetPersonalDataFallback called outside a test binary")
	}
	personalFallback = r
}
