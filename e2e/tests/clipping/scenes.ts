import type { Locator, Page } from '@playwright/test'
import { expect } from '../fixtures'
import type { Combo } from './matrix'
import { IMAGE, type World } from './seed'
import { type Socket, joinTalk, seatParticipants } from './talk'

// Every view and state the sweep audits, as a table: a name, whether it runs
// in the smoke subset, and how to get there from a signed-in page. A scene
// leaves the page in the state to audit; the spec audits and moves on. Scenes
// that need a Talk come last, because leaving the Talk would undo them.

export interface SceneContext {
  page: Page
  world: World
  combo: Combo
  socket: Socket
  t: (key: string, params?: Record<string, string>) => string
  /** The phone layout (below 768 CSS pixels): drawers instead of columns. */
  compact: boolean
}

export interface Scene {
  name: string
  smoke?: boolean
  reach: (s: SceneContext) => Promise<void>
  /** Undoes what the scene left on the server (drafts, staged uploads), so the next size starts clean. */
  leave?: (s: SceneContext) => Promise<void>
}

// Settings and admin tabs, in the order the app lists them (shared/lib/settingsDialog.ts, admin/lib/adminNav.ts).
const SETTINGS_TABS = ['audio', 'app', 'signin', 'sessions', 'privacy', 'data']
const ADMIN_TABS = ['users', 'roles', 'channels', 'invites', 'onboarding', 'access', 'emoji', 'reports', 'automod', 'bans', 'audit', 'bots', 'import', 'media', 'system', 'status']

const STAGED = [1, 2, 3].map(n => `a-picture-with-a-long-file-name-number-${n}.png`)

/** Matches a name that starts with the text (menu items end with their shortcut key). */
const startsWith = (text: string) => new RegExp(`^${text.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')}`)

/** Loads a path and waits until the shell is there and settled. */
export async function open(page: Page, path: string) {
  await page.goto(path)
  await expect(page.getByTestId('own-profile-button')).toBeAttached()
}

async function openChat(s: SceneContext) {
  await open(s.page, `/c/${s.world.chat.id}`)
  await expect(s.page.locator('[data-msg-id]', { hasText: s.world.lastMessageText })).toBeVisible()
}

async function openDrawer(s: SceneContext) {
  const drawer = s.page.getByTestId('channel-drawer')
  if (await drawer.evaluate(el => el.hasAttribute('inert'))) await s.page.getByTestId('open-nav').click()
  await expect(drawer).toBeInViewport({ ratio: 1 })
}

/** The message list's scroll container. */
function messageScroller(page: Page): Locator {
  return page.locator('[data-msg-id]').first().locator('xpath=..')
}

async function scrollMessages(s: SceneContext, to: 'top' | 'middle') {
  const list = messageScroller(s.page)
  // The list loads older pages as it nears the top; scroll until it stops growing.
  for (let i = 0; i < 4; i++) {
    await list.evaluate((el, where) => { el.scrollTop = where === 'top' ? 0 : (el.scrollHeight - el.clientHeight) / 2 }, to)
    await s.page.waitForTimeout(300)
  }
}

// The header buttons that fold into its "more" menu on a narrow window, with that menu item's label.
const FOLDED: Record<string, string> = { 'mentions-inbox-button': 'mentions.inbox', 'saved-button': 'panel.title' }

async function headerButton(s: SceneContext, testid: string) {
  const button = s.page.getByTestId(testid)
  const label = FOLDED[testid]
  if (label && !(await button.isVisible())) {
    await s.page.getByTestId('header-more').click()
    await s.page.getByRole('menuitem', { name: s.t(label) }).click()
    return
  }
  await button.click()
}

async function messageMenu(s: SceneContext, text: string, corner: 'centre' | 'bottom-right' = 'centre') {
  const row = s.page.locator('[data-msg-id]', { hasText: text }).last()
  // Rows move while banners (notifications, tips) arrive, so the click is aimed at the row as it is then, and retried.
  await expect(async () => {
    await s.page.keyboard.press('Escape')
    await row.scrollIntoViewIfNeeded()
    const box = await row.boundingBox()
    if (!box) throw new Error('message not on screen')
    const position = corner === 'centre' ? { x: box.width / 2, y: box.height / 2 } : { x: box.width - 4, y: box.height - 4 }
    await row.click({ button: 'right', position })
    await expect(s.page.getByRole('menu')).toBeVisible({ timeout: 2_000 })
  }).toPass({ timeout: 15_000 })
}

