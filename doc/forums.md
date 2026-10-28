# Forum channels

A **forum channel** is a structured channel type (`channels.type = 'forum'`) introduced in Mnema Talk 1.2. Instead of a flat streaming chat, a forum channel organizes discussions into posts displayed as cards, each opening a focused thread.

The server advertises the feature as `channels.forum` in the `features` list of `GET /api/health`. The web app hides forum channel creation and views when the feature is absent.

## Post model

Forum channels reuse the existing thread model rather than maintaining a separate message store:
- A **post** is a root message (`parent_id IS NULL`) in a forum channel with:
  - A required `title` (1 to 100 characters).
  - An initial message body (`content`, 1 to 4000 characters).
  - Up to 5 optional tags associated with the post.
- **Replies** to a post form the thread (`messages.parent_id = post.id`), using standard thread routes and WebSocket events.
- Creating a post automatically follows the post's thread for the creator, so they receive unread indicators and notifications for subsequent replies.

## Tags

Each forum channel can define up to 20 curated tags:
- **Admin management**: Members with *Manage channels* can create, update and delete tags (`GET/POST /api/channels/{id}/tags`, `PATCH/DELETE /api/channels/{id}/tags/{tagID}`).
- **Properties**: Each tag has a required name (1 to 20 characters) and an optional emoji (up to 32 characters).
- **Filtering**: Members can filter posts in the forum view by selecting tag pills.
- **Sorting**: The post list can be sorted by latest activity (`sort=latest_activity`, default; `activity` is an alias) or newest creation (`sort=newest`). Other values answer `400`. To page by activity, send `before` (the last post of the previous page) together with `before_activity` (its `last_activity_at`), so a reply that arrives between two requests cannot shift the page boundary.
- **Tag names** are unique per forum; creating or renaming a tag to a name that exists answers `409`.
- **Audit**: tag creation, change and removal (`forum.tag_create`, `forum.tag_update`, `forum.tag_delete`) and a moderator's change to another member's post (`forum.post_update`) are recorded in the audit log.

## Permissions and moderation

Forum channels respect the server-side permission model:

| Action | Required permission | Notes |
|---|---|---|
| View forum posts & tags | *View channels* | Hidden channels return 404. |
| Create a post | *Send messages* | Requires title and initial message. |
| Reply to a post | *Send messages* | Disallowed if post is locked (unless exempt). |
| Edit post title & tags | Post author OR *Manage messages* | Non-authors cannot edit unless holding *Manage messages*. |
| Manage channel tags | *Manage channels* | Up to 20 tags per channel. |
| Lock or unlock post | *Manage messages* | Prevents regular members from replying. |

When a post is locked (`messages.is_locked = TRUE`):
- Regular members without *Manage messages* see a lock badge and notice, and the reply composer is disabled. Attempts to post a reply return 403 Forbidden.
- Moderators with *Manage messages* can continue replying or unlock the post at any time.

## Search and unread tracking

Because forum posts and replies are standard messages, they are seamlessly integrated into:
- **Search**: Full-text search and search filters (`in:channel-name`, `from:username`, `has:file`, etc.) index forum posts and their thread replies.
- **Unread & Mentions**: Unread indicators in the sidebar and mentions inbox treat forum posts and followed thread replies identically to ordinary channel messages.
