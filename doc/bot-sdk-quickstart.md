# Bot SDK quick start

A complete bot in about forty lines, in TypeScript or in Go. Both programs do
the same: answer `!ping` with a button, answer the button, and register and
answer the slash command `/roll`. They are the same ones the SDK READMEs build
on ([TypeScript](https://github.com/mnema-xyz/bot-sdk-js#readme), [Go](../sdk/go/README.md)); what
each call sends is in [bots](bots.md), what the gateway delivers in
[gateway events](gateway-events.md), and more tasks are in the
[recipes](bot-recipes.md).

## 1. Make the bot

You need to be the Owner or hold *Manage bots* ([who may create bots](bots.md#who-may-create-bots)).

1. **Admin area > Bots > New bot.** Name it, and allow the intent
   `GUILD_MESSAGES` (the program asks for it; an intent the admin has not
   allowed closes the gateway with `4014`). Slash commands and buttons need no
   intent.
2. Give it a role with **View channels** and **Send messages** and allow that
   role in the channels where it should work (a bot starts with no access at
   all). The *Access* view of the Bots tab shows the result.
3. **Issue a token** and copy it. It is shown once.

Keep the token out of the code and out of the repository. The programs read it,
and the server address, from the environment:

```sh
export MNEMA_URL=https://talk.example.org
export MNEMA_TOKEN=mnb_...
```

## 2. TypeScript

Node 24 or newer. The SDK has no runtime dependencies.

```sh
mkdir my-bot && cd my-bot
npm init -y && npm pkg set type=module
npm install @mnema-xyz/bot-sdk
```

Save this as `bot.ts`:

```ts
import { ButtonStyle, Client, Intents, OptionType, actionRow, button } from '@mnema-xyz/bot-sdk'

const baseUrl = process.env.MNEMA_URL
const token = process.env.MNEMA_TOKEN
if (!baseUrl || !token) {
  console.error('set MNEMA_URL and MNEMA_TOKEN')
  process.exit(1)
}

const client = new Client({ baseUrl, token, intents: Intents.GuildMessages })

client.on('ready', async (ready) => {
  console.log(`connected as ${ready.user.username}`)
  await client.commands.set([
    {
      name: 'roll',
      description: 'Rolls a die',
      options: [{ name: 'sides', description: 'How many sides (default 6)', type: OptionType.Integer }],
    },
  ])
})

client.on('messageCreate', async (message) => {
  // The bot's own messages arrive too: ignore them.
  if (message.user_id === client.user?.id || message.content !== '!ping') return
  await client.rest.createMessage(message.channel_id, {
    content: 'pong',
    message_reference: { message_id: message.id },
    components: [actionRow(button({ customId: 'again', label: 'Again', style: ButtonStyle.Primary }))],
  })
})

client.on('interactionCreate', async (interaction) => {
  if (interaction.isCommand() && interaction.commandName === 'roll') {
    const asked = Number(interaction.options.get('sides') ?? 6)
    const sides = asked >= 2 ? asked : 6
    const result = 1 + Math.floor(Math.random() * sides)
    await interaction.reply(`${interaction.user.display_name} rolled ${result} (d${sides})`)
  } else if (interaction.isButton() && interaction.customId === 'again') {
    await interaction.update({ content: 'pong again' })
  }
})

client.on('error', (err) => console.error(err))
process.on('SIGINT', () => void client.close().then(() => process.exit(0)))

await client.connect()
```

```sh
node bot.ts
```

Node strips the types, so no build step is needed. `connect()` checks the token,
opens the gateway and resolves with `READY`; heartbeats, resume and reconnects
are the SDK's job.

## 3. Go

Go 1.26 or newer.

```sh
mkdir my-bot && cd my-bot
go mod init example.org/my-bot
go get github.com/msch128/mnema-talk/sdk/go@main
```

Save this as `main.go`:

```go
package main

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"os"
	"os/signal"
	"syscall"

	mnema "github.com/msch128/mnema-talk/sdk/go"
)

func main() {
	baseURL, token := os.Getenv("MNEMA_URL"), os.Getenv("MNEMA_TOKEN")
	if baseURL == "" || token == "" {
		slog.Error("set MNEMA_URL and MNEMA_TOKEN")
		os.Exit(1)
	}
	c, err := mnema.New(mnema.Options{BaseURL: baseURL, Token: token, Intents: mnema.IntentGuildMessages, Logger: slog.Default()})
	if err != nil {
		slog.Error("configure", "err", err)
		os.Exit(1)
	}

	c.OnReady(func(ctx context.Context, r *mnema.Ready) {
		slog.Info("connected", "as", r.User.Username)
		_, err := c.REST.SetCommands(ctx, []mnema.CommandInput{{
			Name: "roll", Description: "Rolls a die",
			Options: []mnema.CommandOption{{Name: "sides", Description: "How many sides (default 6)", Type: mnema.OptionInteger}},
		}})
		if err != nil {
			slog.Error("register commands", "err", err)
		}
	})

	c.OnMessageCreate(func(ctx context.Context, m *mnema.Message) {
		if m.IsBot || m.Content != "!ping" { // bots, this one included, are not answered
			return
		}
		_, err := c.REST.CreateMessage(ctx, m.ChannelID, mnema.MessageCreate{
			Content:          "pong",
			MessageReference: &mnema.MessageReference{MessageID: m.ID},
			Components:       []mnema.Component{mnema.ActionRow(mnema.PrimaryButton("again", "Again"))},
		})
		if err != nil {
			slog.Warn("pong", "err", err)
		}
	})

	c.OnInteraction(func(ctx context.Context, i *mnema.Interaction) {
		var err error
		switch {
		case i.IsCommand() && i.CommandName() == "roll":
			sides := i.Option("sides").Int()
			if sides < 2 {
				sides = 6
			}
			err = i.Reply(ctx, mnema.Response{Content: fmt.Sprintf("%s rolled %d (d%d)", i.User.DisplayName, rand.Int64N(sides)+1, sides)})
		case i.IsButton() && i.CustomID() == "again":
			err = i.Update(ctx, mnema.Edit{Content: mnema.Ptr("pong again")})
		}
		if err != nil {
			slog.Warn("answer", "err", err)
		}
	})

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := c.Run(ctx); err != nil {
		slog.Error("run", "err", err) // a *mnema.CloseError for 4004, 4013, 4014
		os.Exit(1)
	}
}
```

```sh
go run .
```

`Run` connects, identifies and keeps the session alive until the context ends;
it returns an error only for a refusal that retrying cannot fix (a bad or
revoked token, intents the admin has not allowed).

## 4. Try it

In a channel the bot can view, write `!ping`: the bot replies with a button.
Type `/roll` to run the command; a command shows in the composer's slash menu of
every channel where the bot may view and send. If nothing happens, the bot most
likely cannot view that channel (the *Access* view of the Bots tab shows what it
can); a refusal of the token or of the intents ends the program with the close
code (see [close codes](bots.md#close-codes)).

## Where to go next

- [Recipes](bot-recipes.md): replies, embeds, reactions, files, threads, selects, modals.
- [Allowed mentions](allowed-mentions.md): whom a message may notify.
- [Gateway events](gateway-events.md): every event and its payload.
- What a bot can not do: [bots](bots.md#what-a-bot-cannot-do).