const chatScenes: Scene[] = [
  { name: 'chat newest messages', smoke: true, reach: openChat },
  { name: 'chat oldest messages', smoke: true, reach: async (s) => { await openChat(s); await scrollMessages(s, 'top') } },
  { name: 'chat middle of the history', reach: async (s) => { await openChat(s); await scrollMessages(s, 'middle') } },
  {
    name: 'composer with a reply and attachments', smoke: true,
    reach: async (s) => {
      await openChat(s)
      await messageMenu(s, s.world.lastMessageText)
      // The item's name ends with its shortcut key.
      await s.page.getByRole('menuitem', { name: startsWith(s.t('chat.replyInChannel')) }).click()
      await s.page.locator('input[type="file"]').first().setInputFiles(STAGED.map(name => ({ name, mimeType: 'image/png', buffer: IMAGE })))
      await expect(s.page.getByTestId('attachment-tray')).toBeVisible()
    },
    // Staged uploads count against the member's limit of unsent files, and the reply is a draft.
    leave: async (s) => {
      for (const name of STAGED) await s.page.getByRole('button', { name: s.t('chat.attachRemove', { name }) }).click()
      await s.page.getByRole('button', { name: s.t('chat.cancelReply') }).first().click()
    },
  },
  {
    name: 'message menu near the bottom right', smoke: true,
    reach: async (s) => { await openChat(s); await messageMenu(s, s.world.lastMessageText, 'bottom-right') },
  },
  {
    name: 'confirm dialog: delete a message',
    reach: async (s) => {
      await openChat(s)
      await messageMenu(s, s.world.lastMessageText)
      await s.page.getByRole('menuitem', { name: startsWith(s.t('chat.delete')) }).click()
      await expect(s.page.getByRole('alertdialog')).toBeVisible()
    },
  },
  {
    name: 'toast',
    reach: async (s) => {
      await openChat(s)
      await messageMenu(s, s.world.lastMessageText)
      await s.page.getByRole('menuitem', { name: startsWith(s.t('chat.copyLink')) }).click()
      await expect(s.page.locator('[data-toast]').first()).toBeVisible()
    },
  },
  {
    name: 'image viewer',
    reach: async (s) => {
      await openChat(s)
      const image = s.page.locator('[data-attachment="image"] img').first()
      await image.scrollIntoViewIfNeeded()
      await image.click()
      await expect(s.page.getByTestId('media-viewer')).toBeVisible()
    },
  },
  {
    name: 'member list', smoke: true,
    reach: async (s) => {
      await openChat(s)
      const toggle = s.page.getByRole('button', { name: s.t('members.toggle') })
      if ((await toggle.getAttribute('aria-pressed')) !== 'true') await toggle.click()
      await expect(s.page.locator('[data-member]').first()).toBeVisible()
    },
  },
  {
    name: 'member menu at the right edge',
    reach: async (s) => {
      await openChat(s)
      const toggle = s.page.getByRole('button', { name: s.t('members.toggle') })
      if ((await toggle.getAttribute('aria-pressed')) !== 'true') await toggle.click()
      const row = s.page.locator('[data-member]').last()
      await row.scrollIntoViewIfNeeded()
      const box = await row.boundingBox()
      if (!box) throw new Error('member row not on screen')
      await s.page.mouse.click(box.x + box.width - 4, box.y + box.height / 2, { button: 'right' })
      await expect(s.page.getByRole('menu')).toBeVisible()
    },
  },
  {
    name: 'search results',
    reach: async (s) => {
      await openChat(s)
      await headerButton(s, 'search-in-channel')
      // The channel's messages are listed at once; Enter would open the first one.
      await expect(s.page.getByTestId('search-results-header')).toBeVisible()
    },
  },
  {
    name: 'mentions inbox',
    reach: async (s) => {
      await openChat(s)
      await headerButton(s, 'mentions-inbox-button')
      await expect(s.page.getByTestId('mentions-inbox')).toBeVisible()
    },
  },
  {
    name: 'saved messages',
    reach: async (s) => { await openChat(s); await headerButton(s, 'saved-button'); await expect(s.page.getByTestId('saved-panel')).toBeVisible() },
  },
  {
    name: 'later list',
    reach: async (s) => {
      await openChat(s)
      await headerButton(s, 'saved-button')
      await s.page.getByTestId('panel-tab-later').click()
      await expect(s.page.getByTestId('later-list').or(s.page.getByTestId('later-empty'))).toBeVisible()
    },
  },
  {
    name: 'thread panel',
    reach: async (s) => {
      await openChat(s)
      await scrollMessages(s, 'top')
      await s.page.getByRole('button', { name: new RegExp(s.t('chat.openThread')) }).first().click()
      await expect(s.page.getByTestId('thread-close')).toBeVisible()
    },
  },
  {
    name: 'channel with the longest name, empty', smoke: true,
    reach: async (s) => { await open(s.page, `/c/${s.world.longName.id}`); await expect(s.page.getByTestId('chat-channel-name')).toBeVisible() },
  },
  {
    name: 'a list that fails to load',
    reach: async (s) => {
      await s.page.route('**/api/mentions?**', route => route.fulfill({ status: 500, body: '{}' }))
      await openChat(s)
      await headerButton(s, 'mentions-inbox-button')
      await expect(s.page.getByTestId('state-retry')).toBeVisible()
      await s.page.unroute('**/api/mentions?**')
    },
  },
  {
    name: 'channel while loading',
    reach: async (s) => {
      await s.page.route(`**/api/channels/${s.world.chat.id}/messages**`, async (route) => { await new Promise(r => setTimeout(r, 8_000)); await route.continue().catch(() => undefined) })
      await s.page.goto(`/c/${s.world.chat.id}`)
      await expect(s.page.getByTestId('own-profile-button')).toBeAttached()
      await s.page.waitForTimeout(1_000)
      await s.page.unroute(`**/api/channels/${s.world.chat.id}/messages**`)
    },
  },
]

