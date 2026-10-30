# Talks: lobbies, temporary Talks and invitations

A **Talk** is a voice room. Since 1.2 members can open Talks of their own
without the *Manage channels* permission. This page describes the three
parts: Create-a-Talk lobbies, starting a Talk from a direct message, and the
access presets in the Talk settings. The server advertises the whole feature as
`talks.lobby` in the `features` list of `GET /api/health`; a web app hides the
new interface when the name is missing.

## Create-a-Talk lobbies

A member with *Manage channels* marks a permanent Talk as a **lobby** (Talk
settings, or `is_lobby` in `PATCH /api/admin/channels/{id}`). Nobody ever sits
in a lobby. When a member joins it:

1. The server checks that the member may view and join the lobby, like for any
   Talk, and that they have opened fewer than 5 Talks in the last 10 minutes and
   that the lobby has fewer than 50 temporary Talks open.
2. It creates a **temporary Talk** in the lobby's category, named after the
   lobby: "<lobby name> 1", "<lobby name> 2", ... (the lowest number no
   temporary Talk of that category uses under that name; a very long lobby name
   is cut so the name stays within the channel name limit). Renaming the lobby
   leaves open Talks as they are; new ones use the new name,
   with the lobby's member limit and a copy of its permission overwrites. Who
   may see and join the temporary Talk is exactly who may see and join the lobby.
   It sorts directly behind the lobby and moves no permanent channel.
3. It sends `talk_moved` (`{from_channel_id, channel_id}`) to the connection that
   joined. The client then joins the new Talk with its ordinary `voice_join`,
   so media setup and the SFU are the same as for every Talk. The member is never
   listed in the lobby.

If the lobby opened no Talk, the member gets `voice_kicked` with the reason
`talk_rate_limited` or `talk_unavailable`.

Changing a lobby's permission overwrites copies them again, in the same transaction, to every temporary Talk that is open from it, and the usual permission sweep disconnects members who lost *View channels* or *Connect*. A temporary Talk's permissions always and only mirror its lobby: `PUT /api/admin/channels/{id}/overwrites` on a temporary Talk answers 409 ("Temporary Talks follow their lobby's permissions"), for everyone. A leader can only rename the Talk and set its limit.

Temporary Talks are not part of the saved layout: a layout change ignores them
(`PUT /api/admin/layout`), and they are marked with `is_temp` in the channel
list, next to the lobby's `is_lobby`.

### Leader

The member who opened a temporary Talk is its **leader** (`leader_id` in the
channel list). The leader can rename the Talk and set or clear its member limit
(`PATCH /api/talks/{id}`) without *Manage channels*, for that Talk only. When the
leader leaves, the member who has been in the Talk longest becomes the leader.
Every change is broadcast as `talk_leader` (`{channel_id, leader_id}`; `leader_id`
is null when nobody is left). The leader is shown with a small green mark in the
Talk's member list.

### Removal

An empty temporary Talk is removed 30 seconds after its last member left (the hub
already keeps a member whose connection dropped for 30 seconds, so a reload does
not lose the Talk), or 30 seconds after it was opened when nobody entered it. The
removal is recorded in the audit log as `channel.delete` by `system`, with the
details `temporary: true` and the reason. After a restart the server removes all
temporary Talks, because nobody is in a Talk then.

## Start a Talk from a direct message

A conversation has no call of its own. In the header of a direct message
"Start a Talk" (`POST /api/dms/{id}/talk`) opens a temporary Talk **that belongs
to the conversation** and posts an **invitation card** into it. No lobby and no
admin setup are needed, so it works on a fresh community. The Talk is a temporary
Talk without a lobby (`is_temp`, `lobby_id` NULL) with `channels.dm_id` pointing
at the conversation (migration 0079): the permission snapshot lets only the
conversation's members see and join it, the Owner and Administrators included
(like the conversation itself), a member added to a group gets it too, it shows
in the sidebar of its members only, and it is deleted when it is empty (like any
temporary Talk) or when the conversation is. Nothing about it reaches the audit log (who joined it, renamed it or when it went), because that would tell the Owner who called whom. It is led by whoever started it and
counts against the 5 new Talks per 10 minutes. While in a Talk, "Invite to my
Talk" (`POST /api/talks/{id}/invites` with the conversation) posts the same card.

The card is an ordinary message whose embed carries `talk_id`. Only the server
sets it (webhook payloads cannot). Its **Join** button runs the normal join, so an
invitation never lets anyone into a Talk they could not join: a member who cannot
see the Talk sees it as unavailable.

