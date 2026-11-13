# Invites

Sign-up is by invite only. Invites come from two places: the admin area, and
members themselves when they are allowed to.

## Admin invites

*Manage invites* (`create_invites`, part of the admin area) lists every invite,
creates them with a custom code or none, an expiry and a use limit, edits and
revokes any of them. The list names who created each invite. Routes:
`GET/POST /api/admin/invites`, `PATCH/DELETE /api/admin/invites/{id}`. Revoking
an invite leaves the accounts that signed up with it alone.

## Member invites

*Create invites* (`create_member_invites`, bit `1 << 30`) lets a member bring
people in without the admin area. It is a separate permission from *Manage
invites*: it never lists, edits or revokes someone else's invite.

- **Who holds it.** On a fresh install @everyone holds it. An **existing**
  install keeps what its members could do: migration 0108 gives the permission
  only to the roles that already hold *Manage invites*, and not to @everyone.
  The Owner turns it on for @everyone or for a role in *Roles*, and can allow
  or deny it per category and channel (an invite that lands in a channel needs
  it there). A timeout, pending rules and the new-member wait of
  [raid protection](moderation.md) take it away like they take sending
  messages. A self-assignable role cannot hold it.
- **What a member can do.** Create invites (`POST /api/me/invites`), see their
  own (`GET /api/me/invites`) and revoke their own
  (`DELETE /api/me/invites/{id}`). Another member's invite answers `404`.
  Seeing and revoking one's own needs no permission, so a member whose right
  was withdrawn can still take back what they made. Invites that exist stay
  valid until they expire or are revoked.
- **Safe defaults and limits.** A member's invite always expires and always has
  a use limit; omitted fields default to 24 hours and 10 uses, cut down to the
  limits below. Custom codes are an admin feature. Under **Admin, Invites,
  Member invites** the holders of *Manage invites* (the Owner and Administrators among them) set the longest expiry (default
  7 days), the most uses of one invite (25), how many invites one member may
  have active at once (10) and how many one member may create per hour (10;
  revoking an invite does not reset that count). Holders of *Manage invites* are
  not held to the last two, but the bounds on one invite stay.
- **Raid protection still applies.** A lockdown that also pauses invites
  refuses member invites (`409`), as it does admin invites.
- **A creator who is disabled or banned.** Their invites stop working at once
  (sign-up and the preview answer as for an unknown code); they work again if
  the account is enabled. Deleting the account deletes its invites.
- **Audit log.** Member invites write `invite.member_create` and
  `invite.member_revoke` with the member as the actor (never the code); changing
  the limits writes `invite.limits`.

## Invite people into a channel

Right-click a channel (or use its header menu, or long-press on a phone) and
choose *Invite people*. The dialog makes a link from `POST /api/me/invites`
with `channel_id`, to copy, with an expiry to choose from the allowed range.
The member must see the channel and hold *Create invites* in it, otherwise the
server answers `404` or `403`; a conversation is never a target.

Whoever signs up with that link lands in the channel if every new member can see
it through @everyone. For any other channel (a private one, for example) the
invite still works, but the joiner lands in the default channel:
`GET /api/invites/{code}` only names a channel that a plain new member can see.
If the channel is deleted the invite keeps working and lands in the default
channel.

Links are built from the server's public address (`PUBLIC_URL`, announced as
`public_url` by `GET /api/health`), so a link copied in the desktop app opens
for the person you send it to.
