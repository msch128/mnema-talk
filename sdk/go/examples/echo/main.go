// Command echo is a bot that repeats messages starting with "!echo ".
//
//	MNEMA_URL=https://talk.example.org MNEMA_TOKEN=mnb_... go run ./examples/echo
//
// The bot needs the GUILD_MESSAGES intent allowed, and View channels and Send
// messages in the channels it should answer in.
package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	mnema "github.com/msch128/mnema-talk/sdk/go"
)

func main() {
	c, err := mnema.New(mnema.Options{
		BaseURL: os.Getenv("MNEMA_URL"),
		Token:   os.Getenv("MNEMA_TOKEN"),
		Intents: mnema.IntentGuildMessages,
		Logger:  slog.Default(),
	})
	if err != nil {
		slog.Error("configure", "err", err)
		os.Exit(1)
	}
	c.OnReady(func(_ context.Context, r *mnema.Ready) { slog.Info("ready", "as", r.User.Username) })
	c.OnMessageCreate(func(ctx context.Context, m *mnema.Message) {
		text, ok := strings.CutPrefix(m.Content, "!echo ")
		if !ok || m.IsBot { // bots, this one included, are not echoed
			return
		}
		_, err := c.REST.CreateMessage(ctx, m.ChannelID, mnema.MessageCreate{
			Content:          text,
			MessageReference: &mnema.MessageReference{MessageID: m.ID},
			AllowedMentions:  &mnema.AllowedMentions{Parse: []string{}},
		})
		if err != nil {
			slog.Warn("echo", "err", err)
		}
	})
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := c.Run(ctx); err != nil {
		var ce *mnema.CloseError
		if errors.As(err, &ce) {
			slog.Error("the gateway refused the bot", "code", ce.Code, "reason", ce.Error())
		} else {
			slog.Error("run", "err", err)
		}
		os.Exit(1)
	}
}