### Ringing

Starting a Talk from a conversation, or sharing a Talk into one ("Invite to my
Talk"), also *rings* the conversation (`internal/chat/dm_calls.go`). Nothing
about the room changes: it is still a Talk, and the card in the conversation
still works. The ring is what makes the invitation visible where the member is:

- **Events.** `dm_call_ring` goes to the conversation's members' sessions that
  asked for conversations (`?dm=1`), the caller's own included. It carries the
  Talk, the invitation message, the caller and how long it rings
  (`ring_seconds`, 45 by default). `dm_call_end` says it stopped ringing:
  `answered` (another member entered the Talk; `user_id` says who, so their
  other devices and the other members drop the card), `cancelled` (the caller
  left before anybody joined, or started another Talk in the conversation),
  `timeout` (nobody joined in time) or `ended` (the last member left). `active`
  is true while somebody is still in the Talk. Both are additive: a client that
  ignores them still gets the card as an ordinary message.
- **Missed call.** After `cancelled` and `timeout` the server writes a "missed
  call" message in the conversation (an embed with `call: "missed"`, written by
  the caller, set by the server only) and announces it with `message_create`
  before `dm_call_end` (`message_id`). It is an ordinary unread message, so the
  conversation gets its badge and the preview reads "Missed call". It does not
  count towards the cap on a starter's unanswered messages. A call that rang out
  while the caller waits in the Talk leaves one record, and a member who joins
  later writes nothing more.
- **After a reload.** `GET /api/dms` and `GET /api/dms/{id}` carry `call` (the
  Talk, the caller, `ringing` and the seconds it still rings) for a conversation
  with a call going on, and `last_message.call` names the card and the record
  of a call, so a window that opens during a call shows it. The calls are kept in
  memory: a restart ends them, together with their Talks.
- **The web app** shows an incoming call card (caller, "is calling you", Join,
  the conversation and Dismiss) over whatever is open, a phone on the
  conversation in the sidebar while a call rings or goes on, and to the caller
  "Calling …" with Cancel, then "… hasn't joined". Joining on one device ends the
  ring on the member's others. A muted conversation, a blocked caller and
  do not disturb show no card or sound (do not disturb still shows the card
  quietly), and the ringtone is the sound effect "Incoming call ringtone" in the
  audio settings (volume, output device and the switch for it apply). A
  notification of the operating system says "{name} is calling you" with
  "Direct message · Click to join", and Web Push says the same; using either
  opens the conversation at the card.

## Access presets

The Talk settings (Talk context menu, or the settings button in the Talk header)
offer three presets, all plain writes of the per-channel permission overwrites and
the member limit, no new permission model:

- **Everyone** clears the denial of *View channels* and *Connect* for @everyone.
- **Only these roles** denies both to @everyone and allows them for the chosen roles.
- **Limit** sets the member limit.

On a lobby they decide who may open Talks (its temporary Talks follow it); a temporary Talk itself offers only the limit. They need *Manage channels* (and the rank and permissions that editing overwrites
needs anyway); a Talk leader can use the limit.

### Member limit

A Talk with a limit (1-999; 0 is unlimited) shows `N/limit` in the sidebar, live from the voice state events, in a warm colour once it is full. A join that would exceed the limit is refused with `voice_kicked` and `reason: "room_full"`, and the sidebar says so before trying. The check and the seat are taken under one lock, so two joins for the last seat cannot both get in. A member who is already in the Talk (a second tab or device) is never refused, by the server or by the sidebar. Members with *Disconnect members* in that Talk (Administrators and the Owner hold every permission) may exceed the limit, so `3/2` is possible: the permission to remove people from a Talk already marks its moderators, and *Manage channels* is not needed because its holders could raise the limit anyway. A lobby holds nobody and shows no counter.

## Stages

A Talk can be a **Stage**: a room with a speakers' row and an audience. Without
*talk power* nobody sends audio, camera or screen there. A member with
*Manage channels* turns a permanent Talk into a Stage (and back) with "Make Stage" /
"Make normal Talk" in the Talk's context menu, or creates a Talk as Stage
(`is_stage` in `PATCH /api/admin/channels/{id}` and `POST /api/admin/channels`).
Lobbies and temporary Talks cannot be Stages. Making a Talk a Stage silences its
audience at once; making it a normal Talk again lifts every limit and forgets all
talk power and hands. The advertised feature name is `talks.stage`.

