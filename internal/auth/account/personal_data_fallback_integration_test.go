//go:build integration

package account_test

import (
	"github.com/msch128/mnema-talk/internal/auth/account"
	"github.com/msch128/mnema-talk/internal/bot"
	"github.com/msch128/mnema-talk/internal/chat/account"
	"github.com/msch128/mnema-talk/internal/communityevents"
	"github.com/msch128/mnema-talk/internal/discordimport"
	"github.com/msch128/mnema-talk/internal/emoji"
	"github.com/msch128/mnema-talk/internal/later"
	"github.com/msch128/mnema-talk/internal/push"
	"github.com/msch128/mnema-talk/internal/userdata"
	"github.com/msch128/mnema-talk/internal/webhook"
	"github.com/msch128/mnema-talk/internal/wiki"
)

// Integration tests in package auth build a Handler without the server, so
// they need the same domain registry the server installs.
func init() {
	r := &userdata.Registry{}
	account.RegisterAccountData(r)
	later.RegisterAccountData(r)
	communityevents.RegisterAccountData(r)
	push.RegisterAccountData(r)
	wiki.RegisterAccountData(r)
	bot.RegisterAccountData(r)
	discordimport.RegisterAccountData(r)
	emoji.RegisterAccountData(r)
	webhook.RegisterAccountData(r)
	account.SetPersonalDataFallback(r)
}
