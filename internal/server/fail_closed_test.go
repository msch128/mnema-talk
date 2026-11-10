package server

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/auth/authn"
	"github.com/msch128/mnema-talk/internal/automod"
	"github.com/msch128/mnema-talk/internal/bot"
	"github.com/msch128/mnema-talk/internal/chat"
	"github.com/msch128/mnema-talk/internal/chat/core"
	"github.com/msch128/mnema-talk/internal/chat/messages"
	"github.com/msch128/mnema-talk/internal/chat/talks"
	"github.com/msch128/mnema-talk/internal/communityevents"
	"github.com/msch128/mnema-talk/internal/config"
	"github.com/msch128/mnema-talk/internal/discordimport"
	"github.com/msch128/mnema-talk/internal/emoji"
	"github.com/msch128/mnema-talk/internal/gateway"
	"github.com/msch128/mnema-talk/internal/later"
	"github.com/msch128/mnema-talk/internal/media"
	"github.com/msch128/mnema-talk/internal/moderation"
	"github.com/msch128/mnema-talk/internal/onboarding"
	"github.com/msch128/mnema-talk/internal/perm"
	"github.com/msch128/mnema-talk/internal/perm/permtest"
	"github.com/msch128/mnema-talk/internal/poll"
	"github.com/msch128/mnema-talk/internal/roles"
	"github.com/msch128/mnema-talk/internal/slashcmd"
	"github.com/msch128/mnema-talk/internal/webhook"
	"github.com/msch128/mnema-talk/internal/wiki"
	"github.com/msch128/mnema-talk/internal/ws"
)

// Everything that asks the permission model is built through a constructor
// that refuses a nil service. A component wired without one stops the server
// at startup; it never serves requests that nobody checks.
func TestNoPermissionConsumerIsBuiltWithoutThePermissionService(t *testing.T) {
	for name, build := range map[string]func(){
		"websocket hub":      func() { ws.NewHub(nil, nil, nil, nil, nil) },
		"chat guard":         func() { core.NewGuard(nil) },
		"chat routes":        func() { (&chat.Handler{}).Mount(chi.NewRouter()) },
		"thread follow sync": func() { messages.NewFollowSync(context.Background(), nil, nil, nil) },
		"talk manager":       func() { talks.NewTalkManager(&talks.TalkManager{}) },
		"auth handler":       func() { authn.NewHandler(&auth.Sessions{}, nil, nil) },
		"roles handler":      func() { roles.New(roles.Handler{}) },
		"onboarding handler": func() { onboarding.NewHandler(nil, nil) },
		"emoji handler":      func() { emoji.New(emoji.Handler{}) },
		"media handler":      func() { media.New(&media.Handler{}) },
		"webhook handler":    func() { webhook.New(webhook.Handler{}) },
		"bot handler":        func() { bot.NewHandler(bot.Handler{}) },
		"bot authenticator":  func() { bot.NewAuthenticator(nil, nil) },
		"slash commands":     func() { bot.NewCommands(bot.Commands{}) },
		"built-in commands":  func() { slashcmd.Commands(slashcmd.Services{}) },
		"bot gateway":        func() { gateway.New(gateway.Config{}) },
		"polls":              func() { poll.NewService(poll.Service{}) },
		"reminders":          func() { later.NewService(later.Service{}) },
		"community events":   func() { communityevents.New(&communityevents.Handler{}) },
		"wiki":               func() { wiki.New(wiki.Handler{}) },
		"automod engine":     func() { automod.New(nil, nil, nil, nil) },
		"moderation handler": func() { moderation.New(moderation.Handler{}) },
		"discord import":     func() { discordimport.New(discordimport.Handler{Importer: &discordimport.Importer{}}) },
		"discord importer": func() {
			discordimport.New(discordimport.Handler{Perms: permtest.NewFixture().Service(), Importer: &discordimport.Importer{}})
		},
		"permission question": func() { perm.Must(nil, "test") },
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				r, _ := recover().(string)
				if !strings.HasSuffix(r, "needs a permission service") {
					t.Fatalf("built without a permission service (recovered %q)", r)
				}
			}()
			build()
		})
	}
}

// The router hands the permission service to every component that has a place
// for one. A handler added with a struct literal that leaves Perms out would
// fail closed at request time; this fails before it ships.
func TestTheRouterWiresThePermissionServiceIntoEveryComponent(t *testing.T) {
	svc := permtest.NewFixture().Service()
	cfg := &config.Config{AllowedOrigins: []string{"http://example.test"}, JWTSecret: "router-test-secret-router-test-secret", LinkPreviews: true}
	c, cancel, err := assemble(Deps{Config: cfg, Perms: svc})
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	defer c.hub.Close()

	holders := map[string]bool{}
	var walk func(path string, v reflect.Value, depth int)
	walk = func(path string, v reflect.Value, depth int) {
		for v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface {
			if v.IsNil() {
				return
			}
			v = v.Elem()
		}
		if v.Kind() != reflect.Struct || depth > 3 || !strings.Contains(v.Type().PkgPath(), "/mnema-talk/internal/") {
			return
		}
		for i := range v.NumField() {
			f, name := v.Field(i), path+"."+v.Type().Field(i).Name
			if f.Type() == reflect.TypeOf(svc) {
				holders[name] = !f.IsNil()
				continue
			}
			walk(name, f, depth+1)
		}
	}
	walk("components", reflect.ValueOf(c), 0)

	if len(holders) < 20 {
		t.Fatalf("found only %d places for the permission service: %v", len(holders), holders)
	}
	for name, set := range holders {
		if !set {
			t.Errorf("%s is nil", name)
		}
	}
}
