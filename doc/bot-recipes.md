# Bot recipes

Short answers to common bot tasks, each in TypeScript and in Go. They build on
the program of the [SDK quick start](bot-sdk-quickstart.md): `client` (TypeScript)
and `c` (Go) are the connected client, and a snippet goes where that program
registers its handlers. Every snippet is compiled against the SDK in the
repository before it is published here.

The TypeScript snippets use these imports:

```ts
import { RestError, TextInputStyle, actionRow, modal, stringSelect, textInput } from '@mnema-xyz/bot-sdk'
```

The Go snippets use `mnema "github.com/msch128/mnema-talk/sdk/go"` and the
standard packages they name. What the bot may do in a channel is decided by its
roles ([permissions](bots.md#permissions-roles-and-overwrites)); a call it may not
make fails with `403` and code `50013`, see [handle an error](#handle-an-error).

## Reply to a message

TypeScript:

```ts
client.on('messageCreate', async (message) => {
  if (message.user_id === client.user?.id || message.content !== '!hello') return
  await client.rest.createMessage(message.channel_id, {
    content: `Hello, ${message.display_name}!`,
    message_reference: { message_id: message.id },
  })
})
```

Go:

```go
c.OnMessageCreate(func(ctx context.Context, m *mnema.Message) {
	if m.IsBot || m.Content != "!hello" {
		return
	}
	_, err := c.REST.CreateMessage(ctx, m.ChannelID, mnema.MessageCreate{
		Content:          "Hello, " + m.DisplayName + "!",
		MessageReference: &mnema.MessageReference{MessageID: m.ID},
	})
	if err != nil {
		slog.Warn("reply", "err", err)
	}
})
```

The bot's own messages are dispatched to it too, so a handler that answers every
message must skip them (`message.user_id` against `client.user.id`, or `IsBot` in
Go, which also skips other bots and webhooks).

## Reply without notifying anyone

Send an empty `allowed_mentions`: names in the text stay visible and nobody is
notified ([allowed mentions](allowed-mentions.md)).

TypeScript:

```ts
await client.rest.createMessage(channelId, {
  content: `Build for @${author} finished.`,
  allowed_mentions: { parse: [] },
})
```

Go:

```go
_, err := c.REST.CreateMessage(ctx, channelID, mnema.MessageCreate{
	Content:         "Build for @" + author + " finished.",
	AllowedMentions: &mnema.AllowedMentions{},
})
return err
```

To notify exactly one member, list them instead: `allowed_mentions: { users: [userId] }`
(Go: `&mnema.AllowedMentions{Users: []string{userID}}`).

## Recognise a mention of the bot

A message that mentions the bot lists the bot's id in `mentions`; the id is
`client.user.id` (Go: `Ready.User.ID`, from `OnReady`).

TypeScript:

```ts
client.on('messageCreate', async (message) => {
  const me = client.user
  if (!me || message.user_id === me.id || !message.mentions.includes(me.id)) return
  await client.rest.addReaction(message.channel_id, message.id, '👀')
})
```

Go:

```go
var botID string
c.OnReady(func(ctx context.Context, r *mnema.Ready) { botID = r.User.ID })
c.OnMessageCreate(func(ctx context.Context, m *mnema.Message) {
	if m.IsBot || !m.MentionsUser(botID) {
		return
	}
	if err := c.REST.AddReaction(ctx, m.ChannelID, m.ID, "👀"); err != nil {
		slog.Warn("react", "err", err)
	}
})
```

Reacting needs *Add reactions*. The mention only reaches the bot when the sender's
request allowed it to notify the bot, so a message whose text names the bot but
sent `allowed_mentions: { parse: [] }` does not list it.

## Post an embed

TypeScript:

```ts
await client.rest.createMessage(channelId, {
  embeds: [{
    title: 'Run 42',
    description: 'All checks passed.',
    color: 0x2ecc71,
    fields: [{ name: 'Branch', value: 'main', inline: true }, { name: 'Time', value: '3m 12s', inline: true }],
    footer: { text: 'ci-bot' },
  }],
})
```

Go:

```go
_, err := c.REST.CreateMessage(ctx, channelID, mnema.MessageCreate{
	Embeds: []mnema.Embed{{
		Title:       "Run 42",
		Description: "All checks passed.",
		Color:       0x2ecc71,
		Fields: []mnema.EmbedField{
			{Name: "Branch", Value: "main", Inline: true},
			{Name: "Time", Value: "3m 12s", Inline: true},
		},
		Footer: &mnema.EmbedFooter{Text: "ci-bot"},
	}},
})
return err
```

A message needs content, embeds or components. Pictures in an embed are shown as
links, not loaded ([webhooks](webhooks.md#how-the-message-looks) has the subset).

## Answer in a thread

`parent_id` is the root message of the thread. For a message that is itself a
reply in a thread, use its own `parent_id`.

TypeScript:

```ts
client.on('messageCreate', async (message) => {
  if (message.user_id === client.user?.id || message.content !== '!details') return
  await client.rest.createMessage(message.channel_id, {
    content: 'Here are the details.',
    parent_id: message.parent_id ?? message.id,
  })
})
```

Go:

```go
c.OnMessageCreate(func(ctx context.Context, m *mnema.Message) {
	if m.IsBot || m.Content != "!details" {
		return
	}
	root := m.ID
	if m.ParentID != "" {
		root = m.ParentID
	}
	_, err := c.REST.CreateMessage(ctx, m.ChannelID, mnema.MessageCreate{Content: "Here are the details.", ParentID: root})
	if err != nil {
		slog.Warn("thread reply", "err", err)
	}
})
```

## Send a file

TypeScript:

```ts
const report = new Blob(['id,name\n1,Mara\n'], { type: 'text/csv' })
await client.rest.uploadFile(channelId, { file: report, name: 'report.csv', content: 'The weekly report' })
```

Go:

```go
_, err := c.REST.Upload(ctx, channelID, mnema.Upload{
	Name:    "report.csv",
	Reader:  strings.NewReader("id,name\n1,Mara\n"),
	Content: "The weekly report",
})
return err
```

The bot needs *Attach files* besides *Send messages*, and the upload limit that
applies to its roles holds.

## Edit and delete the bot's own message

TypeScript:

```ts
const sent = await client.rest.createMessage(channelId, 'Working on it…')
await client.rest.editMessage(channelId, sent.id, 'Done.')
await client.rest.deleteMessage(channelId, sent.id)
```

Go:

```go
sent, err := c.REST.CreateMessage(ctx, channelID, mnema.MessageCreate{Content: "Working on it…"})
if err != nil {
	return err
}
if _, err := c.REST.EditMessage(ctx, channelID, sent.ID, mnema.MessageEdit{Content: mnema.Ptr("Done.")}); err != nil {
	return err
}
return c.REST.DeleteMessage(ctx, channelID, sent.ID)
```

A bot edits only its own messages. Deleting other people's messages needs
*Manage messages*.

## Read the recent history

`GET /channels/{id}/messages` returns top-level messages (no thread replies),
oldest first within the page; without an anchor it is the newest page. Page back
with `before`.

TypeScript:

```ts
const page = await client.rest.listMessages(channelId, { limit: 50 })
const older = await client.rest.listMessages(channelId, { limit: 50, before: page[0]?.id })
console.log(page.length + older.length, 'messages')
```

Go:

```go
page, err := c.REST.Messages(ctx, channelID, mnema.MessageQuery{Limit: 50})
if err != nil || len(page) == 0 {
	return err
}
older, err := c.REST.Messages(ctx, channelID, mnema.MessageQuery{Limit: 50, Before: page[0].ID})
if err != nil {
	return err
}
fmt.Println(len(page)+len(older), "messages")
```

## Answer slowly: defer, then edit

An interaction can be answered for 15 minutes. Deferring first tells the member
that the bot is working on it; edit the result in when it is ready. `ephemeral`
shows the answer only to the member who ran the command.

TypeScript:

```ts
client.on('interactionCreate', async (interaction) => {
  if (!interaction.isCommand() || interaction.commandName !== 'report') return
  await interaction.deferReply({ ephemeral: true })
  const text = await buildReport() // the slow part
  await interaction.editReply(text)
})
```

Go:

```go
c.OnInteraction(func(ctx context.Context, i *mnema.Interaction) {
	if !i.IsCommand() || i.CommandName() != "report" {
		return
	}
	if err := i.DeferReply(ctx, true); err != nil {
		slog.Warn("defer", "err", err)
		return
	}
	text := buildReport() // the slow part
	if _, err := i.EditReply(ctx, mnema.Edit{Content: &text}); err != nil {
		slog.Warn("edit reply", "err", err)
	}
})
```

## A select menu

TypeScript:

```ts
await client.rest.createMessage(channelId, {
  content: 'Where to deploy?',
  components: [actionRow(stringSelect({
    customId: 'env',
    placeholder: 'Environment',
    options: [{ label: 'Staging', value: 'staging' }, { label: 'Production', value: 'prod' }],
  }))],
})

client.on('interactionCreate', async (interaction) => {
  if (interaction.isSelect() && interaction.customId === 'env') {
    await interaction.update({ content: `Deploying to ${interaction.values[0]}.`, components: [] })
  }
})
```

Go:

```go
_, err := c.REST.CreateMessage(ctx, channelID, mnema.MessageCreate{
	Content: "Where to deploy?",
	Components: []mnema.Component{mnema.ActionRow(mnema.StringSelect{
		CustomID:    "env",
		Placeholder: "Environment",
		Options:     []mnema.SelectOption{{Label: "Staging", Value: "staging"}, {Label: "Production", Value: "prod"}},
	})},
})
if err != nil {
	return err
}

c.OnInteraction(func(ctx context.Context, i *mnema.Interaction) {
	if i.IsSelect() && i.CustomID() == "env" {
		err := i.Update(ctx, mnema.Edit{Content: mnema.Ptr("Deploying to " + i.Values()[0] + "."), Components: &[]mnema.Component{}})
		if err != nil {
			slog.Warn("select", "err", err)
		}
	}
})
```

`update` with `components: []` removes the menu. The custom id is yours to name
and to read back; keep state you need in it (for example `deploy:prod`), because
the server keeps nothing for a component.

## Ask for input with a modal

A modal opens from a command or a button; its submit arrives as an interaction.

TypeScript:

```ts
client.on('interactionCreate', async (interaction) => {
  if (interaction.isCommand() && interaction.commandName === 'feedback') {
    await interaction.showModal(modal({
      customId: 'feedback-form',
      title: 'Feedback',
      components: [
        textInput({ customId: 'subject', label: 'Subject', maxLength: 80 }),
        textInput({ customId: 'body', label: 'What happened?', style: TextInputStyle.Paragraph, required: false }),
      ],
    }))
  } else if (interaction.isModalSubmit() && interaction.customId === 'feedback-form') {
    await interaction.reply({ content: `Thanks: ${interaction.fields.get('subject')}`, ephemeral: true })
  }
})
```

Go:

```go
c.OnInteraction(func(ctx context.Context, i *mnema.Interaction) {
	var err error
	switch {
	case i.IsCommand() && i.CommandName() == "feedback":
		err = i.ShowModal(ctx, mnema.NewModal("feedback-form", "Feedback",
			mnema.TextInput{CustomID: "subject", Label: "Subject", MaxLength: mnema.Ptr(80)},
			mnema.TextInput{CustomID: "body", Label: "What happened?", Style: mnema.TextInputParagraph, Required: mnema.Ptr(false)},
		))
	case i.IsModalSubmit() && i.CustomID() == "feedback-form":
		err = i.Reply(ctx, mnema.Response{Content: "Thanks: " + i.Field("subject"), Ephemeral: true})
	}
	if err != nil {
		slog.Warn("modal", "err", err)
	}
})
```

## Set the bot's status

TypeScript:

```ts
client.setPresence({ status: 'dnd', activity: { name: 'the build queue', type: 3 } })
```

Go:

```go
return c.SetPresence(ctx, mnema.Presence{Status: "dnd", Activity: &mnema.Activity{Name: "the build queue", Type: mnema.ActivityWatching}})
```

The server accepts 5 updates per minute and drops the rest. Types: `0` playing,
`2` listening, `3` watching, `4` custom, `5` competing ([presence
updates](bots.md#presence-updates)).

## Handle an error

A refused call is a `RestError` (Go: `*mnema.RESTError`) with the HTTP status and
Discord's code: `10003` unknown channel (also for a channel the bot cannot view),
`10008` unknown message, `50013` missing permissions. The SDK already waits out
`429`; it throws only after its retries.

TypeScript:

```ts
try {
  await client.rest.createMessage(channelId, 'hello')
} catch (err) {
  if (err instanceof RestError && err.code === 50013) {
    console.warn('no permission to post in', channelId)
  } else {
    throw err
  }
}
```

Go:

```go
_, err := c.REST.CreateMessage(ctx, channelID, mnema.MessageCreate{Content: "hello"})
var re *mnema.RESTError
if errors.As(err, &re) && re.Code == mnema.CodeMissingPermissions {
	slog.Warn("no permission to post", "channel", channelID)
} else if err != nil {
	return err
}
```

## What a bot cannot do

It cannot manage roles or members, so there are no reaction roles: use a
[self-assignable role](bots.md#what-a-bot-cannot-do) instead.
