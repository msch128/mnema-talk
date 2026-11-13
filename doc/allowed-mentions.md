# Allowed mentions

`allowed_mentions` is how a bot or a webhook says whom a message may notify. It
is Discord's object, with Mnema Talk's rules. This page is the one place for
all of them; [bots](bots.md) and [webhooks](webhooks.md) link here.

A mention in the text (`@username`, `@all`, `@here`, `@Role Name`) always stays
visible as text. `allowed_mentions` decides only whether it **notifies**: the
mention badge, push and the notification sound. A name that is left as plain
text addresses nobody.

## The object

```json
{ "parse": ["users", "roles"], "users": ["<member id>"], "roles": ["<role id>"] }
```

| Field | Meaning |
|---|---|
| `parse` | The kinds that notify: `users` (`@username`), `everyone` (`@all` and `@here`), `roles` (`@Role Name`) |
| `users` | Member ids (UUIDs) that notify although `users` is not in `parse` |
| `roles` | Role ids (UUIDs) that notify although `roles` is not in `parse` |

All three are optional. An id that is not a UUID is ignored. The object can
only **narrow** what the sender may do, never widen it: asking for `everyone`
does nothing unless the sender is allowed to mention everyone.

## Without the object, and with an empty one

| Request | Who is notified |
|---|---|
| no `allowed_mentions` (or `null`) | the members named by `@username` who can view the channel; never `@all`, `@here` or a role |
| `{}` or `{"parse": []}` | nobody: every mention stays plain text |
| `{"parse": ["users"]}` | the same members as without the object |

So an existing bot that never sends the object notifies members it names and
nobody else. To make a bot's reply quiet, send `{"parse": []}`. The Go SDK omits
an empty `Parse` from the JSON, so `&mnema.AllowedMentions{Parse: []string{}}`
sends `{}`, which means the same.

## Each kind

**Users.** With `users` in `parse`, every `@username` in the text notifies. Without
it, only the members listed in `users` do. A bot is a member and can be
mentioned too; `@all` and `@here` never reach bots.

**Everyone.** `@all` and `@here` notify only when both hold: `parse` contains
`everyone`, and the sender may mention everyone. For a bot that is the
*Mention @all and @here* permission in the channel. For a webhook it is the webhook's
`allow_everyone` setting, which is off by default.

**Roles.** `@Role Name` notifies the role's members when `parse` contains
`roles` (every role the sender may mention), or when the role's id is in
`roles`. Roles are off unless the request asks for them. The sender may mention
the roles marked mentionable; with *Mention @all and @here* (a bot) or
`allow_everyone` (a webhook) it may mention any custom role. `@everyone` and the
managed Bot roles can never be mentioned by name.

## What always applies

- Nobody who cannot view the channel is ever notified, whatever the object says.
- A sender that may **not** mention everyone notifies at most **50** members per
  message, named members first and then the members of mentioned roles, in a
  fixed order. The text and the role pills stay; the rest are not notified.
  With `@all` and `@here` allowed there is no cap, since they reach everyone.
- At most 100 distinct `@names` are read from one message.
- A muted channel stays silent except for a message that addresses the member by
  name, through a role they hold, or as a reply; `@all` and `@here` do not break
  a mute (see [architecture](architecture.md)).

## Where it is accepted

| Where | Notes |
|---|---|
| `POST /api/bot/v1/channels/{id}/messages` | the field `allowed_mentions` in the JSON body |
| `PATCH /api/bot/v1/channels/{id}/messages/{id}` | applies to the new text. An edit **without** the field notifies the members it names, never `@all`, `@here` or a role. An edit **with** it takes one post from the channel's limit of 5 per 5 seconds |
| `POST /api/bot/v1/channels/{id}/upload` | the form field `allowed_mentions`, JSON-encoded, for the caption: `{"parse":["everyone","roles"]}`. Without the field the caption notifies the members it names and nothing else |
| interaction responses (callbacks `4` and `7`, follow-ups, `PATCH @original`) | as on a post. An update (callback `7`) or `@original` edit that sends it takes one post from the channel's limit |
| `POST /api/webhooks/{id}/{token}` | the same object, narrowed by the webhook's settings, see below |

## Webhooks

A webhook has its own settings ([webhooks](webhooks.md#who-may-manage-webhooks)):
`allow_user_mentions` (on by default) and `allow_everyone` (off by default).
They are the webhook's, not any user's rights, and the request can only narrow
them:

- With the defaults, `@username` notifies and `@all` and `@here` do not.
- `@all` and `@here` need `parse: ["everyone"]` **and** `allow_everyone`.
- Roles need the object: without it, or with a `parse` that leaves out `roles`
  and no ids in `roles`, role mentions stay plain text.
- Unlike a bot, a webhook request is refused with `400` when `parse` contains
  `users` and `users` lists ids, or when `parse` contains `roles` and `roles`
  lists ids (Discord does the same). A bot request is not refused: `parse` wins
  and the list is redundant.
- With `allow_user_mentions` off, every member mention is plain text.

## In the SDKs

```ts
// TypeScript: reply without notifying anyone
await client.rest.createMessage(channelId, {
  content: `Thanks @${username}, noted.`,
  allowed_mentions: { parse: [] },
})
```

```go
// Go: notify one member and nobody else
_, err := c.REST.CreateMessage(ctx, channelID, mnema.MessageCreate{
	Content:         "@" + username + " your build is ready",
	AllowedMentions: &mnema.AllowedMentions{Users: []string{userID}},
})
```

Both SDKs send nothing by default, which is the first row of the table above.
