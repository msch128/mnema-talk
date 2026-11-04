# Wiki

The wiki is the community's shared handbook: pages in a tree, written by the
people the community trusts with it, readable by everybody, with a full history
of every change. It is a section of the community, like [events](events.md),
not a channel: the entry sits beside *Events* in the sidebar, and a page is at
`/wiki/<address>`.

This page covers using the wiki and running it. The API is in the
[OpenAPI document](../api/openapi.json) (tag *Wiki*).

## Who may do what

| | Read | Edit | Manage |
|---|---|---|---|
| Permission | *View the wiki* | *Edit the wiki* | *Manage the wiki* |
| Default | on for @everyone | **off** for @everyone | **off** for @everyone |
| Pages, history, search | read | create, change title and text, restore an earlier revision | |
| Lock, move, rename, delete | | | yes |
| Deleted pages | | | read and restore |

- Everybody reads by default; switch *View the wiki* off for @everyone to close
  the wiki to members who have no role with it.
- **Editing is not granted to all members.** A member who joins can read, not
  write, so one account cannot wipe pages. Give *Edit the wiki* to the roles you
  trust with the wiki (and *Manage the wiki* to those who revert vandalism).
  The seeded Moderator and Admin roles, and any role that holds *Timeout members*,
  get both by migration 0089; Administrators and the Owner hold everything.
- *Edit the wiki* and *Manage the wiki* are taken by a timeout (a timed-out
  manager cannot lock, delete, rename or move either). *Edit the wiki* also
  behaves like writing a message: the timeout takes it away, and
  so does the new-member wait of [raid protection](moderation.md#raid-protection)
  (*Bypass new member limits* and Administrators are exempt). Reading stays.
- Neither editing nor managing can be put on a self-assignable role.
- A **locked** page changes only for holders of *Manage the wiki*; use it for
  pages that must not move (the rules, say). Changing the text of a locked page
  needs *Edit the wiki* as well: a manager without it can lock, unlock, move and
  delete, but not write.
- A title is one line (no line breaks, no invisible or direction-changing
  characters) and unique among the pages, ignoring case, so `[[Title]]` names
  one page.

## Writing

Pages use the same Markdown as messages (headings up to level 3, lists, quotes,
code, links), rendered by the same safe renderer: no HTML, no scripts. Two
additions apply in the wiki only: text up to 50 KB, and `[[Page title]]`, a link
to another page by its title or address (it shows as plain text while no such
page exists, and follows a renamed page).

- A page has a title (100 characters) and an **address** (the slug, lower-case
  letters, digits and single hyphens, 80 characters). The address derives from
  the title (`Größe & Ärger` → `groesse-aerger`, `-2` when taken) unless you
  choose it. Words the wiki uses itself (`search`, `new`, `edit`, `history`,
  `recent`, `deleted`, `api`, `admin`, `wiki`, `pages`, `revisions`) are reserved.
- Pages nest at most **5 levels** deep and the wiki holds at most **500 pages**
  (deleted ones do not count). Moving a page moves its subpages with it.
- Renaming (a *Manage the wiki* action) leaves a **redirect** from the old
  address; links and bookmarks keep working, and the old address is not free
  for another page.
- Saving needs the revision you started from. If somebody saved meanwhile, the
  save is **refused** (nothing is overwritten) and you open the page again and
  apply your change to the new version. The refusal is `409` with the stable
  code `STALE_REVISION`.
- Each member may make 20 wiki changes a minute.

## History and undoing vandalism

Every save writes a **revision**: who, when, the summary they gave, and the
complete title and text. The page's *History* lists them, shows what changed
between two (a line diff) and restores one.

- **Restoring never overwrites or removes anything:** it saves the old title and
  text as a *new* revision that says which one it came from. You can restore the
  restore.
- *Revert vandalism* is two clicks: open *History*, pick the last good revision,
  *Restore*. Lock the page afterwards if it keeps happening.
- A page keeps its **latest 100 revisions**. Older ones are pruned after a save,
  except a revision that one of the kept revisions is a restore of. The history
  therefore never loses the thing a recent restore points to.
- **Deleting** a page only takes it out of the wiki: it keeps its address and
  all revisions, and a manager restores it from *Deleted pages*. A page with
  subpages is not deleted until they are moved or deleted.

The [audit log](moderation.md#audit-log) records `wiki.create`, `wiki.edit`,
`wiki.move` (move or rename), `wiki.delete`, `wiki.restore` (a revision or a
deleted page) and `wiki.lock` (locked or unlocked), with the page and, for
edits, the revision number and summary.

## Search

*Search the wiki* finds pages whose title or text contains the words (at least
three characters), titles first, with a few words around the match. Deleted pages are never found.

## Privacy

- The wiki is **shared**: what a member writes belongs to the community's
  handbook. When an account is erased, the pages and revisions stay and are
  detached from the person (`editor_id` is set to nothing; the history shows
  "a former member"). Wiki pages are not deleted with the author.
- The account export (`wiki_revisions.json`) holds every revision the member
  saved: the page's address, the title and text they wrote, the summary and
  the time.
- Pages are readable by every member who may view the wiki; do not put anything
  into it that members are not meant to read. Messages in private channels or
  direct messages are never copied into it.

## For operators

- The 50 KB, 500 pages, 5 levels, 100 revisions and 20 changes a minute are
  fixed. Worst case, every one of the 500 pages is full (50 KB) and holds its
  100 latest revisions: about 2.5 GB, plus, for 90 days, one more revision per
  editor and day and page. A real wiki is a few megabytes; the history list
  never reads the texts, only their sizes.
- Pruning: besides the latest 100, a page keeps for 90 days the last revision of
  every editor and day, and every revision a kept restore points to. One
  account that saves over and over (20 a minute at most) therefore cannot push
  the good revisions of other members out; an old good revision stays
  restorable.
- Migration 0089 adds the tables `wiki_pages`, `wiki_revisions` and
  `wiki_redirects`, turns on *View the wiki* for @everyone and grants *Edit* and
  *Manage the wiki* to roles holding *Timeout members*. The bits are
  `1 << 27` (edit), `1 << 28` (manage) and `1 << 29` (view); see
  [roles and permissions](roles-and-permissions.md).
- `GET /api/health` lists `wiki` among its features.
