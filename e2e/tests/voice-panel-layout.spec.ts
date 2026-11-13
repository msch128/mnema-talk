import type { BrowserContext, Locator, Page } from '@playwright/test'
import { apiFetch, bounds, trackPeerConnections, expectTalkLive, leaveAndClose } from './helpers'
import { test, expect } from './fixtures'
import { fakeDisplayMedia } from './clipping/talk'

// The voice panel at the bottom of the sidebar ("Connected · <Talk>", the
// ping and the connection details, the call's buttons) and the user bar under
// it stay whole while seated in a Talk: at the desktop client's default and
// minimum window, common laptop screens and a phone, with the channel list at
// its narrowest and widest, in English and German, connected and sharing a
// screen, with the longest Talk name and status line the server allows. Every
// text either fits, or is cut to at least 2.5em with an ellipsis and its full
// text in a tooltip, or is left out; a sliver is a failure. The channel list
// above the panel is what scrolls.

const ADMIN_USER = process.env.E2E_ADMIN_USER || 'cool_guy'
const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD ?? ''
if (!ADMIN_PASSWORD) throw new Error('E2E_ADMIN_PASSWORD is required (run via e2e/run.sh)')

const opened: BrowserContext[] = []
// Leaves by the panel's own button, which has the same test id in both languages.
test.afterEach(async () => {
  for (const ctx of opened) {
    for (const page of ctx.pages()) await page.getByTestId('voice-panel-leave').click({ timeout: 2_000 }).catch(() => undefined)
  }
  await leaveAndClose(opened)
})

const suffix = Date.now().toString(36)
// 64 characters, the longest Talk name (internal/chat/core MaxChannelNameLen).
const TALK = `Long Talk name for the voice panel layout check ${suffix}`.padEnd(64, 'x').slice(0, 64)
// 32 characters, the longest status line (internal/auth MaxStatusTextLen).
const STATUS = 'Back after the long weekend trip'

// The desktop client opens at 1200x820 with a minimum of 640x480 and keeps 68px
// for its server rail on the left (mnema-talk-desktop: web_shell/window.rs, RAIL_WIDTH).
interface Size { name: string; width: number; height: number; sidebar?: 'Home' | 'End' }
const SIZES: Size[] = [
  { name: 'phone', width: 390, height: 844 },
  { name: 'desktop minimum', width: 572, height: 480 },
  { name: 'desktop default', width: 1132, height: 820 },
  { name: 'desktop default, narrowest channel list', width: 1132, height: 820, sidebar: 'Home' },
  { name: 'desktop default, widest channel list', width: 1132, height: 820, sidebar: 'End' },
  { name: 'laptop', width: 1366, height: 768 },
  { name: 'small laptop', width: 1280, height: 720 },
]

// The interface speaks the account's stored language (stores/auth/locale.ts), so
// each test stores its own and the Owner goes back to German afterwards, as the other specs expect.
const LOCALES = [
  { locale: 'en', status: 'Connected', details: 'Connection details', takeover: 'Take over here', resize: 'Resize the channel list' },
  { locale: 'de', status: 'Verbunden', details: 'Verbindungsdetails', takeover: 'Hier übernehmen', resize: 'Breite der Kanalliste anpassen' },
] as const
type Labels = (typeof LOCALES)[number]

async function openDrawer(page: Page) {
  const drawer = page.getByTestId('channel-drawer')
  if (await drawer.evaluate(el => el.hasAttribute('inert'))) await page.getByTestId('open-nav').click()
  await expect(drawer).toBeInViewport({ ratio: 1 })
}

/**
 * Opens the drawer once the window has settled after a resize: the Talk view
 * reacts to a new size (its layout, the phone breakpoint) and may close the
 * drawer while it does. The drawer has to stay open on its own afterwards.
 */
async function openSettledDrawer(page: Page) {
  await expect(async () => {
    await openDrawer(page)
    await page.waitForTimeout(400)
    await expect(page.getByTestId('channel-drawer')).toBeInViewport({ ratio: 1, timeout: 100 })
  }).toPass({ timeout: 10_000 })
}

/** Joins the Talk; a session of an earlier attempt still listed in it is taken over. */
async function joinTalk(page: Page, takeoverLabel: string) {
  await openDrawer(page)
  await page.locator('[data-channel-type="voice"]', { hasText: TALK }).first().click()
  const takeover = page.getByRole('button', { name: takeoverLabel })
  await takeover.or(page.getByTestId('voice-panel-leave')).first().waitFor()
  if (await takeover.isVisible()) await takeover.click()
  await expectTalkLive(page)
}

