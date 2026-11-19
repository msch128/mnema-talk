//go:build integration

package account_test

import (
	"github.com/mnema-xyz/talk/internal/auth/account"
	"github.com/mnema-xyz/talk/internal/bot"
	chataccount "github.com/mnema-xyz/talk/internal/chat/account"
	"github.com/mnema-xyz/talk/internal/communityevents/accountdata"
	"github.com/mnema-xyz/talk/internal/discordimport"
	"github.com/mnema-xyz/talk/internal/emoji"
	"github.com/mnema-xyz/talk/internal/later"
	"github.com/mnema-xyz/talk/internal/push"
	"github.com/mnema-xyz/talk/internal/userdata"
	"github.com/mnema-xyz/talk/internal/webhook"
	"github.com/mnema-xyz/talk/internal/wiki"
)

// Integration tests in package auth build a Handler without the server, so
// they need the same domain registry the server installs.
func init() {
	r := &userdata.Registry{}
	chataccount.RegisterAccountData(r)
	later.RegisterAccountData(r)
	accountdata.Register(r)
	push.RegisterAccountData(r)
	wiki.RegisterAccountData(r)
	bot.RegisterAccountData(r)
	discordimport.RegisterAccountData(r)
	emoji.RegisterAccountData(r)
	webhook.RegisterAccountData(r)
	account.SetPersonalDataFallback(r)
}
