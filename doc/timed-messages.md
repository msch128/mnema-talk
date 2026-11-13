# Timed messages, auto-delete and removed files

Three related features: a message can be sent with a **lifetime** after which
the server deletes it; the Owner and Administrators get a finer
**auto-delete** for channels (hours, days or weeks, the files only, a community
default); and a file that is gone shows a quiet **"File removed"** tile in its
place. Introduced in 3.1. The server advertises the lifetimes as
`messages.lifetime` and the new auto-delete as `channels.auto_delete.v2` in the
`features` of `GET /api/health`; a client offers neither unless it is listed.

## What it is not

A lifetime decides when the server deletes a message. It does not protect the
message from anyone who could read it before that:

- Members can copy it, quote it, take a screenshot or keep what their client
  received. A client that does not follow the rules (an old app, a script, a
  bot) need not hide it at all.
- Text is not end-to-end encrypted. The server can read it while it exists,
  and backups made while it existed keep it.
- A push notification that was already delivered stays on the device. A
  message whose lifetime is under 10 minutes is pushed as "New message" alone,
  with no text, no sender and no channel or conversation (a burst of messages
  that ends in one shows only the count), so its words do not end up in a
  notification centre; longer lifetimes push as usual.
- A report filed before the end keeps its copy of the message for the
  moderators, as it does for a deleted message: a lifetime does not make a
  message unreportable. A data export taken before the end contains it.

The composer says this in one line next to the timer.

## As a member

**Sending.** The timer button next to the composer opens a small menu: *Off*,
*1 minute*, *5 minutes*, *1 hour*, *8 hours*, *1 day*, *1 week* and *Custom*
(minutes, hours or days). The chosen lifetime shows as a small chip next to the
send button and stays for that channel or conversation (remembered on this
device) until it is set to *Off*. The clock starts when the message is sent,
not when somebody reads it. The shortest lifetime is 60 seconds, the longest 30
days or less when the community set a lower ceiling. Lifetimes work in
channels, Talk chats, conversations and group conversations, and in threads:
the thread composer has the same timer, and a thread keeps its choice apart
from its channel's. Files sent
with the message go with it. A message written offline keeps its lifetime in
the outbox; the lifetime starts when the server stores it.

**Reading.** A message with a lifetime carries a small mark with the time left
("5 min", "3 h", "2 d", updated as it runs down). At the end it leaves the list
on everybody's screen at once, the same way a deleted message does, even if the
server's sweep is a few seconds late: every client hides it on its own clock,
and every read of the server filters it out by its end time.

**What goes with it.** At the end the message is deleted exactly as if its
author had deleted it: its files, reactions and edit history; its thread
replies, if it was a thread root; the saved entries of everyone who saved it;
reminders on it turn into "a message you can no longer see". Replies that
quoted it show "Original message deleted". Unread and mention counts are
corrected by the recount after the delete; until the sweep has run (about a
second) a count may still include the ended message, as the counts are read
from an index that does not hold the end.

**What it cannot be.** A message with a lifetime cannot be pinned (the pin is
refused with "A message with a lifetime cannot be pinned"), forwarded (the
forward would outlive the original) or edited into a message without one: an
edit changes the text only, never the lifetime. Replying to it and saving it
work as for any message. A scheduled message can carry a lifetime; it starts
when the server posts the message. Forum posts (the root of a post) are meant
to last and take no lifetime; replies in a post may have one. Polls and
built-in commands (`/me`, `/shrug` and the others) are sent without one.

## As a moderator

A moderator sees and removes messages with a lifetime like any other; a
moderator's delete is audited as always. The expiry itself writes no audit
entry: it is the author's own choice, like deleting one's own message.

**Auto-delete** of a channel (*Manage channels*, **Channel settings > Delete
old messages**): *Use the community default*, *Off*, or a period of hours, days
or weeks (1 hour to 365 days), and what to delete:

- **Messages and their files**: what is older than the period goes, as before.
  Pinned messages stay, and so does a thread root while it has a reply that is
  newer than the period or pinned.
- **Files only**: the files of messages older than the period are removed; the
  text stays and every file shows the "File removed" tile. Files of pinned
  messages stay.

The settings show in plain words what will be deleted and when the next check
is (every ten minutes, on the server's clock). A change that would delete content at once (a shorter period, or turning
it on where old content exists) first shows how many messages and files it
would remove and asks for confirmation. Conversations and the Talks started
from them are never touched by auto-delete.

## As an operator (Owner and Administrators)

**Admin > Messages**:

- **Messages with a lifetime**: on (the default) or off. Off refuses new
  messages with a lifetime everywhere; messages that already have one still end
  on time.
- **Longest lifetime**: the ceiling members may choose, 1 minute to 30 days
  (30 days by default). It holds for everyone, the Owner included.
- **Auto-delete default**: off (the default), or a period and a scope that
  every channel without a setting of its own follows. A channel opts out with
  *Off*. Turning it on or shortening it shows the counts across all channels
  that follow it and asks for confirmation; the confirmation says that every
  channel without a period of its own follows it.

Changing the default is the audit entry `settings.auto_delete_default_set`,
changing the lifetime settings `settings.message_lifetimes_set`. The channel
entries stay `channel.auto_delete_set` and `channel.auto_delete_run`, now with
`hours` and `scope` in their details.

**The sweeper.** Lifetimes are enforced by a worker that sleeps until the next
end and wakes when a message with an earlier end is stored, so a message
goes within about a second of its end. It works through ended messages in
batches of 100, one transaction each, and on start removes everything that
ended while the server was down. Reads never depend on it: history, threads,
search, the mentions inbox, pins, saved messages, reminders, exports, reply
previews, forward quotes, new report copies, link cards and the bot API all
leave out a message whose end has passed. Auto-delete keeps its own sweep
every ten minutes.