/** Shown texts in the panel and the user bar that are cut keep at least 2.5em: a sliver of a letter is not a name. */
async function expectNoSlivers(page: Page, where: string) {
  const report = await page.locator('section.vpanel-wrap, [data-testid="own-profile-button"]').evaluateAll((roots) => {
    const out: Array<{ text: string; width: number; em: number; cut: boolean }> = []
    for (const root of roots) {
      for (const el of root.querySelectorAll<HTMLElement>('span, p')) {
        const text = (el.textContent ?? '').trim()
        const s = getComputedStyle(el)
        if (!text || el.children.length > 0 || s.display === 'none' || el.getClientRects().length === 0) continue
        out.push({ text, width: el.clientWidth, em: parseFloat(s.fontSize), cut: el.scrollWidth > el.clientWidth + 1 })
      }
    }
    return out
  })
  const slivers = report.filter(r => r.cut && r.width < 2.5 * r.em).map(r => `"${r.text.slice(0, 24)}" ${r.width}px`)
  expect(slivers, `${where}: cut to a sliver`).toEqual([])
}

/** The word is drawn in full: not under the ellipsis of a cut text, not past its box. */
function wordShown(root: Locator, word: string): Promise<boolean> {
  return root.evaluate((el, word) => {
    const walker = document.createTreeWalker(el, NodeFilter.SHOW_TEXT)
    for (let node = walker.nextNode(); node; node = walker.nextNode()) {
      const at = node.textContent?.indexOf(word) ?? -1
      if (at < 0 || !node.parentElement) continue
      const range = document.createRange()
      range.setStart(node, at)
      range.setEnd(node, at + word.length)
      const box = node.parentElement.getBoundingClientRect()
      const cut = node.parentElement.scrollWidth > node.parentElement.clientWidth + 1
      const ellipsis = cut ? parseFloat(getComputedStyle(node.parentElement).fontSize) : 0
      return range.getBoundingClientRect().right <= box.right - ellipsis + 0.5
    }
    return false
  }, word)
}

/** The element at the centre of each shown control is the control itself: nothing lies over it. */
async function expectUncovered(controls: Locator[], where: string) {
  for (const control of controls) {
    if (!(await control.isVisible())) continue
    const free = await control.evaluate((el) => {
      const r = el.getBoundingClientRect()
      const hit = document.elementFromPoint(r.left + r.width / 2, r.top + r.height / 2)
      return !!hit && (hit === el || el.contains(hit))
    })
    expect(free, `${where}: ${await control.getAttribute('data-testid')} is covered`).toBe(true)
  }
}

async function expectPanelWhole(page: Page, labels: Labels, where: string) {
  const panel = page.locator('section.vpanel-wrap')
  const ids = ['voice-panel-open', 'voice-panel-ping', 'toggle-mute', 'toggle-deafen', 'voice-panel-share', 'voice-panel-stream-audio', 'voice-panel-leave']
  const controls = ids.map(id => page.getByTestId(id))

  // Whole panel on screen, inside the drawer and above the user bar, every shown control uncovered.
  await expect(panel, where).toBeInViewport({ ratio: 1 })
  for (const control of controls) if (await control.isVisible()) await expect(control, where).toBeInViewport({ ratio: 1 })
  const drawer = await bounds(page.getByTestId('channel-drawer'))
  const box = await bounds(panel)
  expect(box.y + box.height, `${where}: panel bottom inside the drawer`).toBeLessThanOrEqual(drawer.y + drawer.height + 0.5)
  expect(box.x + box.width, `${where}: panel right edge inside the drawer`).toBeLessThanOrEqual(drawer.x + drawer.width + 0.5)
  const own = await bounds(page.getByTestId('own-profile-button'))
  expect(box.y + box.height, `${where}: panel above the user bar`).toBeLessThanOrEqual(own.y + 0.5)
  expect(own.y + own.height, `${where}: user bar inside the window`).toBeLessThanOrEqual((page.viewportSize()?.height ?? 0) + 0.5)
  await expectUncovered(controls, where)

  // The connection details are whole, the status word stands on its own, no text is a sliver.
  const details = panel.getByText(labels.details, { exact: true })
  await expect(details, `${where}: connection details shown`).toBeVisible()
  expect(await details.evaluate(el => el.scrollWidth <= el.clientWidth + 1), `${where}: connection details cut`).toBe(true)
  await expectNoSlivers(page, where)
  expect(await wordShown(page.getByTestId('voice-panel-open'), labels.status), `${where}: "${labels.status}" whole`).toBe(true)

  // The channel list above it scrolls instead of pushing the panel down.
  const nav = page.getByRole('navigation').filter({ has: page.locator('[data-channel-type]') })
  const list = await nav.evaluate(el => ({ overflow: getComputedStyle(el).overflowY, bottom: el.getBoundingClientRect().bottom }))
  expect(list.overflow).toBe('auto')
  expect(list.bottom, `${where}: channel list ends above the panel`).toBeLessThanOrEqual(box.y + 0.5)
}

