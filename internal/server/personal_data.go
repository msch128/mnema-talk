package server

import (
	"github.com/msch128/mnema-talk/internal/chat"
	"github.com/msch128/mnema-talk/internal/communityevents"
	"github.com/msch128/mnema-talk/internal/later"
	"github.com/msch128/mnema-talk/internal/push"
	"github.com/msch128/mnema-talk/internal/userdata"
)

// wirePersonalData registers each domain's export files and erasure statements
// on the account handler. Auth does not import those domains.
func (c *components) wirePersonalData() {
	r := &userdata.Registry{}
	chat.RegisterAccountData(r)
	later.RegisterAccountData(r)
	communityevents.RegisterAccountData(r)
	push.RegisterAccountData(r)
	c.authH.SetPersonalData(r)
}
