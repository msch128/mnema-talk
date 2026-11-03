//go:build integration

package auth_test

import (
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/chat"
	"github.com/msch128/mnema-talk/internal/communityevents"
	"github.com/msch128/mnema-talk/internal/later"
	"github.com/msch128/mnema-talk/internal/push"
	"github.com/msch128/mnema-talk/internal/userdata"
)

// Integration tests in package auth build a Handler without the server, so
// they need the same domain registry the server installs.
func init() {
	r := &userdata.Registry{}
	chat.RegisterAccountData(r)
	later.RegisterAccountData(r)
	communityevents.RegisterAccountData(r)
	push.RegisterAccountData(r)
	auth.SetPersonalDataFallback(r)
}
