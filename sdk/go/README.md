# Mnema Talk SDK for Go

The official Go SDK for [Mnema Talk bots](../../doc/bots.md). It covers the
bot REST API with its rate limits, the gateway with heartbeats, resume and
reconnects, slash commands, interactions, message components and modals, and
the add link of a bot manifest. Its only dependency is
[gorilla/websocket](https://github.com/gorilla/websocket).

The wire contract (routes, frames, close codes, limits) is
[doc/bots.md](../../doc/bots.md); this README shows how the SDK maps it.

## Install

```sh
go get github.com/msch128/mnema-talk/sdk/go@main
```

This resolves to a pseudo-version of the latest commit. Tags of the form
`sdk/go/vX.Y.Z` may follow; then `@latest` works too. The package name is
`mnema`. It needs Go 1.27 or newer.

## Quick start

Create a bot and a token first ([Getting started](../../doc/bots.md#getting-started)).

```go
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"

	mnema "github.com/msch128/mnema-talk/sdk/go"
)

func main() {
	c, err := mnema.New(mnema.Options{
		BaseURL: "https://talk.example.org",
		Token:   os.Getenv("MNEMA_TOKEN"),
		Intents: mnema.IntentGuilds | mnema.IntentGuildMessages,
		Logger:  slog.Default(),
	})
	if err != nil {
		slog.Error("configure", "err", err)
		os.Exit(1)
	}
	c.OnReady(func(ctx context.Context, r *mnema.Ready) {
		slog.Info("ready", "as", r.User.Username)
	})
	c.OnMessageCreate(func(ctx context.Context, m *mnema.Message) {
		if m.Content == "!ping" {
			_, _ = c.REST.CreateMessage(ctx, m.ChannelID, mnema.MessageCreate{
				Content:          "pong",
				MessageReference: &mnema.MessageReference{MessageID: m.ID},
			})
		}
	})
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := c.Run(ctx); err != nil {
		slog.Error("run", "err", err) // a *mnema.CloseError for 4004, 4013, 4014
		os.Exit(1)
	}
}
```

Three runnable examples read `MNEMA_URL` and `MNEMA_TOKEN`:

| Example | What it does |
|---|---|
| [examples/echo](examples/echo/main.go) | repeats messages that start with `!echo ` as a reply |
| [examples/slash](examples/slash/main.go) | registers `/ping` (ephemeral reply) and `/roll` with options (deferred reply, then edited) |
| [examples/buttons](examples/buttons/main.go) | answers `!deploy` with buttons and a select, updates the message on use, opens a modal and answers its submit |

```sh
MNEMA_URL=https://talk.example.org MNEMA_TOKEN=mnb_... go run ./examples/slash
```

## Handlers

Register handlers before `Run`; several per event are allowed and run in
order.

| Method | Dispatch | Intent |
|---|---|---|
| `OnReady(func(ctx, *Ready))` | `READY` | none |
| `OnResumed(func(ctx))` | `RESUMED` | none |
| `OnChannelsUpdate(func(ctx, *ChannelsUpdate))` | `CHANNELS_UPDATE` | `IntentGuilds` |
| `OnMemberAdd(func(ctx, *MemberAdd))` | `GUILD_MEMBER_ADD` | `IntentGuildMembers` |
| `OnMessageCreate`, `OnMessageUpdate(func(ctx, *Message))` | `MESSAGE_CREATE`, `MESSAGE_UPDATE` | `IntentGuildMessages` |
| `OnMessageDelete(func(ctx, *MessageDelete))` | `MESSAGE_DELETE` | `IntentGuildMessages` |
| `OnReactionsUpdate(func(ctx, *ReactionsUpdate))` | `MESSAGE_REACTIONS_UPDATE` | `IntentGuildMessageReactions` |
| `OnInteraction(func(ctx, *Interaction))` | `INTERACTION_CREATE` | none |
| `OnDispatch(func(ctx, name, json.RawMessage))` | every dispatch, also unknown ones | |

**Concurrency.** Each dispatch runs its handlers in a goroutine of its own, so
a slow handler never stalls heartbeats or the reading of further events. At
most `Options.HandlerConcurrency` (default 64) dispatches are handled at once;
further ones wait for a free slot without holding up the connection, up to
16 times that many. Beyond that the client drops events with a logged
warning (never READY, RESUMED or an interaction, which always get their
turn) rather than stop reading the connection (which would leave heartbeat
ACKs unread and end in a reconnect that replays the same flood): a handler
that waits on something slow should hand the work off and return. Events can therefore
be handled concurrently and out of order: guard shared state yourself. A panic
in a handler is recovered and logged with its stack; the client keeps
running. When `Run` returns, the handlers' context is cancelled and `Run`
waits for them first, so no goroutine outlives it.

The bot's own messages are dispatched to it too; check `m.IsBot` (or compare
`m.UserID` with `Ready.User.ID`) to ignore them. `m.MentionsUser(id)` tells
whether a message mentions the bot.

`c.SetPresence(ctx, mnema.Presence{Status: "dnd", Activity: &mnema.Activity{Name: "chess", Type: mnema.ActivityPlaying}})`
sets the status (gateway op 3). It is kept and sent again with the next
IDENTIFY and after a resume; `Options.Presence` sets the starting one. The
server applies at most 5 updates a minute.

## REST

`c.REST` (or `mnema.NewREST(opts)` without a gateway) has one method per route:
`Me`, `Gateway`, `Channels`, `Messages(ctx, ch, mnema.MessageQuery{Limit, Before, After, Around})`,
`Message`, `CreateMessage(ctx, ch, mnema.MessageCreate{Content, Embeds, Components, AllowedMentions, MessageReference, ParentID})`,
`EditMessage(ctx, ch, id, mnema.MessageEdit{Content, Components})`, `DeleteMessage`,
`AddReaction`, `RemoveReaction`, `Upload(ctx, ch, mnema.Upload{Name, Reader, Content, ...})`,
`Members`, `Member`, `Commands`, `SetCommands`, `CreateCommand`, `DeleteCommand`.
Ids are UUID strings. Optional fields of edits are pointers: `mnema.Ptr("text")`;
`Components: &[]mnema.Component{}` removes the components.

A non-2xx answer is a `*mnema.RESTError{Status, Code, Message}` with Discord's
codes (`mnema.CodeUnknownChannel`, `CodeMissingPermissions`, ...):

```go
var re *mnema.RESTError
if errors.As(err, &re) && re.Code == mnema.CodeMissingPermissions { ... }
```

### Rate limits

The client keeps within the server's limits before it is told so: **50
requests per second** per bot and **5 messages per 5 seconds per channel** for
posts and uploads (each channel has its own bucket, other routes have one per
route). A `429` is waited out (`retry_after` of the body, else the
`Retry-After` header; a `global` one holds back every request) and sent again,
at most `Options.MaxRetries` times (default 3); then the `429` is returned as a
`*RESTError` with `RetryAfter` and `Global`. Waiting stops when the context
ends. Slow mode answers `429` the same way. Requests carry
`User-Agent: mnema-talk-sdk-go/<module version>` and nothing personal.

## Slash commands and interactions

```go
_, err := c.REST.SetCommands(ctx, []mnema.CommandInput{{
	Name: "roll", Description: "Rolls a die",
	Options: []mnema.CommandOption{{Name: "sides", Description: "Sides", Type: mnema.OptionInteger}},
}})

c.OnInteraction(func(ctx context.Context, i *mnema.Interaction) {
	switch {
	case i.CommandName() == "roll":
		_ = i.DeferReply(ctx, false)
		_, _ = i.EditReply(ctx, mnema.Edit{Content: mnema.Ptr(fmt.Sprint(rand.Int64N(max(i.Option("sides").Int(), 2)) + 1))})
	case i.IsButton() && i.CustomID() == "again":
		_ = i.Update(ctx, mnema.Edit{Content: mnema.Ptr("again!")})
	}
})
```

An `Interaction` has `ID`, `Token`, `Type`, `ChannelID`, `User`, `Message` (the
message a component is on) and `Data`, plus helpers: `CommandName()`,
`Option(name)` with `String()`, `Int()`, `Bool()` (zero values when missing),
`CustomID()`, `Values()`, `Field(customID)` for modal submits, and
`IsCommand`, `IsComponent`, `IsButton`, `IsSelect`, `IsModalSubmit`.

| Method | Callback | For |
|---|---|---|
| `Reply(ctx, mnema.Response{Content, Embeds, Components, AllowedMentions, Ephemeral})` | 4 | commands, components, modal submits |
| `DeferReply(ctx, ephemeral)` | 5 | the same; answer later with `EditReply` or `FollowUp` |
| `Update(ctx, mnema.Edit{...})` | 7 | components (and modal submits opened from one) |
| `DeferUpdate(ctx)` | 6 | the same |
| `ShowModal(ctx, mnema.Modal{...})` | 9 | commands and components |
| `EditReply(ctx, mnema.Edit{...})`, `DeleteReply(ctx)` | `PATCH`/`DELETE .../messages/@original` | after the first answer |
| `FollowUp(ctx, mnema.Response{...})` | `POST .../followups` | up to 5 responses in total |

The first answer must come within 15 minutes and can be given once (a second
one returns `mnema.ErrAlreadyAnswered`). `EditReply` and `FollowUp` return a
nil message for an ephemeral response, which is never stored. `Ephemeral`
belongs to `Reply`, `DeferReply` and `FollowUp`; `mnema.Edit` (for
`EditReply` and `Update`) has none, because the first response decides and
the server refuses flags on an edit. `AllowedMentions` works as on
`CreateMessage`: without it an answer notifies the members it names, never
`@all`, `@here` or a role. `i.User` is always set: the server sends the member
with every interaction. `Ready.User.Handle` is the name members mention the
bot by (`@bot-...`); `Ready.User.Username` is its display name.

## Components and modals

```go
rows := []mnema.Component{
	mnema.ActionRow(
		mnema.SuccessButton("deploy:v2.4", "Deploy"),
		mnema.SecondaryButton("cancel", "Cancel"),
		mnema.LinkButton("https://example.org/changes", "Changelog"),
	),
	mnema.ActionRow(mnema.StringSelect{CustomID: "env", Placeholder: "Environment", Options: []mnema.SelectOption{
		{Label: "Staging", Value: "staging"}, {Label: "Production", Value: "prod", Default: true},
	}}),
}
_, err := c.REST.CreateMessage(ctx, ch, mnema.MessageCreate{Content: "Deploy v2.4?", Components: rows})

modal := mnema.NewModal("feedback", "Feedback",
	mnema.TextInput{CustomID: "subject", Label: "Subject", MaxLength: mnema.Ptr(80)},
	mnema.TextInput{CustomID: "body", Label: "What happened?", Style: mnema.TextInputParagraph, Required: mnema.Ptr(false)},
)
```

Builders: `ActionRow`, `Button{...}` and `PrimaryButton`, `SecondaryButton`,
`SuccessButton`, `DangerButton`, `LinkButton`, `StringSelect{...}`,
`UserSelect{...}`, `RoleSelect{...}`, `ChannelSelect{...}`, `TextInput{...}`,
`NewModal`. `mnema.Component` is Discord's JSON and can be built by hand too.
`Validate()` on each builder, `mnema.ValidateComponents(rows)` and
`Modal.Validate()` check the documented limits (5 rows, 25 components, 5
buttons or one select per row, `custom_id` 1 to 100 characters and unique,
labels, options, placeholders, min/max values, text input lengths); the SDK
runs them before every request that carries components or a modal, and the
errors wrap `mnema.ErrInvalidComponent`.

## Reconnects

`Run(ctx)` fetches the gateway address (`GET /api/bot/v1/gateway`, or
`Options.GatewayURL`), connects with `Authorization: Bot <token>` on the
upgrade, and identifies with the intents and the presence. It sends
heartbeats every interval, the first after a random part of it; a heartbeat
whose ACK has not come by the next one drops the connection. After a drop it
reconnects and **resumes** (session ID and last sequence number, within the
server's 2 minutes); when the server answers `INVALID_SESSION` it identifies
again on the same connection, and after `4000`, `4003` or `4005` it starts a
new session. Reconnects wait with exponential backoff and jitter from 1 to 60
seconds (at least 30 seconds after `4008`). Outgoing frames stay within 4096
bytes (`mnema.ErrFrameTooLarge`) and 120 a minute.

`Run` returns `nil` when the context ends, a `*mnema.CloseError` on the fatal
close codes `4004` (bad or revoked token, disabled or banned bot), `4013` and
`4014` (intents), and a `*mnema.RESTError` when `GET /gateway` answers 401 or
403.

## The add link

A bot's README can offer an add link that opens the server's confirm screen
with its manifest ([Adding a bot from a manifest](../../doc/bots.md#adding-a-bot-from-a-manifest)):

```go
link, err := mnema.AddBotLink("https://talk.example.org", mnema.Manifest{
	Name:        "Mnemo",
	Description: "Answers questions and keeps notes.",
	Intents:     (mnema.IntentGuilds | mnema.IntentGuildMessages).Names(),
	Permissions: []string{"view_channel", "send_messages", "add_reactions"},
	Commands:    commands,
})
// https://talk.example.org/admin/bots/add#manifest=<base64url JSON>
```

The manifest is validated first (name, description, https homepage, avatar
data URL, intent and permission names, commands) and travels in the URL
fragment, which the browser never sends to the server.

## Tests

```sh
go test -race ./...
```

The unit tests run offline against `httptest` servers, including a WebSocket
gateway. The **contract tests** (build tag `contract`) run against a real
server and are skipped without `MNEMA_CONTRACT_URL`:

```sh
MNEMA_CONTRACT_URL=http://127.0.0.1:8080 MNEMA_CONTRACT_TOKEN=mnb_... \
MNEMA_CONTRACT_CHANNEL=<channel id> MNEMA_CONTRACT_MEMBER_COOKIE='<Cookie header of a member>' \
go test -count=1 -tags=contract -run Contract ./...
```

The bot must be able to view and send in the channel and be allowed
`GUILDS|GUILD_MESSAGES|GUILD_MESSAGE_REACTIONS`; the member must be able to view
and send there. The server's integration suite runs them on every change
(`TestSDKContract` in `internal/server`, with `SDK_CONTRACT=1`).
