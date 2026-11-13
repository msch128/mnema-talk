// Command slash is a bot with two slash commands: /ping answers privately,
// /roll rolls dice after a deferred reply.
//
//	MNEMA_URL=https://talk.example.org MNEMA_TOKEN=mnb_... go run ./examples/slash
//
// Members see the commands in every channel where the bot can view and send.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	mnema "github.com/msch128/mnema-talk/sdk/go"
)

var commands = []mnema.CommandInput{
	{Name: "ping", Description: "Replies with pong, only to you"},
	{Name: "roll", Description: "Rolls dice", Options: []mnema.CommandOption{
		{Name: "sides", Description: "Sides of each die (default 6)", Type: mnema.OptionInteger},
		{Name: "count", Description: "How many dice (default 1)", Type: mnema.OptionInteger},
		{Name: "secret", Description: "Show the result only to you", Type: mnema.OptionBoolean},
	}},
}

func main() {
	c, err := mnema.New(mnema.Options{BaseURL: os.Getenv("MNEMA_URL"), Token: os.Getenv("MNEMA_TOKEN")})
	if err != nil {
		slog.Error("configure", "err", err)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if _, err := c.REST.SetCommands(ctx, commands); err != nil {
		slog.Error("register commands", "err", err)
		os.Exit(1)
	}
	c.OnInteraction(func(ctx context.Context, i *mnema.Interaction) {
		var err error
		switch i.CommandName() {
		case "ping":
			err = i.Reply(ctx, mnema.Response{Content: "pong", Ephemeral: true})
		case "roll":
			err = roll(ctx, i)
		}
		if err != nil {
			slog.Warn("answer", "command", i.CommandName(), "err", err)
		}
	})
	if err := c.Run(ctx); err != nil {
		slog.Error("run", "err", err)
		os.Exit(1)
	}
}

// roll defers the reply (as a slow command would), then edits it in.
func roll(ctx context.Context, i *mnema.Interaction) error {
	sides, count := i.Option("sides").Int(), i.Option("count").Int()
	if sides < 2 {
		sides = 6
	}
	if count < 1 || count > 20 {
		count = 1
	}
	if err := i.DeferReply(ctx, i.Option("secret").Bool()); err != nil {
		return err
	}
	time.Sleep(500 * time.Millisecond) // the work a real command would do
	var dice []string
	total := int64(0)
	for range count {
		n := rand.Int64N(sides) + 1
		total += n
		dice = append(dice, fmt.Sprint(n))
	}
	text := fmt.Sprintf("%s rolled %s (d%d): **%d**", i.User.DisplayName, strings.Join(dice, " + "), sides, total)
	_, err := i.EditReply(ctx, mnema.Edit{Content: &text})
	return err
}
