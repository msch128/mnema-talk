package server

import (
	"github.com/msch128/mnema-talk/internal/bot"
	"github.com/msch128/mnema-talk/internal/chat"
	"github.com/msch128/mnema-talk/internal/communityevents"
	"github.com/msch128/mnema-talk/internal/discordimport"
	"github.com/msch128/mnema-talk/internal/emoji"
	"github.com/msch128/mnema-talk/internal/later"
	"github.com/msch128/mnema-talk/internal/push"
	"github.com/msch128/mnema-talk/internal/userdata"
	"github.com/msch128/mnema-talk/internal/webhook"
	"github.com/msch128/mnema-talk/internal/wiki"
)

// wirePersonalData registers each domain's export files and erasure statements
// on the account handler. Auth does not import those domains.
func (c *components) wirePersonalData() {
	r := &userdata.Registry{}
	chat.RegisterAccountData(r)
	later.RegisterAccountData(r)
	communityevents.RegisterAccountData(r)
	push.RegisterAccountData(r)
	wiki.RegisterAccountData(r)
	bot.RegisterAccountData(r)
	discordimport.RegisterAccountData(r)
	emoji.RegisterAccountData(r)
	webhook.RegisterAccountData(r)
	c.account.SetPersonalData(r)
}
