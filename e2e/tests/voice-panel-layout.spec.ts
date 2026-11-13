import type { BrowserContext, Locator, Page } from '@playwright/test'
import { apiFetch, bounds, trackPeerConnections, expectTalkLive, leaveAndClose } from './helpers'
import { test, expect } from './fixtures'

// The voice panel at the bottom of the sidebar ("Connected · <Talk>", the
// ping and the connection details, share and leave) stays whole while seated
// in a Talk: at the desktop client's default and minimum window, common laptop
// screens and a phone, in English and German, with the longest Talk name and
// status line the server allows. The channel list above it is what scrolls.

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
const VIEWPORTS = [
  { name: 'phone', width: 390, height: 844 },
  { name: 'desktop minimum', width: 572, height: 480 },
  { name: 'desktop default', width: 1132, height: 820 },
  { name: 'laptop', width: 1366, height: 768 },
  { name: 'small laptop', width: 1280, height: 720 },
]

// The interface speaks the account's stored language (stores/auth/locale.ts), so
// each test stores its own and the Owner goes back to German afterwards, as the other specs expect.
const LOCALES = [
  { locale: 'en-US', status: 'Connected', details: 'Connection details', takeover: 'Take over here' },
  { locale: 'de-DE', status: 'Verbunden', details: 'Verbindungsdetails', takeover: 'Hier übernehmen' },
]

async function openDrawer(page: Page) {
  const drawer = page.getByTestId('channel-drawer')
  if (await drawer.evaluate(el => el.hasAttribute('inert'))) await page.getByTestId('open-nav').click()
  await expect(drawer).toBeInViewport({ ratio: 1 })
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

/** Whether the element's text is cut (ellipsis or hidden overflow). */
const truncated = (locator: Locator) => locator.evaluate(el => el.scrollWidth > el.clientWidth || el.scrollHeight > el.clientHeight)

/** The element at the centre of each control is the control itself: nothing lies over it. */
async function expectUncovered(controls: Locator[]) {
  for (const control of controls) {
    const free = await control.evaluate((el) => {
      const r = el.getBoundingClientRect()
      const hit = document.elementFromPoint(r.left + r.width / 2, r.top + r.height / 2)
      return !!hit && (hit === el || el.contains(hit))
    })
    expect(free, `${await control.getAttribute('data-testid')} is covered`).toBe(true)
  }
}

async function expectPanelWhole(page: Page, labels: (typeof LOCALES)[number], where: string) {
  const panel = page.locator('section.vpanel-wrap')
  const open = page.getByTestId('voice-panel-open')
  const ping = page.getByTestId('voice-panel-ping')
  const leave = page.getByTestId('voice-panel-leave')
  const channel = page.getByTestId('voice-panel-channel')
  const nav = page.getByRole('navigation').filter({ has: page.locator('[data-channel-type]') })

  // Whole panel on screen and inside the drawer, above the user bar.
  for (const part of [panel, open, ping, leave]) await expect(part, where).toBeInViewport({ ratio: 1 })
  const drawer = await bounds(page.getByTestId('channel-drawer'))
  const box = await bounds(panel)
  expect(box.y + box.height, `${where}: panel bottom inside the drawer`).toBeLessThanOrEqual(drawer.y + drawer.height + 0.5)
  expect(box.x + box.width, `${where}: panel right edge inside the drawer`).toBeLessThanOrEqual(drawer.x + drawer.width + 0.5)
  const own = await bounds(page.getByTestId('own-profile-button'))
  expect(box.y + box.height, `${where}: panel above the user bar`).toBeLessThanOrEqual(own.y + 0.5)

  // "Connected", the ping and the details text are whole; a long name truncates and is named in full.
  await expect(open.locator('span').nth(1)).toHaveText(labels.status)
  expect(await truncated(open.locator('span').nth(1)), `${where}: status label cut`).toBe(false)
  await expect(channel).toHaveText(TALK)
  await expect(open).toHaveAttribute('aria-label', new RegExp(`${TALK}$`))
  await expect(ping).toContainText(' ms')
  const details = ping.getByText(labels.details)
  if (await details.isVisible()) expect(await truncated(details), `${where}: details cut`).toBe(false)
  expect(await truncated(ping), `${where}: ping row cut`).toBe(false)
  await expectUncovered([open, ping, leave])

  // The channel list above it scrolls instead of pushing the panel down.
  const list = await nav.evaluate(el => ({ overflow: getComputedStyle(el).overflowY, bottom: el.getBoundingClientRect().bottom }))
  expect(list.overflow).toBe('auto')
  expect(list.bottom, `${where}: channel list ends above the panel`).toBeLessThanOrEqual(box.y + 0.5)
}

/** Truncated texts are readable in full: the Talk name and the status line in their tooltips. */
async function expectFullTextInTooltips(page: Page) {
  await page.getByTestId('voice-panel-channel').hover()
  await expect(page.getByRole('tooltip')).toContainText(TALK)
  await page.getByTestId('own-subline').hover()
  await expect(page.getByRole('tooltip')).toContainText(STATUS)
  await page.mouse.move(0, 0)
  await expect(page.getByRole('tooltip')).toBeHidden()
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
    const first = VIEWPORTS[0] ?? { width: 390, height: 844 }
    const ctx = await browser.newContext({ permissions: ['microphone', 'camera'], viewport: { width: first.width, height: first.height }, locale: labels.locale })
    opened.push(ctx)
    await ctx.addInitScript(trackPeerConnections)
    const page = await ctx.newPage()
    await page.goto('/')
    expect((await apiFetch(page, 'POST', '/api/auth/login', { username: ADMIN_USER, password: ADMIN_PASSWORD })).status).toBe(200)
    expect((await apiFetch(page, 'PUT', '/api/users/me/locale', { locale: labels.locale.slice(0, 2) })).status).toBe(200)
    await page.reload()
    await joinTalk(page, labels.takeover)
    await expect(page.getByTestId('own-subline')).toHaveText(STATUS)
    for (const viewport of VIEWPORTS) {
      await page.setViewportSize({ width: viewport.width, height: viewport.height })
      await openDrawer(page)
      await expectPanelWhole(page, labels, `${viewport.name} ${viewport.width}x${viewport.height}`)
      if (viewport.name === 'desktop default') await expectFullTextInTooltips(page)
      // E2E_SHOT_DIR=<dir> saves the sidebar of every size for a look by eye.
      if (process.env.E2E_SHOT_DIR) await page.getByTestId('channel-drawer').screenshot({ path: `${process.env.E2E_SHOT_DIR}/voice-panel-${labels.locale}-${viewport.width}x${viewport.height}.png` })
    }
    await page.getByTestId('voice-panel-leave').click()
    await expect(page.getByTestId('voice-panel-leave')).toHaveCount(0)
  })
}