const otherScenes: Scene[] = [
  { name: 'direct message', reach: async (s) => { await open(s.page, `/dm/${s.world.dmId}`); await expect(s.page.getByTestId('dm-header-title').or(s.page.getByTestId('chat-channel-name')).first()).toBeVisible() } },
  { name: 'group conversation', reach: async (s) => { await open(s.page, `/dm/g/${s.world.groupDmId}`); await expect(s.page.getByTestId('dm-header-title').or(s.page.getByTestId('chat-channel-name')).first()).toBeVisible() } },
  { name: 'forum', smoke: true, reach: async (s) => { await open(s.page, `/c/${s.world.forum.id}`); await expect(s.page.getByTestId('forum-post-card').first()).toBeVisible() } },
  {
    name: 'forum post',
    reach: async (s) => {
      await open(s.page, `/c/${s.world.forum.id}`)
      await s.page.getByTestId('forum-post-card').first().click()
      await expect(s.page.getByTestId('forum-post-header')).toBeVisible()
    },
  },
  { name: 'wiki page', smoke: true, reach: async (s) => { await open(s.page, `/wiki/${s.world.wikiSlug}`); await expect(s.page.getByTestId('wiki-title')).toBeVisible() } },
  {
    name: 'wiki editor',
    reach: async (s) => {
      await open(s.page, `/wiki/${s.world.wikiSlug}`)
      await s.page.getByTestId('wiki-edit').click()
      await expect(s.page.getByTestId('wiki-editor')).toBeVisible()
    },
  },
  {
    name: 'events list',
    reach: async (s) => {
      await open(s.page, '/')
      await openDrawer(s)
      await s.page.getByRole('button', { name: s.t('events.events') }).click()
      await expect(s.page.getByRole('dialog')).toBeVisible()
    },
  },
  {
    name: 'event with comments',
    reach: async (s) => {
      await open(s.page, '/')
      await openDrawer(s)
      await s.page.getByRole('button', { name: s.t('events.events') }).click()
      await s.page.getByRole('dialog').getByText('Community evening', { exact: false }).first().click()
      await expect(s.page.getByTestId('event-comments')).toBeVisible()
    },
  },
  {
    name: 'profile card', smoke: true,
    reach: async (s) => {
      await openChat(s)
      await openDrawer(s)
      await s.page.getByTestId('own-profile-button').click()
      await expect(s.page.getByTestId('profile-presence')).toBeVisible()
    },
  },
  {
    name: 'create channel dialog',
    reach: async (s) => {
      await open(s.page, '/')
      await openDrawer(s)
      await s.page.locator('[aria-controls="server-menu"]').click()
      await s.page.getByRole('menuitem', { name: s.t('channel.create') }).click()
      await expect(s.page.getByRole('dialog')).toBeVisible()
    },
  },
  {
    name: 'server menu',
    reach: async (s) => {
      await open(s.page, '/')
      await openDrawer(s)
      await s.page.locator('[aria-controls="server-menu"]').click()
      await expect(s.page.getByRole('menu')).toBeVisible()
    },
  },
  {
    name: 'account menu',
    reach: async (s) => {
      await open(s.page, '/')
      await openDrawer(s)
      await s.page.getByTestId('account-menu-button').click()
      await expect(s.page.getByRole('menu')).toBeVisible()
    },
  },
  ...SETTINGS_TABS.map((tab): Scene => ({
    name: `settings: ${tab}`, smoke: tab === 'signin',
    reach: async (s) => { await open(s.page, `/settings/${tab}`); await expect(s.page.getByTestId(`settings-tab-${tab}`)).toHaveAttribute('aria-selected', 'true') },
  })),
  {
    name: 'admin console start',
    reach: async (s) => { await open(s.page, '/admin'); await expect(s.page.getByTestId('admin-tabs')).toBeVisible() },
  },
  ...ADMIN_TABS.map((tab): Scene => ({
    name: `admin: ${tab}`, smoke: tab === 'users' || tab === 'audit',
    reach: async (s) => {
      await open(s.page, `/admin/${tab}`)
      // On a phone the console starts with the list of tabs; the tab opens on its own page with a back button.
      const button = s.page.getByTestId(`tab-${tab}`)
      if (!s.compact) return expect(button).toHaveAttribute('aria-pressed', 'true')
      const back = s.page.getByTestId('admin-back')
      await expect(button.or(back).first()).toBeVisible()
      if (!(await back.isVisible())) await button.click()
      await expect(back).toBeVisible()
    },
  })),
]

