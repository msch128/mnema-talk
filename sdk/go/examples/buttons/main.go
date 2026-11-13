// Command buttons is a bot that answers "!deploy" with buttons and a select
// menu, updates the message when they are used, and asks for feedback in a
// modal.
//
//	MNEMA_URL=https://talk.example.org MNEMA_TOKEN=mnb_... go run ./examples/buttons
//
// The bot needs the GUILD_MESSAGES intent allowed, and View channels and Send
// messages in the channels it should work in.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	mnema "github.com/msch128/mnema-talk/sdk/go"
)

func deployRows(env string) []mnema.Component {
	options := []mnema.SelectOption{{Label: "Staging", Value: "staging"}, {Label: "Production", Value: "prod"}}
	for k := range options {
		options[k].Default = options[k].Value == env
	}
	return []mnema.Component{
		mnema.ActionRow(
			mnema.SuccessButton("deploy:"+env, "Deploy"),
			mnema.SecondaryButton("cancel", "Cancel"),
			mnema.Button{Style: mnema.ButtonSecondary, CustomID: "feedback", Label: "Feedback", Emoji: "💬"},
			mnema.LinkButton("https://example.org/changes", "Changelog"),
		),
		mnema.ActionRow(mnema.StringSelect{CustomID: "env", Placeholder: "Environment", Options: options}),
	}
}

func main() {
	c, err := mnema.New(mnema.Options{BaseURL: os.Getenv("MNEMA_URL"), Token: os.Getenv("MNEMA_TOKEN"), Intents: mnema.IntentGuildMessages})
	if err != nil {
		slog.Error("configure", "err", err)
		os.Exit(1)
	}
	c.OnMessageCreate(func(ctx context.Context, m *mnema.Message) {
		if m.Content != "!deploy" || m.IsBot {
			return
		}
		if _, err := c.REST.CreateMessage(ctx, m.ChannelID, mnema.MessageCreate{Content: "Deploy v2.4 to staging?", Components: deployRows("staging")}); err != nil {
			slog.Warn("post", "err", err)
		}
	})
	c.OnInteraction(func(ctx context.Context, i *mnema.Interaction) {
		if err := handle(ctx, i); err != nil {
			slog.Warn("answer", "custom_id", i.CustomID(), "err", err)
		}
	})
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := c.Run(ctx); err != nil {
		slog.Error("run", "err", err)
		os.Exit(1)
	}
}

func handle(ctx context.Context, i *mnema.Interaction) error {
	switch {
	case i.IsSelect() && i.CustomID() == "env":
		env := i.Values()[0]
		return i.Update(ctx, mnema.Edit{Content: mnema.Ptr("Deploy v2.4 to " + env + "?"), Components: mnema.Ptr(deployRows(env))})
	case i.IsButton() && strings.HasPrefix(i.CustomID(), "deploy:"):
		env := strings.TrimPrefix(i.CustomID(), "deploy:")
		text := fmt.Sprintf("Deploying v2.4 to %s, started by %s.", env, i.User.DisplayName)
		return i.Update(ctx, mnema.Edit{Content: &text, Components: &[]mnema.Component{}})
	case i.IsButton() && i.CustomID() == "cancel":
		return i.Update(ctx, mnema.Edit{Content: mnema.Ptr("Cancelled."), Components: &[]mnema.Component{}})
	case i.IsButton() && i.CustomID() == "feedback":
		return i.ShowModal(ctx, mnema.NewModal("feedback", "Feedback",
			mnema.TextInput{CustomID: "subject", Label: "Subject", MaxLength: mnema.Ptr(80)},
			mnema.TextInput{CustomID: "body", Label: "What happened?", Style: mnema.TextInputParagraph, Required: mnema.Ptr(false), Placeholder: "Tell us more"},
		))
	case i.IsModalSubmit() && i.CustomID() == "feedback":
		return i.Reply(ctx, mnema.Response{Content: "Thanks for the feedback on “" + i.Field("subject") + "”.", Ephemeral: true})
	}
	return nil
}