## Removed files

A file that is gone (removed by a moderator or the Owner, by auto-delete with
*Files only*, by media retention, or missing in storage) shows a tile in the
theme's surface colour with a small icon and the words "File removed", in the
place and at the size the file had. The server measures the width and height
of a picture at upload (`width`, `height` on the attachment) and keeps them
when the file is removed, so the tile takes the picture's size (never wider
than the column) and nothing jumps; pictures uploaded before 3.1 and videos use
the standard tile size of their kind. Any other file (a text, a PDF, audio)
keeps its row: its name, struck through, and "File removed" in place of its
size. A removal by the Owner or by auto-delete reaches open clients at once
(`message_update`); media retention shows on the next load. A reply preview of a
message whose files are all removed says "File removed", and the image viewer
closes when its message is deleted or ends. The tile is drawn in code, not an
image, and is announced to screen readers as "File removed: name".

## API

All changes are additive ([API stability](api-stability.md)).

| Where | What |
|---|---|
| `POST /api/channels/{id}/messages` | Optional `lifetime_seconds` (60 to 2,592,000, at most the community's ceiling). Refused with `400 INVALID_INPUT` when out of bounds, when lifetimes are off, or in a forum channel's root. |
| `POST /api/channels/{id}/upload` | Optional form field `lifetime_seconds`, as above. |
| `POST /api/channels/{id}/scheduled` | Optional `lifetime_seconds`; checked when scheduling and again when the server posts it (a refusal there turns the scheduled message into a failed draft as usual). It is fixed when scheduling: `PATCH /api/me/scheduled/{id}` does not change it (delete and schedule again). The scheduled message carries `lifetime_seconds`. |
| Bot API `POST /api/bot/v1/channels/{id}/messages` | Optional `lifetime_seconds`, as for members: a bot posts through the same path. |
| Incoming webhooks | Not accepted: the request follows Discord's execute format, which has no such field, and a webhook's channel is better served by auto-delete. |
| Message payload (REST, `message_create`, `message_update`, bot gateway, outgoing webhooks) | `expires_at` (RFC 3339), present only on a message with a lifetime. |
| `PUT /api/channels/{id}/messages/{messageID}/pin`, `POST /api/messages/{id}/forward` | `400 INVALID_INPUT` for a message with a lifetime. |
| `GET /api/message-settings` | Every member: `message_lifetimes_enabled`, `message_lifetime_max_seconds`; a change comes with `channels_changed`. |
| `GET /api/admin/message-settings` | Owner and Administrators: the lifetime switch and ceiling, the auto-delete default (`auto_delete_default_hours`, `auto_delete_default_scope`) and `auto_delete_next_run_at`. |
| `PUT /api/admin/message-settings/lifetimes` | Owner and Administrators: `message_lifetimes_enabled`, `message_lifetime_max_seconds` (audited as `settings.message_lifetimes_set`). |
| `PUT /api/admin/message-settings/auto-delete` | Owner and Administrators: `hours` (0 to 8760), `scope` (audited as `settings.auto_delete_default_set`). |
| `POST /api/admin/message-settings/auto-delete/preview` | Owner and Administrators: the same body; the messages and files the default would delete of what exists now, over its next runs (a run removes at most 5,000 per channel), across all channels that follow it. Files removed before are not counted. |
| `PUT /api/admin/channels/{id}/auto-delete` | Still takes `days` (0, 1, 7, 30, 90). New: `hours` (0 to 8760; wins over `days`), `scope` (`all`, `attachments`), `use_default` (follow the community default). |
| `POST /api/admin/channels/{id}/auto-delete/preview` | *Manage channels* (the editor of the setting; the counts are of a channel they can read anyway): the same body; the messages and files it would delete of what exists now, over its next runs, and when the next run is. |
| Channel payload | New: `auto_delete_hours` (the effective period, 0 = off), `auto_delete_scope`, `auto_delete_follows_default`. `auto_delete_days` keeps its meaning for whole days and otherwise answers the effective period rounded down to whole days, held within 1 to 90 while it is on (the range older clients and the 3.0 OpenAPI document accept: a period of a year answers 90); with the scope *Files only*, and when off, it answers 0, as no message is deleted. `auto_delete_follows_default` is never set for a conversation or its call Talk. |
| Attachment payload | New: `width`, `height` for pictures the server measured. |
| Reply preview (`reply_to` of a message) | New: `attachments_removed`, set when the quoted message had files and every one of them was removed. |
| `DELETE /api/admin/media/{id}` | Now also sends `message_update` for the message the file was attached to. |

## Limits

- Lifetime: 60 seconds to 30 days, fixed at sending; the community's ceiling
  applies to everyone.
- The worker deletes up to 100 ended messages per batch; a backlog after a long
  downtime is worked off in a loop of batches at start.
- Auto-delete: 1 hour to 365 days (stored in hours); runs every ten minutes,
  at most 25 batches of 200 messages (or files) per channel and run. Each
  batch holds the channel's row and the settings with a share lock and reads
  the setting again first, so a change stops a run at once.
- Upgrading: a channel whose auto-delete was off by its own setting (its last
  `channel.auto_delete_set` entry) stays off; every other channel without a
  period follows the community default, which is off until set
  ([upgrade notes](upgrade.md#messages-with-a-lifetime-auto-delete-in-hours-migration-0106)).
