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

## Limits

| What | Limit |
|---|---|
| Temporary Talks one member opens | 5 per 10 minutes |
| Temporary Talks per lobby | 50 |
| Edits and invitations per member | 30 per minute |
| Empty temporary Talk | removed after 30 seconds |