**Talk power** belongs to one stay in the Stage. Holders of the permission
*Manage stage* (bit `1 << 24`, off for @everyone, always held by the Owner and
Administrators, allowed per Stage through a channel overwrite) have it
implicitly and can give it to and take it from any member in the Stage. It is
cleared when the member leaves, and the member still needs *Speak* (and *Video*
for camera and screen), as in any Talk.

The server decides, not the interface: the media peer of a member without talk
power is told which sources it may not publish (audio, the sound of a screen
share, camera, screen), the same mechanism *Speak* and *Video* use, and anything
already published is unpublished. A client that ignores the interface sends
nothing anybody hears.

**Raising a hand.** A member without talk power who may speak raises their hand
(`stage_hand` with `raised: true`). Holders of *Manage stage* see the queue,
oldest first, and accept with one click (that gives talk power and lowers the
hand) or dismiss. Everybody in the Talk receives the events and `talk_power` /
`hand_raised_at` on the members of `voice_snapshot` and `voice_state_update`.
The hand goes down by itself when talk power is given, and when the member leaves.

| Client event | Payload | Needs |
|---|---|---|
| `stage_hand` | `{raised}` for the member's own hand; `{user_id, raised: false}` dismisses another's | being in the Stage and *Speak*; *Manage stage* to dismiss |
| `stage_talk_power` | `{user_id, granted}` | *Manage stage*; the target is in the Stage |

| Server event | Payload |
|---|---|
| `stage_hand` | `{channel_id, user_id, raised, at?}` |
| `stage_talk_power` | `{channel_id, user_id, granted}`; `granted` is the effective state, so a manager shows as a speaker |

Both go to the members of the Talk only. Commands are limited to 30 per 10 seconds
per member. Anything the server refuses (a stranger, a missing permission, a normal
Talk) is dropped without an answer.

## Live reactions

Any member in a Talk, Stage or not, can send a short-lived reaction: an emoji,
including a custom emoji of the community, which floats briefly over their tile
(`talk_reaction` with `{emoji}`; the feature name is `talks.reactions`). The server
sends `{channel_id, user_id, emoji}` to the members of that Talk and stores
nothing: a reaction is in no history and no export, no webhook or bot sees it. A
reaction needs *Add reactions* in the Talk (a timeout takes it), at most 5 per 10
seconds per member are relayed over all their connections, and an invalid or
unknown emoji (a custom emoji that does not exist) is dropped. With reduced motion
the web app shows a still badge instead of a floating emoji.

## Taking a call over from another device

The browser and the desktop client are both sessions of the same web app, and one account may be signed in on several at once. Joining a Talk while the account is already in a Talk on another session (the same Talk or a different one) asks first: "You are in <Talk> on another device. Take over here?" Confirming moves the call; cancelling changes nothing.

- The confirmed join is sent with `takeover: true`. The server seats the new session first; only when its media connection is up does it end the voice of the account's other sessions, each told `voice_kicked` with `reason: "voice_taken_over"`. If the new join fails, the old call carries on.
- Only the voice ends. The old session stays connected and keeps its text and chat. It shows "Your call moved to another device" with a "Back here" button, which takes the call back without asking again.
- Of two takeovers at the same moment the later seat wins; exactly one session keeps the call.
- Mute and deafen are not carried over: the new session starts from its own saved settings, and device choices stay per device.
- Without `takeover` nothing changes: a second session joining the same Talk replaces the first (`voice_kicked` with `joined_elsewhere`), and one joining a different Talk is seated there as well. Rejoins nobody asked for (after a reload) never ask. A session that reconnects its WebSocket resumes its call with `resume: true`; if another device took the call over meanwhile, the server refuses with `voice_taken_over` instead of taking it back. A resume that only replaces the account's own stale socket (a network flap) always succeeds.
- A device that vanished without leaving (closed lid, lost network) stays listed for the 30 s voice grace period, so a join in that time still asks once; confirming simply ends the leftover.

## Limits

| What | Limit |
|---|---|
| Temporary Talks one member opens | 5 per 10 minutes |
| Temporary Talks per lobby | 50 |
| Edits and invitations per member | 30 per minute |
| Empty temporary Talk | removed after 30 seconds |
| Stage commands (hand, talk power) per member | 30 per 10 seconds |
| Live reactions per member | 5 per 10 seconds |
