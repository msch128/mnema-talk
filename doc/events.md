# Community events

Community events let organizers schedule voice hangouts or server-wide activities, gather RSVPs, and remind members before start. The server advertises the feature as `events` in the `features` list of `GET /api/health`; web clients hide the events interface when the flag is absent.

## Overview

- **Sidebar entry**: An **Events** link appears at the top of the channel sidebar, showing a count badge of upcoming events. Clicking it opens the Events list dialog.
- **Events list & detail**: Members can browse upcoming and past events, view full details, read markdown descriptions, see participant counts, and RSVP.
- **Access control**: An event's location can be either a **Talk (voice channel)** or **free text**. When linked to a Talk, only members with *View channels* on that Talk can see the event, view its cover image, or RSVP. Free-text location events are visible to all server members.

### Past events

The **Past** tab lists events that are over (completed, cancelled, or ended), newest first, and loads older ones with **Show older events**. It uses `GET /api/events?when=past` with `limit` (1 to 200) and the cursor `before` / `before_id`: the start time and id of the last event of the previous page. Visibility is the same as for upcoming events: an event in a Talk shows only to members who may view that channel. Without `when` the endpoint keeps returning the window from one day ago to 90 days ahead.

## Managing events

Creating, editing, cancelling events, or uploading cover images requires the **Manage events** permission (`1 << 25` in `internal/perm`). Administrators and the Owner have it implicitly. On upgrade and on a new server it is granted to every role that holds *Timeout members* (the seeded Moderator and Admin roles); grant or remove it for other roles in the role settings. `@everyone` does not have it.

- **Title**: Up to 100 characters.
- **Description**: Up to 2,000 characters of Markdown (same formatting as chat messages).
- **Start & End time**: ISO 8601 timestamps. Start time is required; optional end time must be at or after start time.
- **Location**: Either a voice channel (Talk) within the server or custom free text (up to 255 characters).
- **Cover image**: Optional 3:1 banner image (PNG, JPEG, GIF or WebP up to 5 MB), processed through the same pipeline as profile banners (re-encoded, metadata stripped).

## RSVPs and reminders

Members who can view an event can submit their RSVP:
- **Going**: Indicates attendance. The event dialog displays going member avatars and count.
- **Interested**: Expresses interest.
- **Not going**: Declines attendance.

### Reminders

15 minutes before an event starts, the server automatically delivers a reminder notification (in-app notification and Web Push) to every member who RSVP'd **going** or **interested**. Reminders respect member notification settings, including Do Not Disturb and muted channels.

## Roles and waitlist

An event can have **roles with places**, for example a raid with Tank, Heal and DPS. The server advertises the feature as `events.roles` in `GET /api/health`; the web app hides the role UI without it.

- **Roles**: up to 12 per event, each with a name (up to 40 characters, unique within the event), an optional emoji, a capacity of 1 to 1,000 or none (unlimited) and a position. The event dialog lists them as cards with their fill (`3/5`) and the members.
- **Sign-up**: everyone who can view the event signs up for one role while it is scheduled or active (`PUT /api/events/{id}/signup`). A sign-up counts as RSVP *going*; leaving keeps the RSVP as it is. Signing up for another role moves the member over.
- **Waitlist**: a full role puts the next sign-up on its waitlist (`state: waitlist`, with the place in line). When a place frees (the member leaves or is removed, switches role, or the capacity grows), the oldest waitlisted member moves up first in, first out, in the same transaction, and is told in-app and by Web Push (`event_signup_notice`; do-not-disturb and a muted Talk hold the push back). A scheduler pass moves members up where a place was freed without a request, for example after an erased account.
- **No overbooking**: every roster change locks the event's role rows, so the last place is never given twice.
- **Who changes the roster**: the creator of the event and members with *Manage events* set the roles (`PUT /api/events/{id}/roles`, replaces the list; `DELETE /api/events/{id}/roles/{roleID}`) and remove sign-ups (`DELETE /api/events/{id}/signup/{userID}`; the member is told). A capacity below the confirmed members is refused with 409 naming how many to remove; a role that still has sign-ups is removed only with `?move_to=<other role>`, which takes them over. Leaving (`DELETE /api/events/{id}/signup`) is always allowed. A completed or cancelled event keeps its roster read-only.
- **Visibility**: the roster follows the event: members who cannot view the event's Talk neither see nor change it and are told nothing. `GET /api/events/{id}` carries `signups`; the list and the live updates carry `roles` with the counts and the caller's own `my_signup` only in answers to the caller's requests.
- **Role templates**: sets of roles to start from (`GET /api/event-role-templates`). Members see only the templates an administrator enabled. Two presets (*Party of five*, *Raid of ten*) ship disabled; administrators enable or disable them, create their own and delete custom ones (`POST`/`PATCH`/`DELETE`, audited as `event.role_template_*`). Nothing shows to members until then.
- **Audit, erasure and export**: role and roster changes are audited as `event.edit` (`event.talk_edit` in a Talk) with a `roster` detail. Erasing an account deletes its sign-ups; `event_signups.json` in the account export lists a member's own.

## Comments

Every event has a flat discussion under it (no replies).

- **Model**: a comment has a text of 1 to 2,000 characters; an event keeps at most 500 comments (the next one answers `409`). Posting and editing are rate limited to 10 a minute per member (`429`). Comments are plain text with the chat's Markdown.
- **API**: `GET /api/events/{id}/comments` returns the newest page, oldest first (`before` names the oldest comment already loaded; `has_more` says older ones exist; `muted` is the caller's own setting); `POST` adds a comment, `PATCH /api/events/{id}/comments/{commentID}` changes the author's own text (shown as edited), `DELETE` removes one, `PUT /api/events/{id}/comments/mute` switches the notices off or on.
- **Permissions**: everyone who can view the event reads and comments (an event in a Talk needs *View channel* on it). Authors edit and delete their own comments; members with *Manage events* or *Manage messages* delete any (audited as `event.comment_delete`, `event.talk_comment_delete` in a Talk). Nobody edits another member's words.
- **Live and notices**: a created, edited or deleted comment is announced to everyone who may see the event (`event_comment`); open lists follow. The event's creator and everyone who answered going or interested or holds a role are told about a new comment (`event_comment_notice` and Web Push, which respects do-not-disturb, focus and a muted Talk), except the author and members who switched the event's comment notices off. A member who cannot view the Talk is told nothing.
- **Erasure and export**: erasing an account deletes the member's comments and mute settings; the export holds them in `event_comments.json`.

## Active events and joining Talks

When an event's start time arrives:
- The event's status automatically transitions from `scheduled` to `active`.
- If the event takes place in a Talk channel, members see a **Join Talk** button that immediately connects to the voice channel (subject to normal channel join permissions).
- After the scheduled end time (or two hours after start if no end time was set), active events transition to `completed`.

## Privacy and audit logging

- **Audit log**: Changes are recorded in the audit log under the actions `event.create`, `event.edit`, and `event.cancel`.
- **Account erasure (GDPR)**: When a member deletes their account or an operator erases it, the member's RSVPs are purged immediately, and any events they created have their creator field set to NULL.
- **Account export**: When exporting personal data (`POST /api/users/me/export`), the member's own RSVPs are included in `rsvps.json`.