/** A cut Talk name and status line are readable in full in their tooltips. */
async function expectFullTextInTooltips(page: Page) {
  const tip = page.getByRole('tooltip')
  for (const [testid, full] of [['voice-panel-channel', TALK], ['own-subline', STATUS]] as const) {
    const el = page.getByTestId(testid)
    if (!(await el.evaluate(e => e.scrollWidth > e.clientWidth + 1))) continue
    await el.hover()
    await expect(tip).toContainText(full)
  }
  await page.mouse.move(0, 0)
  await expect(tip).toBeHidden()
}

async function checkEverySize(page: Page, labels: Labels, state: string) {
  for (const size of SIZES) {
    await page.setViewportSize({ width: size.width, height: size.height })
    await openSettledDrawer(page)
    if (size.sidebar) await page.getByRole('separator', { name: labels.resize }).press(size.sidebar)
    const where = `${state}, ${size.name} ${size.width}x${size.height}`
    await expectPanelWhole(page, labels, where)
    if (size.name === 'desktop default') await expectFullTextInTooltips(page)
    // E2E_SHOT_DIR=<dir> saves the sidebar of every size for a look by eye.
    if (process.env.E2E_SHOT_DIR) await page.getByTestId('channel-drawer').screenshot({ path: `${process.env.E2E_SHOT_DIR}/voice-panel-${labels.locale}-${state}-${size.name.replace(/\W+/g, '-')}.png` })
    // The next size starts from the default width.
    if (size.sidebar) await page.getByRole('separator', { name: labels.resize }).dblclick()
  }
}

test.beforeAll(async ({ browser }) => {
  const ctx = await browser.newContext()
  const page = await ctx.newPage()
  await page.goto('/')
  expect((await apiFetch(page, 'POST', '/api/auth/login', { username: ADMIN_USER, password: ADMIN_PASSWORD })).status).toBe(200)
  expect((await apiFetch(page, 'POST', '/api/admin/channels', { name: TALK, type: 'voice' })).status).toBe(201)
  expect((await apiFetch(page, 'PUT', '/api/users/me/status', { status_text: STATUS })).status).toBe(200)
  await ctx.close()
})

test.afterAll(async ({ browser }) => {
  const ctx = await browser.newContext()
  const page = await ctx.newPage()
  await page.goto('/')
  await apiFetch(page, 'POST', '/api/auth/login', { username: ADMIN_USER, password: ADMIN_PASSWORD })
  await apiFetch(page, 'PUT', '/api/users/me/status', { status_text: '' })
  await apiFetch(page, 'PUT', '/api/users/me/locale', { locale: 'de' })
  await ctx.close()
})

for (const labels of LOCALES) {
  test(`the voice panel stays whole in the sidebar (${labels.locale})`, async ({ browser }) => {
    test.setTimeout(180_000)
    const first = SIZES[0] ?? { width: 390, height: 844 }
    const ctx = await browser.newContext({ permissions: ['microphone', 'camera'], viewport: { width: first.width, height: first.height }, locale: labels.locale === 'en' ? 'en-US' : 'de-DE' })
    opened.push(ctx)
    await ctx.addInitScript(trackPeerConnections)
    await ctx.addInitScript(fakeDisplayMedia)
    const page = await ctx.newPage()
    await page.goto('/')
    expect((await apiFetch(page, 'POST', '/api/auth/login', { username: ADMIN_USER, password: ADMIN_PASSWORD })).status).toBe(200)
    expect((await apiFetch(page, 'PUT', '/api/users/me/locale', { locale: labels.locale })).status).toBe(200)
    await page.reload()
    await joinTalk(page, labels.takeover)
    await expect(page.getByTestId('own-subline')).toHaveText(STATUS)
    await checkEverySize(page, labels, 'connected')
    // A screen reader hears the ping value, not only "Connection details".
    await expect(page.getByTestId('voice-panel-ping')).toHaveAccessibleName(/(\d+|–) ms/)

    // Sharing a screen adds the stream audio button to the controls.
    await page.setViewportSize({ width: 1132, height: 820 })
    await page.getByTestId('voice-panel-share').click()
    await expect(page.getByTestId('voice-panel-stream-audio')).toBeVisible()
    // The Talk view opens on the shared screen (which closes the phone's channel drawer); let it settle first.
    await expect(page.getByTestId('stage')).toBeVisible()
    await checkEverySize(page, labels, 'sharing')

    await page.getByTestId('voice-panel-leave').click()
    await expect(page.getByTestId('voice-panel-leave')).toHaveCount(0)
  })
}
