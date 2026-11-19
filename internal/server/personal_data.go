package server

import (
	"github.com/mnema-xyz/talk/internal/bot"
	"github.com/mnema-xyz/talk/internal/chat/account"
	"github.com/mnema-xyz/talk/internal/communityevents/accountdata"
	"github.com/mnema-xyz/talk/internal/discordimport"
	"github.com/mnema-xyz/talk/internal/emoji"
	"github.com/mnema-xyz/talk/internal/later"
	"github.com/mnema-xyz/talk/internal/push"
	"github.com/mnema-xyz/talk/internal/userdata"
	"github.com/mnema-xyz/talk/internal/webhook"
	"github.com/mnema-xyz/talk/internal/wiki"
)

// wirePersonalData registers each domain's export files and erasure statements
// on the account handler. Auth does not import those domains.
func (c *components) wirePersonalData() {
	r := &userdata.Registry{}
	account.RegisterAccountData(r)
	later.RegisterAccountData(r)
	accountdata.Register(r)
	push.RegisterAccountData(r)
	wiki.RegisterAccountData(r)
	bot.RegisterAccountData(r)
	discordimport.RegisterAccountData(r)
	emoji.RegisterAccountData(r)
	webhook.RegisterAccountData(r)
	c.account.SetPersonalData(r)
}
