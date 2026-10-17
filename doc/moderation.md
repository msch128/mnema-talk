# Moderation and the audit log

This page describes the audit log. Code: [`internal/audit`](../internal/audit/).
Permissions and the hierarchy are in [roles-and-permissions.md](roles-and-permissions.md).

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
delete, duplicate, layout), invites (created, revoked), members (disable,
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
| `voice.join`, `voice.leave` | A member enters or leaves a Talk | Member and Talk with the time. A reload within the grace period is neither. High volume on a busy server: filter by the *Talk presence* group |
| `auth.login` | Successful sign-in (browser and native) | The member |
| `auth.login_failed`, `auth.login_locked` | A wrong password, or a blocked attempt, for an **existing** account | The member. At most one entry per account and kind per minute, and no address, password or typed name; a name that is no account is not stored at all |
| `auth.register`, `auth.password_change`, `auth.logout_all` | Own-account events | The member |

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
*Audit log* tab). *Administrator* includes it. Entries about webhooks and bots, system and media
administration, settings and the Owner-only account actions (setting or
resetting a password, clearing a login lock) are only returned to the Owner and
Administrators, even to holders of *View audit log* (`audit.Sensitive`). The
audit log is not sent to outgoing webhooks.

### API

`GET /api/admin/audit-log` takes the filters `actor` (user id), `action` (exact
or a group: `role` matches every `role.*`), `target_type`, `target_id`, `from`
and `to` (RFC 3339 or `YYYY-MM-DD`), `before` (an entry id, for the next page)
and `limit` (1 to 200, default 50). It returns `{entries, next_before}`, newest
first. See [`api/openapi.json`](../api/openapi.json).
