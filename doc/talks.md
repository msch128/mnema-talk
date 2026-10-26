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
2. It creates a **temporary Talk** in the lobby's category, named "Talk 1",
   "Talk 2", ... (the lowest number no temporary Talk of that category uses),
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
"Start a Talk" (`POST /api/dms/{id}/talk`) opens a temporary Talk from the first
lobby the member can see and join (in sidebar order) and posts an **invitation
card** into the conversation. If no such lobby exists the request answers 409 and
the web app explains that the community has no Talk lobby. While in a Talk, "Invite
to my Talk" (`POST /api/talks/{id}/invites` with the conversation) posts the same
card.

The card is an ordinary message whose embed carries `talk_id`. Only the server
sets it (webhook payloads cannot). Its **Join** button runs the normal join, so an
invitation never lets anyone into a Talk they could not join: a member who cannot
see the Talk sees it as unavailable.

## Access presets

The Talk settings (Talk context menu, or the settings button in the Talk header)
offer three presets, all plain writes of the per-channel permission overwrites and
the member limit, no new permission model:

- **Everyone** clears the denial of *View channels* and *Connect* for @everyone.
- **Only these roles** denies both to @everyone and allows them for the chosen roles.
- **Limit** sets the member limit.

On a lobby they decide who may open Talks (its temporary Talks follow it); a temporary Talk itself offers only the limit. They need *Manage channels* (and the rank and permissions that editing overwrites
needs anyway); a Talk leader can use the limit.

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

## Limits

| What | Limit |
|---|---|
| Temporary Talks one member opens | 5 per 10 minutes |
| Temporary Talks per lobby | 50 |
| Edits and invitations per member | 30 per minute |
| Empty temporary Talk | removed after 30 seconds |
| Stage commands (hand, talk power) per member | 30 per 10 seconds |
| Live reactions per member | 5 per 10 seconds |
