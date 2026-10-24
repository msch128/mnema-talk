# Community events

Community events let organizers schedule voice hangouts or server-wide activities, gather RSVPs, and remind members before start. The server advertises the feature as `events` in the `features` list of `GET /api/health`; web clients hide the events interface when the flag is absent.

## Overview

- **Sidebar entry**: An **Events** link appears at the top of the channel sidebar, showing a count badge of upcoming events. Clicking it opens the Events list dialog.
- **Events list & detail**: Members can browse upcoming and past events, view full details, read markdown descriptions, see participant counts, and RSVP.
- **Access control**: An event's location can be either a **Talk (voice channel)** or **free text**. When linked to a Talk, only members with *View channels* on that Talk can see the event, view its cover image, or RSVP. Free-text location events are visible to all server members.

## Managing events

Creating, editing, cancelling events, or uploading cover images requires the **Manage events** permission (`1 << 24` in `internal/perm`). Seeded Admin and Moderator roles hold this permission by default; `@everyone` does not.

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

## Active events and joining Talks

When an event's start time arrives:
- The event's status automatically transitions from `scheduled` to `active`.
- If the event takes place in a Talk channel, members see a **Join Talk** button that immediately connects to the voice channel (subject to normal channel join permissions).
- After the scheduled end time (or two hours after start if no end time was set), active events transition to `completed`.

## Privacy and audit logging

- **Audit log**: Changes are recorded in the audit log under the actions `event.create`, `event.edit`, and `event.cancel`.
- **Account erasure (GDPR)**: When a member deletes their account or an operator erases it, the member's RSVPs are purged immediately, and any events they created have their creator field set to NULL.
- **Account export**: When exporting personal data (`POST /api/users/me/export`), the member's own RSVPs are included in `rsvps.json`.
