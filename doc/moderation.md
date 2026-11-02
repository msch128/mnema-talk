# Moderation and the audit log

This page describes moderation (timeouts, bans, kicking, message moderation,
reports) and the audit log that records it. Code: [`internal/auth/moderation.go`](../internal/auth/moderation.go),
[`internal/perm`](../internal/perm/) and [`internal/audit`](../internal/audit/).
Permissions and the hierarchy are in [roles-and-permissions.md](roles-and-permissions.md).

## Who may moderate whom

Every moderation action needs its permission **and** a highest role strictly
above the member's. The Owner can never be targeted, and nobody can moderate
themselves. The checks run on the server inside the transaction that changes
the member, under the permission lock, so a concurrent role change cannot move
the target out of reach between the check and the write.

| Action | Permission | Notes |
|---|---|---|
| Timeout, end a timeout | *Timeout members* | `POST` / `DELETE /api/admin/users/{id}/timeout`; `/timeout` in the composer |
| Ban, unban, list bans | *Ban members* | `POST` / `DELETE /api/admin/users/{id}/ban`, `GET /api/admin/bans`; `/ban` in the composer |
| Disable an account | *Manage members* | **Disable**: ends sessions and blocks sign-in until **Enable**. No record beyond the audit log |
| Kick | *Kick members* | `/kick` in the composer, see [Kicks](#kicks) |
| Remove from a Talk | *Disconnect members* | Removes the member from that Talk only; they stay signed in. A role can hold it server-wide or through an overwrite on one Talk or category |
| Read and close reports | *Manage reports* | See [Blocking and reports](#blocking-and-reports) |
| Automod rules | *Administrator* | See [Automod](#automod); no new permission bit |
| Moderation history and notes of a member | *Manage reports* | See [Moderation history and moderator notes](#moderation-history-and-moderator-notes) |

## Timeouts

A timeout silences a member until a time passes: they can still read, and still
delete or take back their own words, but they lose *Send messages*, *Attach
files*, *Add reactions*, *Mention @all and @here*, *Connect*, *Speak*, *Video*
and *Send direct messages* (`perm.TimeoutMask`). Durations run from 5 seconds to 28 days; the interface
offers 1 min, 5 min, 1 h, 1 day, 1 week or a custom time, with an optional
reason of up to 200 characters.

It is enforced in the permission snapshot, so every surface honours it with no
extra check: REST (messages, replies, edits, reactions, uploads), the WebSocket
hub (joining a Talk, typing), and the SFU (it drops audio and video without
*Speak* and *Video*). Channel overwrites cannot hand back what a timeout took.
Starting a timeout rebuilds the permission state at once, which removes the
member from the Talk they are in (`voice_kicked`, `missing_permission`). It ends
by itself: the end time is compared at every check, so nothing has to run, and
the client counts down and refetches its permissions when it reaches zero.

The member gets a `moderation_notice` event (shown as "You were timed out for
1 h: spam") and sees a banner with the remaining time and the reason
(`GET /api/permissions` carries `timeout_until` and `timeout_reason`). Everyone
sees that a member is timed out in the member list (`timeout_until`), but not
the reason.

## Bans

A ban ends the member's sessions and connections and blocks signing in, with an
optional reason and an optional duration (otherwise it lasts until someone lifts
it). The account and its messages stay. A ban is its own state, separate from
Disable: banning does not disable the account, and lifting a ban (or its expiry)
never re-enables an account that was disabled before, just as Enable never
lifts a ban. Every check of whether an account may act (sign-in, sessions,
WebSockets, mentions, the member list) uses one condition
(`auth.ActiveSQL`) that covers both, and a ban whose time has passed stops
counting at once, before the expiry job (it runs every minute) clears it.

By default the invite the account registered with is revoked, so that code
cannot create the account anew; untick it in the dialog (`revoke_invite:
false`) for an invite shared with others. Deleting an invite is *Manage
invites* territory, so the invite is only revoked when the person banning also
holds that permission (or is the Owner); a moderator with *Ban members* alone
bans the account and the invite stays valid. Registration remembers the invite
(`users.invite_id`). Banning by IP address is deliberately not offered: behind
a proxy an address is shared or spoofable, and the trusted-proxy model makes it
a weak signal.

A banned member who signs in with the right password sees the reason and the end
of the ban; with a wrong password the answer does not reveal the ban. The
admin console has a **Bans** tab (for *Ban members*) listing active bans with
reason, moderator and end, and **Unban**.

## Kicks

A kick sends a member away for now and leaves no sanction. It is
the "remove now, may come back" action: every session of the member ends (browser, desktop client and
push devices), their live connections close, and they leave the Talk they are
in. They can sign in again at once; nothing is stored against the account
(compare *Disable*, which blocks sign-in, and *Ban*). The member first receives
a `kicked` WebSocket event (`{"reason": "..."}`) and the web app shows a calm
note; the connection closes a moment later.

The permission is *Kick members* (bit `1 << 23`). It is off for @everyone and
for every seeded role, so by default only the Owner and Administrators can
kick. The role hierarchy is enforced like for a ban: a highest role strictly
above the member's, never the Owner, never oneself. The only way to kick is
the built-in `/kick @member [reason]` command (`POST
/api/channels/{id}/interactions`, [bots](bots.md#built-in-commands-and-polls)):
the server checks the permission again inside the command and writes the audit
entry `moderation.kick` (details: `reason`, `via: slash`). `/timeout` and
`/ban` run the same actions as the admin console and write the same
`moderation.timeout` and `moderation.ban` entries, also marked `via: slash`.

## Slow mode

A text or Talk-chat channel can make members wait between two messages
(`slow_mode_seconds`, 0 to 86400, set with the channel edit by *Manage
channels*; a channel chip shows it). It counts posts, thread replies and
uploads of one member in that channel. Anyone who holds *Manage messages* in
the channel (the Owner always) is exempt, and so are webhooks. The refusal is
`429 RATE_LIMITED` with `Retry-After`. The check and the insert share a
transaction and concurrent posts of one member in one channel are serialized
with an advisory lock, so a burst cannot slip several messages through the same
gap.

## Raid protection

Raid protection keeps a sudden wave of new accounts calm. It has three parts,
all under **Admin > Raid protection** (Owner and Administrators only; every
change is audited as `settings.raid` or `settings.lockdown`). Code:
[`internal/raid`](../internal/raid/) (the settings and the door),
[`internal/perm`](../internal/perm/newmember.go) (the waiting times).
It sits beside the automod *Raid protection* rule, which times out a wave
after it came in; this part keeps the wave out or holds it back.

**Lockdown.** One click shuts the door: while it is on nobody new can sign
up, not even with a valid invite (`503 UNAVAILABLE`, "sign-ups are paused for
now"). *Also pause creating invites* stops `POST /api/admin/invites` as well
(`409`). It ends by itself after 1, 6 or 24 hours if you choose, otherwise
when you switch it off. Existing members, their sessions and the Owner are
untouched. While it is on, administrators see a banner under the header with
an *End lockdown* button.

**Sign-up limit.** At most *N* new members within *M* minutes (0 turns it
off; the window is 1 to 1440 minutes). When the window is full, further
sign-ups get the same calm "paused" answer until there is room again. The
count is the accounts created within the window (bots and deleted accounts
excluded); the check runs before the password is hashed, so a closed door costs
nothing.

**Waiting time of new members.** Three settings, each 0 (off) to 30 days:
how long after joining a member may **write** (send messages, polls, uploads),
**react**, and **speak** in Talks (the microphone, and with it camera and screen share). A member who is too new keeps reading, joining
Talks and listening. The gate lives in the permission snapshot, like a
timeout, so REST, the WebSocket hub (reactions) and the SFU (audio) enforce it
from one place and it ends by itself without a reload. The refusal is
`403 FORBIDDEN` with the seconds left ("new members can write in 600 s"); the
client shows it as "You can write in 10 min." in the composer, on the reaction
button and on the Talk's microphone button, and fetches the permissions again
when the wait is over. `GET /api/permissions` carries `new_member` (`write_at`,
`react_at`, `speak_at`) while a wait is running.

When a wait ends the server tells the member's clients and the Talk they are in
(one timer for all members, set again after every load of the permission
snapshot), so the microphone and the Stage's raised hand work at that moment
without rejoining. Turning a gate on later holds members who joined recently by
their real sign-up time.

Who never waits: the Owner, bots, Administrators, and members with a role that
holds **Bypass new member limits** (bit 26, [roles](roles-and-permissions.md);
Moderator and Admin roles get it by migration 0083). It is separate from the
slow mode exemption. **Direct messages are exempt**: conversations, and the
Talks started from one, are not held back, because their members chose each
other. A member the permission snapshot does not know yet (it is reloaded at
least every minute) counts as having just joined.

## Auto-delete

*Manage channels* can let a channel forget: **Channel settings > Delete old
messages** with *1 day*, *7 days*, *30 days* or *90 days* (off by default, and
never for direct messages, which the database refuses). Turning it on asks
for confirmation, because the deletion cannot be undone. The channel then
shows a timer chip in its header and a line under it, so nobody writes
unaware.

A sweeper runs every ten minutes, removes the messages older than the period
in batches of 200 (at most 25 batches per channel and run, the rest on the next
run) and deletes their attachments through the same path as a deleted message.
Clients get `message_delete` for each. **Pinned messages stay**, and so does a
thread root while it has a reply that is newer than the period or pinned; the
old replies of such a thread go. Turning auto-delete off stops a run at once.
Every run that removed something is one `channel.auto_delete_run` audit entry
with the counts; changing the setting is `channel.auto_delete_set`. The
audit entries of `message.delete` are *not* written for these deletions (they
would hold the text of everything the channel was told to forget).

## Message moderation

Holders of *Manage messages* in a channel can:

- **Pin and unpin** a message (`PUT` / `DELETE
  /api/channels/{channelID}/messages/{messageID}/pin`); a pin icon marks it.
  *Manage messages* is the pin permission as well (no separate bit): whoever
  may remove a message in a channel may also pin one there, and the Owner
  always may. A channel keeps at most 50 pins (a 51st answers `409`); pinning a
  pinned message changes nothing. `GET /api/channels/{channelID}/pins` lists the
  pins, newest pin first, for anyone who can view the channel, and the channel
  header opens that list with a jump to each message. A real pin or unpin
  broadcasts `message_update` and `pins_changed` to the channel's viewers only;
  a pin also shows a compact note under the messages of every open client (it
  is not a stored message, so it is gone after a reload; the list is the
  record). Both actions are audited (`message.pin`, `message.unpin`). Deleting a
  message unpins it. A pinned message carries `pinned_at` and `pinned_by` (the
  member who pinned it, absent once that account is gone) on every payload and
  on the `pins_changed` event, and the pinned list shows "Pinned by".
- **Delete with a reason**: `DELETE /api/channels/{channelID}/messages/{messageID}`
  takes an optional JSON body `{"reason": "..."}`. The author is told through a
  `moderation_notice`; the audit entry (`message.delete`, `moderator: true`)
  holds the full text and the reason. The Owner's messages can still only be
  deleted by the Owner.
- **Delete a member's latest messages** (`POST
  /api/channels/{channelID}/messages/bulk-delete` with `user_id`, `count` 1 to
  100 and a reason): replies and attachments go with them. It needs a higher
  role than the member (checked in the same transaction as the delete) and
  leaves threads that hold a reply of the Owner alone. Every message gets its own
  audit entry with its text, plus one `moderation.bulk_delete` summary.
- **Read the edit history** of an edited message (`GET
  /api/channels/{channelID}/messages/{messageID}/edits`, "Edit history" in the
  message menu of an edited message). See below.

### Edit history

Every edit keeps the version it replaces: its text, when that version was
written (the creation time for the original) and who wrote it. Only members who
hold *Manage messages* in the channel and can view it (the Owner always) can
read the versions, newest first. There is no dedicated permission:
reading what a message used to say is the same trust as deleting it, and a
new bit would need a default, a migration of existing roles and one more switch
in the roles screen for no gain. The author and other members get `403`;
reading is not audited (it changes nothing) but is rate limited to 60 reads a
minute per member (`edit_history` in the rate-limit metrics).

- **Not exposed elsewhere.** The versions are not in the message list, the
  thread, WebSocket events, search, push or notification previews, webhook
  payloads, bot or gateway events, or any export except the author's own
  (`edits.json` in "Export my data", at most the newest 2,000 versions; a
  member never receives anybody else's earlier text). The audit log's
  `message.edit` entry (full `before` and `after`, see above) is a separate
  record with its own access and retention.
- **Bounded.** At most 50 versions are kept per message, the oldest go first.
  `EDIT_HISTORY_RETENTION_DAYS` (default `0`: keep) additionally drops versions
  replaced longer ago than that many days; the worker runs every six hours.
- **Removed with their source.** Deleting the message deletes its versions
  (cascade); the `message.delete` entry keeps them first (`edit_versions`, newest
  first, with the same text policy as the entry's own text). Deleting an account deletes the versions of that person's messages
  together with the message text; the messages stay as emptied tombstones.
  Edits made before this feature (migration 0047) have no history.

## Blocking and reports

### Blocking a member

Any member can block any other member (`PUT /api/me/blocks/{userID}`; at most
500 blocks). Blocking is **private**: the blocked member is never told, and no
response, event, list or error text they can read depends on it. For the
blocker it means:

- the web app collapses the blocked member's messages to "Blocked message ·
  show" (the client reads `GET /api/me/blocks` and the `block_updated` event);
- the blocked member's mentions and replies create no entry in the mentions
  inbox (which excludes direct messages entirely), no mention count and no push,
  whatever the channel's notification level, and a plain message of theirs sends
  no push either. It is decided when the entry is listed or the push is sent,
  so it also covers messages that were written before the block;
- in any direct message conversation (group or two-person), unread and mention
  counts and the conversation's last-activity time leave out messages by members
  the reader blocked, and push recipients skip blockers;
- in a direct message conversation of two, a block stops writing in both
  directions (the other side only sees that the member cannot be written
  to); in a group, the conversation continues, but nobody can add a member
  who blocked them;
- unblocking brings the earlier inbox entries back, because nothing was
  deleted.

A block is not a sanction: it changes nothing for anybody else and is not
audited. Blocking the Owner or a moderator only mutes them for the blocker.
Deleting an account removes the blocks to and from it; the export lists the
member's own blocks by id (`blocks.json`).

### Direct messages

Moderators have no way into a direct message conversation. The Owner and
Administrators are treated like any other non-member: they get the same answer
as for a conversation that does not exist. The only route to the text of a
direct message is a report by one of its members (below). Moderation actions
that target a member (timeout, ban, kick) work as usual, and a timeout also
takes *Send direct messages*. The Owner and Administrators can switch direct messages off for the
community under **Admin > Sign-in & messages**: existing conversations stay
readable, and nothing new can be started or written.

### Reporting a message

Any member can report a message they can see (`POST
/api/messages/{messageID}/report`, with a category of `spam`, `harassment`,
`illegal` or `other` and an optional note of up to 500 characters). A message
the member cannot see answers 404, like a missing one; their own message is
refused. At most 10 reports an hour and 50 open reports per member, and
repeating an open report returns the first one.

The report stores **its own copy** of the message and up to 20 messages around
it (15 before, 5 after) with author names, text and the names, types and sizes
of attachments (never the files). The copy is the point: the author deleting or
editing the message afterwards does not change what was reported. It is also
the *only* way a moderator ever reads a direct message: moderators and
Administrators have no path into a direct message conversation, and a report
shows only what the reporter chose to report and its context.

Members with *Manage reports* (the Owner and Administrators always) work the
queue under **Admin > Reports** (the tab shows only where the server lists the
`reports` feature; `GET /api/admin/reports`, status filter,
newest first; `GET /api/admin/reports/{id}` for the copy). They close a
report as `resolved` or `dismissed` with an optional note, and can delete a
closed one, which removes the copy. The reporter is not notified of the
outcome. Reading a report needs only this permission, not access to the
channel: a report of a private channel is readable by its handlers.

- **Audited without text.** `report.create`, `report.resolve` and
  `report.delete` are in the audit log for the Owner and Administrators only,
  with the category and ids but never message text or the note.
- **Erasure.** When an account is erased, its text is blanked in every report
  copy (as the reported message or as context) and its links to reports are
  cut; the reports stay.

## Automod

Automod checks every new and edited message, and every sign-up, against rules
the Owner or an Administrator sets under **Admin > Automod** (the tab shows
only where the server lists the `automod` feature). Code:
[`internal/automod`](../internal/automod/) (the rules), [`internal/moderation`](../internal/moderation/)
(the REST routes).

### Rules

| Rule | What it does |
|---|---|
| **Blocked words** | Words and phrases, matched as **whole words**, without regard to case. `*` in an entry stands for any run of letters and digits (`cheat*` catches *cheat* and *cheating*). Spaces inside a phrase match any white space; invisible characters (zero-width spaces, soft hyphens) are ignored, so they do not hide a word. |
| **Links** | One mode: block **every** link, allow **only** the listed domains, or block the **listed** domains. A domain covers its subdomains. A link is anything starting with `http://`, `https://` or `www.`; the host is read after any user name and port. |
| **Mention spam** | A message with **more than N different** mentions (`@name`, `@all` and `@here` count once each; the same name twice counts once). |
| **New accounts** | Accounts younger than a time (1 minute to 30 days) may not post links, may not attach files, may not do either, or may not post at all. |
| **Raid protection** | More than N accounts joining within M seconds. Every account of that window, and everyone who joins while the raid lasts, is **timed out** for the restriction time (1 minute to a day), and **one** entry in the reports queue announces the raid. A moderator lifts a timeout like any other. At most one such rule. |

Each rule except raid protection has an **action**: *block* (the message is
refused with a 400 that names the rule, so the author knows why; nothing is
stored) or *flag* (the message is delivered and only marked in the queue). It
may also **time the author out** for a set time. A longer timeout the member
already has is never shortened. Each rule has **exempt roles** and **exempt
channels**, which skip it. The Owner, holders of *Administrator*, bots and
incoming webhooks are never checked.

A rule is edited as a whole (`PUT`); its kind cannot change. At most 25 rules.
The rules are compiled once per version of the configuration and kept in
memory; a change through the API takes effect on the next message, and a change
made behind the server's back (a restore) within 30 seconds. The check in the
send path reads no table except the creation time of the author's account,
and that is remembered.

Automod covers every way a member's message is created: plain messages,
uploads, thread replies, polls, forwards, slash command replies and
**edits** (an edit that breaks a blocking rule is refused and the message stays
as it was).

### Direct messages

In a direct message only the **link** and **mention spam** rules apply. A hit
there is queued **without any text**: the entry says which member broke which
rule in a direct message, nothing more. Automod never keeps the text of a
direct message (a blocked one is dropped, a flagged one stays where it was
written, and the recipients can still report it themselves, which is then the
only way a moderator reads it).

### The queue

Every hit is an entry in **Admin > Reports**, marked **Automod**, with the rule's
name as it was when it hit (the entry outlives the rule) and `source: "automod"`
in the API. It has no reporter. For a blocked message in a channel the entry
keeps the refused text (and nothing around it: no context), exactly the part a
member's report would keep; a flagged message is referenced by id. While an
entry of one member and one rule is open, further hits only count up on it
(`hit_count`), so a spammer cannot flood the queue; closing it lets the next
hit open a new one. A raid alert has `kind: "alert"` and no message.
Closing, dismissing and deleting work as for any report (*Manage reports*).

Audit entries: `automod.rule_create`, `automod.rule_update`,
`automod.rule_delete` (by the Administrator), `automod.timeout` and
`automod.raid` (actor *Automod*). Messages and hits are not audited; the
queue is their record.

### Defaults

New servers and upgraded ones start with the same **conservative** rules
(migration 0066), all editable and deletable: mention spam above 10 mentions
(block), new accounts under 10 minutes may not post links (block), raid
protection at more than 10 joins in 10 seconds (restriction of 10 minutes,
flag). *Blocked words* (empty word list) and *Links* are present and **off**.
See [the upgrade notes](upgrade.md#automod-and-moderation-history).

### API

All under `/api/admin`, **Administrator** only: `GET /automod`,
`POST /automod/rules`, `PUT /automod/rules/{id}`,
`DELETE /automod/rules/{id}`. See [`api/openapi.json`](../api/openapi.json).

## Moderation history and moderator notes

A member's profile has a **Moderation** tab for everyone with *Manage reports*
(never for yourself, for bots, or on a server without the
`moderation.history` feature). It shows

- a **timeline**: timeouts, ended timeouts, bans, unbans, expired bans, kicks,
  removals from a Talk, role changes made by moderators, reports about the
  member (category and status, never the reporter, the note or the text), and
  automod hits (rule, outcome, count). It is read from the audit log and the
  reports table; the latest 200 events;
- **notes**: short private texts (up to 1000 characters, at most 100 per member)
  that moderators write about a member. A note can **count as a strike**; the
  strike count is the small badge on the tab.

A moderator **edits and deletes their own** notes; an Administrator (or the
Owner) **deletes any** note, and edits only their own. Nobody opens the history
or the notes of themselves, whatever they hold.

The timeline needs *Manage reports* only, not *View audit log*, even though it shows
reasons, role changes and Talk kicks: handling a report needs the history. Grant
*Manage reports* with that in mind.

`GET /api/admin/users/{id}/moderation`, `POST .../moderation/notes`,
`PUT` and `DELETE .../moderation/notes/{noteID}`, all *Manage reports*.

### Moderator notes are moderator data

- **Never visible to the member** they are about, in no response, event, list or
  search, and **never in their data export** (the export holds what the
  person wrote and what they filed, not what moderators keep about them).
- **Audited without text**: `moderation.note_add`, `moderation.note_edit` and
  `moderation.note_delete` are in the audit log for the Owner and
  Administrators only, and never carry the note.
- **Erasure.** When an account is erased the notes **stay**, because they are
  the moderators' record (for example against a returning member): the member
  they are about is shown as the account's tombstone, "Deleted member", and a
  note written by an erased moderator shows "Deleted member" as its author.
  The notes are not erased with the account, and the privacy page lists them
  under *Moderation records*; an Administrator can delete any of them.
  Operators who need a shorter retention delete notes by hand.

## Audit log

An append-only record of what was done to the instance and by whom.

### What an entry holds

| Field | Meaning |
|---|---|
| `id`, `created_at` | Order and time |
| `actor_id`, `actor_name` | The member who acted (the name as it was then); empty for the server itself |
| `action` | `group.verb`, for example `role.update`, `user.disable`, `invite.create` |
| `target_type`, `target_id`, `target_name` | What the action was done to |
| `details` | Sanitized: the request, a `before` snapshot of the audited fields, and `changes` (`from`/`to`) |
| `request_id` | The `X-Request-Id` of the request, to find the matching server log lines |

### What is recorded

Every successful mutating request below `/api/admin` is recorded by one
middleware that works from a route table (`internal/audit/routes.go`), so a
handler cannot forget it: roles (create, edit, delete, reorder, assignment),
channel and category overwrites, channels and categories (create, edit,
delete, duplicate, layout), invites (created, changed, revoked), members (disable,
enable, end sessions, status line, password set or reset, login unlock, Talk
kick), incoming and outgoing webhooks (create, edit, regenerate or rotate,
delete), media pruning and deletion, and self-update. A test walks the router
and fails when an admin route is missing from the table; an unlisted route is
still recorded under a generic `admin.<method>` action. Refused or failed
requests leave no entry. Reading the log is not recorded.

### Events besides admin actions

| Action | When | What it holds |
|---|---|---|
| `message.delete` | A message is deleted, by its author or a moderator | The **full text**, the attachment names, types and sizes (not the files), author, channel, send time, `moderator` (true when someone else deleted it) and the thread replies (author and text, up to 50) |
| `message.edit` | An author edits a message | Text `before` and `after`, author, channel |
| `message.edit_history_view` | A moderator opens the earlier versions of **someone else's** message | Message, author, channel and how many versions were shown, not their text. One entry per reader and message every ten minutes; reading your own message leaves none |
| `poll.end` | A moderator ends a poll that someone else posted | Message, author, channel and the question. An author ending their own poll is not audited. No entry of any poll ever holds a vote: an anonymous poll does not store who chose what |
| `channel.auto_delete_set` | Someone with *Manage channels* turns a channel's auto-delete on, off or to another period | The channel with its settings before and after (`auto_delete_days`) |
| `channel.auto_delete_run` | The sweeper removed messages of a channel | The channel, the period (`days`), how many `messages` and `attachments` went, and whether the run finished (`complete`). Written by the server itself, without an actor; a run that removes nothing writes nothing. Never the text |
| `voice.join`, `voice.leave` | A member enters or leaves a Talk | Member and Talk with the time. A reload within the grace period is neither. High volume on a busy server: filter by the *Talk presence* group |
| `report.create`, `report.resolve`, `report.delete` | A member reports a message; a moderator closes or deletes the report | The report id and category (and the closing status). Never message text or notes. See [Blocking and reports](#blocking-and-reports) |
| `auth.login` | Successful sign-in | The member |
| `auth.login_failed`, `auth.login_locked` | A wrong password, or a blocked attempt, for an **existing** account | The member. At most one entry per account and kind per minute, and no address, password or typed name; a name that is no account is not stored at all |
| `auth.register`, `auth.password_change`, `auth.logout_all` | Own-account events | The member |
| `auth.session_revoke`, `auth.sessions_revoke_others` | A member signs out one of their sessions, or all others, under *Settings* > *Sessions* | The member |
| `auth.email_set`, `auth.email_remove`, `auth.email_verified`, `auth.password_reset_mail` | A member sets, removes or confirms an e-mail address, or resets the password through the link in a mail | The member. The address itself is never stored in the log |
| `auth.two_factor_enabled`, `auth.two_factor_disabled`, `auth.recovery_codes_regenerated`, `auth.recovery_code_used` | A member changes or uses their second factor | The member. Never a code or secret |
| `auth.two_factor_failed`, `auth.two_factor_alert` | A wrong second-step code (at most one entry per account and minute); five or more not yet reported | The member. No code |
| `user.two_factor_reset` | The Owner (after confirming their own password and code, or via the host command `mnema-talk reset-2fa`) turns a member's second factor off | Who did it and the member |
| `auth.account_delete`, `auth.export_request`, `auth.export_download` | A member deletes their account or asks for / takes their data export | The account id only. After a deletion, earlier entries about the account lose names and details |
| `system.mail_test` | The Owner sends a test mail | Who did it; neither recipient nor mail text |

Text channels have no membership (who may see one follows from roles and
overwrites, which are audited), so there is no join entry for them.

### What is never stored

Passwords, tokens, secrets, hashes, invite codes, cookies, authorization
values and session ids are redacted by key name wherever they appear. A web
address anywhere in a value is reduced to its scheme and host plus a short
hash of the whole address (`https://hooks.example.com [sha256:1a2b3c4d]`),
because webhook URLs carry tokens in the path as well as the query; the hash
still shows that an address changed. Snapshots list columns one by one, so a secret
column added to a table later does not leak by accident. Strings are clipped to
200 characters and an entry to 32 KiB.

> **Privacy.** Message deletions and edits record the message text, and Talk
> joins and leaves record who was where and when. That is the point of an audit
> log, and it means the log holds personal data. Name it in your privacy
> notice, and set `AUDIT_RETENTION_DAYS` if you want old entries to expire.

### Immutability and retention

Entries are inserted once. A database trigger refuses `UPDATE`, and the API
has no write route. The only deletion is the retention job:
`AUDIT_RETENTION_DAYS=0` (default) keeps everything forever, like
`MEDIA_RETENTION_DAYS=0`; a positive number deletes entries older than that
many days (checked every six hours). Entries have no foreign key to users, so
they outlive the accounts they mention.

### The diff cannot be evaded

The request body is captured whatever its `Content-Type` says, and a body that
is not a JSON object or is too large is marked (`request_unparsed`,
`request_truncated`) instead of dropped. The `changes` of an edit do not come
from the request at all: the audited fields are read before and after the
change and compared, so they show what really happened. If an entry grows
beyond its size limit, the request is trimmed first (`request_truncated`), then
`after`, `before` and `params`; `changes` is kept.

### Access

Only the Owner and holders of the **View audit log** permission may read it
(`GET /api/admin/audit-log`; the permission also opens the admin console on the
*Audit log* tab). *Administrator* includes it. What a reader sees depends on the class of the entry. The table lives in code
(`audit.Classes`, pinned action by action in a test), so a new action cannot
quietly default to something wider than intended; this is the same table.

| Class | Who reads it | Actions |
|---|---|---|
| Auditors | Every holder of *View audit log* | `role.*`, `user.*` except the Owner-only ones below, `invite.*` (and the moderation actions of later releases) |
| Channel | Holders of *View audit log* **who can view the entry's channel when they read** | `message.*`, `voice.*`, `channel.*` (a private channel's name, topic and overwrites are private too) |
| Owner and Administrators only | The Owner and members with *Administrator* | `category.*` and `layout.apply` (a private category's name and overwrites are private too, and a layout lists every placement), `auth.*` (sign-ins, failures, lockouts, password changes of other members), `webhook.*`, `outgoing_webhook.*`, `bot.*`, `media.*`, `settings.*`, `system.*`, `user.password_set`, `user.password_reset`, `user.lockout_clear`, `report.*` (a report may concern a private channel or a direct message), `admin.*` (routes not yet named in the route table) |

The check runs on every read against the current permissions, so access that is
granted later also opens the older entries of that channel, and access that is
taken away closes them. The Owner and Administrators see everything. Deleted
thread replies are recorded by id only; a reply deleted on its own has its own
entry with its text. The audit log is not sent to outgoing webhooks.

The `admin.%` row in `audit.Classes` is mapped to Owner and Administrators on
purpose (`VisPrivileged`): unlisted admin routes are not known to be harmless.
The parenthetical on `admin.*` in the table above is that catch-all.
`TestClassesPinTheVisibilityOfEveryAction` pins `admin.post` to that class, and
`TestAuditCoversEveryMutatingAdminRoute` checks the named mutating `/api/admin`
routes. The catch-all stays.

### Who may send, talk and moderate

Each cell is the function that enforces the action for that actor, and a test
that already exercises it. `gap` means that pair is absent. The role bits are
the [Defaults](roles-and-permissions.md#defaults) section: Member is
`@everyone` with `perm.DefaultEveryone`, Moderator and Admin are the seeded
roles, and the Owner is the account `users.role = 'admin'`. There is no User
role and no Owner role. Neither seeded role holds the *Administrator* bit.
`TestStarterRolesAreSeededEmpty` checks those bits.

`TestTimeoutTakesPostingAndSpeakingButNotReading` builds its own "admin" with
the *Administrator* bit. That user is separate from the seeded Admin role.
`Snapshot.silence` still strips `perm.TimeoutMask` from every non-owner while
a timeout is in the future, and `Snapshot.Channel` applies that after
overwrites, so an overwrite cannot hand the mask back. `Snapshot.Channel`
returns every permission for the Owner before that step.

Slow mode runs in `InsertMessage` unless the message has a `WebhookID` or the
context is exempt. `sendContext` sets the exemption when the caller has
*Manage messages*. `botCreateMessage` calls `InsertMessage` without that
exemption, so a bot token is subject to slow mode. `build` sets `WebhookID`
for an incoming webhook, so `InsertMessage` skips slow mode for it.
`TestBotAPIMessages` and `TestWebhookStandsOnItsOwn` reach `InsertMessage`;
neither turns slow mode on. `TestSlowModeHoldsBackMembersButNotModerators`
covers a member, a holder of *Manage messages*, and the Owner.

A ban or a disable stops an account through `auth.ActiveSQL` (`disabled_at`,
or a `banned_at` that is still in force). `sessionUser` loads a member session
with that condition. `TestActiveConditionsQualifyTheTable` pins both columns.

Posting, reacting and uploading do not write an audit entry. The audit cell is
the class of the entries that path does record.

| Actor | Send message | Upload | React | Edit or delete own message | Join a Talk | Slow mode | Timeout mask | Ban or disable | Per-route rate limit | Audit class |
|---|---|---|---|---|---|---|---|---|---|---|
| Member (`@everyone`) | `sendContext` (`TestChannelPermissionsGateTheirActions`); [Defaults](roles-and-permissions.md#defaults) | `UploadContext` (`TestChannelPermissionsGateTheirActions`); [Defaults](roles-and-permissions.md#defaults) | `toggleReaction` (`TestChannelPermissionsGateTheirActions`); [Defaults](roles-and-permissions.md#defaults) | `EditMessage`, `DeleteMessageAs` (`TestChannelPermissionsGateTheirActions`); [Defaults](roles-and-permissions.md#defaults) | `joinVoice` (`TestJoinNeedsViewAndConnect`); [Defaults](roles-and-permissions.md#defaults) | `InsertMessage` (`TestSlowModeHoldsBackMembersButNotModerators`); [Defaults](roles-and-permissions.md#defaults) | `Snapshot.silence` (`TestTimeoutTakesPostingAndSpeakingButNotReading`) | `sessionUser` (`TestBanBlocksSessionsLoginAndSocketsAndUnbanRestores`, `TestAdminDisablesAndEnablesUsers`) | `n/a` (not a per-route limit) | `ClassOf` (`TestClassesPinTheVisibilityOfEveryAction`) |
| Moderator (seeded set) | `sendContext` (`TestChannelPermissionsGateTheirActions`); [Defaults](roles-and-permissions.md#defaults) | `UploadContext` (`TestChannelPermissionsGateTheirActions`); [Defaults](roles-and-permissions.md#defaults) | `toggleReaction` (`TestChannelPermissionsGateTheirActions`); [Defaults](roles-and-permissions.md#defaults) | `EditMessage`, `DeleteMessageAs` (`TestChannelPermissionsGateTheirActions`); [Defaults](roles-and-permissions.md#defaults) | `joinVoice` (`TestJoinNeedsViewAndConnect`); [Defaults](roles-and-permissions.md#defaults) | `sendContext` (`TestSlowModeHoldsBackMembersButNotModerators`); [Defaults](roles-and-permissions.md#defaults) | `Snapshot.silence` (`TestTimeoutTakesPostingAndSpeakingButNotReading`) | `sessionUser` (`TestBanBlocksSessionsLoginAndSocketsAndUnbanRestores`, `TestAdminDisablesAndEnablesUsers`) | `n/a` (not a per-route limit) | `ClassOf` (`TestClassesPinTheVisibilityOfEveryAction`) |
| Admin (seeded set) | `sendContext` (`TestChannelPermissionsGateTheirActions`); [Defaults](roles-and-permissions.md#defaults) | `UploadContext` (`TestChannelPermissionsGateTheirActions`); [Defaults](roles-and-permissions.md#defaults) | `toggleReaction` (`TestChannelPermissionsGateTheirActions`); [Defaults](roles-and-permissions.md#defaults) | `EditMessage`, `DeleteMessageAs` (`TestChannelPermissionsGateTheirActions`); [Defaults](roles-and-permissions.md#defaults) | `joinVoice` (`TestJoinNeedsViewAndConnect`); [Defaults](roles-and-permissions.md#defaults) | `sendContext` (`TestSlowModeHoldsBackMembersButNotModerators`); [Defaults](roles-and-permissions.md#defaults) | `Snapshot.silence` (`TestTimeoutTakesPostingAndSpeakingButNotReading`) | `sessionUser` (`TestBanBlocksSessionsLoginAndSocketsAndUnbanRestores`, `TestAdminDisablesAndEnablesUsers`) | `n/a` (not a per-route limit) | `ClassOf` (`TestClassesPinTheVisibilityOfEveryAction`) |
| Owner (the account) | `sendContext` (`TestTheOwnerCannotBeTouched`); [Defaults](roles-and-permissions.md#defaults) | `UploadContext` (`TestPrivateChannelIsInvisibleEverywhereToOutsiders`); [Defaults](roles-and-permissions.md#defaults) | `toggleReaction` (`TestGatewayDeliversOnlyWhatTheBotMaySee`); [Defaults](roles-and-permissions.md#defaults) | `EditMessage` (`TestGatewayDeliversOnlyWhatTheBotMaySee`); `DeleteMessageAs` (`TestModeratorsCannotDeleteTheOwnersMessages`) | `joinVoice` (`TestJoinNeedsViewAndConnect`) | `sendContext` (`TestSlowModeHoldsBackMembersButNotModerators`) | `Snapshot.Channel` (`TestTimeoutTakesPostingAndSpeakingButNotReading`) | `checkTarget` (`TestModerationRespectsHierarchyAndPermissions`); `targetUser` (`TestTheOwnerCannotBeTouched`) | `n/a` (not a per-route limit) | `ClassOf` (`TestClassesPinTheVisibilityOfEveryAction`) |
| Bot token | `botCreateMessage` (`TestBotAPIMessages`) | `botUpload` (`TestBotAPIUpload`) | `setBotReaction` (`TestBotAPIReactions`) | `botEditMessage`, `botDeleteMessage` (`TestBotAPIMessages`) | `n/a` (bots have no voice session) | `InsertMessage` (`TestBotAPIMessages`) | `Snapshot.silence` (`TestTimedOutBotCannotSpeak`) | `Lookup` (`TestBannedBotIsRefusedAndDisconnected`) | `LimitSends` (`TestBotAPIRateLimits`) | `ClassOf` (`TestClassesPinTheVisibilityOfEveryAction`) |
| Incoming webhook | `post` (`TestWebhookStandsOnItsOwn`) | `checkAllowed` (`TestExecuteAppliesTheWebhooksSettings`) | `n/a` (posts through InsertMessage only) | `n/a` (posts through InsertMessage only) | `n/a` (posts through InsertMessage only) | `InsertMessage` (`TestWebhookStandsOnItsOwn`) | `n/a` (posts through InsertMessage only) | `n/a` (posts through InsertMessage only) | `execute` (`TestWebhookRateLimits`) | `n/a` (posts are not audited) |
| Bot gateway | `n/a` (delivers events only) | `n/a` (delivers events only) | `n/a` (delivers events only) | `n/a` (delivers events only) | `n/a` (delivers events only) | `n/a` (delivers events only) | `n/a` (delivers events only) | `validTokens` (`TestBannedBotIsRefusedAndDisconnected`) | `frameLimiter.allow` (`TestGatewayFrameRateLimit`) | `n/a` (events are not audited) |

`n/a` means the actor does not do that action, or the limit is not per route.
Chat sends have no per-route limiter. The limiters that exist are `loginPerIP`
(login), `LimitSends` (bot token), `execute` (incoming webhook) and
`frameLimiter.allow` (bot gateway). Signed-in member routes share `perUser.By`
(`api_user`, 600 a minute) for writes and `api_user_read` (3000 a minute) for reads, one bucket of each per user, not per route. A bot token's edits and
deletes are `message.edit` and `message.delete`, already classed with the
member rows. An incoming webhook only posts, and the bot gateway only delivers events. Neither is audited, same as a member's post, so those audit cells are `n/a`.

### API

`GET /api/admin/audit-log` takes the filters `actor` (user id), `action` (exact
or a group: `role` matches every `role.*`), `target_type`, `target_id`, `from`
and `to` (RFC 3339 or `YYYY-MM-DD`), `before` (an entry id, for the next page)
and `limit` (1 to 200, default 50). It returns `{entries, next_before}`, newest
first. See [`api/openapi.json`](../api/openapi.json).