async function talkWith(s: SceneContext, seats: number, seated: number) {
  if (seats > seated) await seatParticipants(s.page, s.socket, s.world.talk.id, seats - seated, seated - 1)
  await expect(s.page.locator('nav [data-voice-user]')).toHaveCount(seats)
}

/** Talk scenes share one join; `seated` counts who is in the Talk so far. */
export function talkScenes(): Scene[] {
  let seated = 0
  const seat = (n: number) => async (s: SceneContext) => {
    if (seated === 0) {
      await open(s.page, `/c/${s.world.chat.id}`)
      await joinTalk(s.page, s.world.talk.name, s.t('voice.takeoverConfirm'))
      seated = 1
    }
    await talkWith(s, n, seated)
    seated = n
    if (s.compact) return
    await expect(s.page.getByTestId('voice-panel-open')).toBeVisible()
  }
  return [
    { name: 'Talk alone', smoke: true, reach: seat(1) },
    { name: 'voice panel in the sidebar', smoke: true, reach: async (s) => { await seat(1)(s); await openDrawer(s) } },
    { name: 'Talk with 2', reach: seat(2) },
    { name: 'Talk with 9', reach: seat(9) },
    { name: 'Talk with 25', smoke: true, reach: seat(25) },
    {
      name: 'Talk sharing a screen',
      reach: async (s) => {
        await seat(25)(s)
        await s.page.getByRole('button', { name: s.t('voice.share') }).first().click()
        await expect(s.page.locator('video').first()).toBeAttached()
      },
    },
  ]
}

export function allScenes(): Scene[] {
  return [...chatScenes, ...otherScenes, ...talkScenes()]
}
