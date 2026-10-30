# Forum channels

A **forum channel** is a structured channel type (`channels.type = 'forum'`) introduced in Mnema Talk 1.2. Instead of a flat streaming chat, a forum channel organizes discussions into posts displayed as cards, each opening a focused thread.

The server advertises the feature as `channels.forum` in the `features` list of `GET /api/health`. The web app hides forum channel creation and views when the feature is absent.

## Post model

Forum channels reuse the existing thread model rather than maintaining a separate message store:
- A **post** is a root message (`parent_id IS NULL`) in a forum channel with:
  - A required `title` (1 to 100 characters).
  - An initial message body (`content`, 1 to 4000 characters), optionally with a file (see below).
  - Up to 5 optional tags associated with the post.
  - A **solved** mark (`is_resolved`), a **lock** (`is_locked`) and the ordinary message **pin** (`is_pinned`).
- **Replies** to a post form the thread (`messages.parent_id = post.id`), using standard thread routes and WebSocket events.
- Creating a post automatically follows the post's thread for the creator, so they receive notifications for subsequent replies; anyone can follow or unfollow a post (`PUT` and `DELETE /api/threads/{threadID}/follow`).
- A post can be created with its first file in one request: `POST /api/channels/{id}/upload` accepts `title` and `tag_ids` (comma-separated) next to the file, and applies the same rules as `POST /api/channels/{id}/posts`. A message carries one file, so the web app sends further files as the author's first replies.
- The author (unless the post is locked) and members with *Manage messages* edit the title (`PATCH /api/channels/{id}/posts/{postID}`), the tags and the solved mark. The first message is edited like any message. A post is deleted like any message (the author, or *Manage messages*); its replies go with it.
- There is no automatic archiving: a post stays open until someone locks it. Lock and solved cover closing a discussion without a hidden state or a background job.

## Tags

Each forum channel can define up to 20 curated tags:
- **Management**: Members with *Manage channels* create, update, reorder and delete tags (`GET/POST /api/channels/{id}/tags`, `PATCH/DELETE /api/channels/{id}/tags/{tagID}`, `PUT /api/channels/{id}/tags/order`). The list comes back in the forum's own order (`position`); a new tag goes last.
- **Properties**: Each tag has a required name (1 to 20 characters), an optional emoji (up to 32 characters), an optional colour (a palette name: `gray`, `red`, `orange`, `yellow`, `green`, `teal`, `blue`, `purple`, `pink`; the client draws it in its own tones) and a **moderated** flag.
- **Moderated tags** are applied and removed only by members with *Manage messages*. A member who edits a post keeps its moderated tags as they are; changing one answers `403`.
- **Rules** (`GET/PATCH /api/channels/{id}/forum-settings`, *Manage channels* to change): `require_tag` makes every post carry at least one tag, on creation and on edit (the requirement is waived for a member who may apply no tag at all, so a forum whose tags are all moderated is not closed), and `guidelines` (up to 1000 characters) is shown above the new-post form.
- **Filtering**: Members filter the post list by one tag (`tag_id`).
- **Searching**: `q` (up to 100 characters) keeps the posts whose title, first message or a reply contains the text (case-insensitive, `%` and `_` are literal).
- **Sorting**: The post list can be sorted by latest activity (`sort=latest_activity`, default; `activity` is an alias) or newest creation (`sort=newest`). Other values answer `400`. To page by activity, send `before` (the last post of the previous page) together with `before_activity` (its `last_activity_at`), so a reply that arrives between two requests cannot shift the page boundary. **Pinned posts** lead the first page (no `before`) and are not repeated on later pages.
- **Tag names** are unique per forum; creating or renaming a tag to a name that exists answers `409`.
- **Audit**: tag creation, change, reorder and removal (`forum.tag_create`, `forum.tag_update`, `forum.tag_reorder`, `forum.tag_delete`), changes of the rules (`forum.settings_update`) and a moderator's change to another member's post (`forum.post_update`, with the title, solved/lock state and tags before and after) are recorded in the audit log.

## Unread posts

Each post has its own read marker per member (`forum_post_reads`). A post is unread when it, or a reply of someone else (not someone the member blocked), is newer than the member's marker; a post never opened counts from the day the member joined. `GET /api/channels/{id}/posts/unread` lists the unread posts (up to 200); `PUT /api/channels/{id}/posts/{postID}/read` sets the marker to now and answers with the marker it had, which is where the first unread reply starts ("Jump to first unread"). The channel's own unread dot in the sidebar still follows new posts only, not replies.

## Permissions and moderation

Forum channels respect the server-side permission model:

| Action | Required permission | Notes |
|---|---|---|
| View forum posts & tags | *View channels* | Hidden channels return 404. |
| Create a post | *Send messages* | Requires title and initial message. |
| Reply to a post | *Send messages* | Disallowed if post is locked (unless exempt). |
| Edit post title, tags & solved mark | Post author OR *Manage messages* | Not on a locked post, unless holding *Manage messages*. Moderated tags need *Manage messages*. |
| Pin or unpin a post | *Manage messages* | Same as pinning any message (up to 50 per channel). |
| Manage channel tags and rules | *Manage channels* | Up to 20 tags per channel. |
| Lock or unlock post | *Manage messages* | Prevents regular members from replying. |
| Delete a post | Post author OR *Manage messages* | The replies are removed with it. |

When a post is locked (`messages.is_locked = TRUE`):
- Regular members without *Manage messages* see a lock badge and notice, and the reply composer is disabled. Attempts to post a reply return 403 Forbidden.
- Moderators with *Manage messages* can continue replying or unlock the post at any time.

## Search and unread tracking

Because forum posts and replies are standard messages, they are seamlessly integrated into:
- **Search**: Full-text search and search filters (`in:channel-name`, `from:username`, `has:file`, etc.) index forum posts and their thread replies.
- **Unread & Mentions**: Unread indicators in the sidebar and mentions inbox treat forum posts and followed thread replies identically to ordinary channel messages.
