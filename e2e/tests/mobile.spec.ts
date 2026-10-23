import { mkdir, readFile, writeFile } from 'node:fs/promises'
import type { Cookie, Page } from '@playwright/test'
import { apiFetch, bounds, responseString, responseUserId } from './helpers'
import { test, expect } from './fixtures'

// The phone layout (below 768px): the channel list is a drawer, the thread and
// member list fill the screen, controls have a 44px touch area and nothing
// scrolls sideways. Runs in the "mobile-*" projects (device emulation).

const ADMIN_USER = process.env.E2E_ADMIN_USER || 'Herzog'
const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD ?? ''
if (!ADMIN_PASSWORD) throw new Error('E2E_ADMIN_PASSWORD is required (run via e2e/run.sh)')

const suffix = Date.now().toString(36)
const TEXT_CHANNEL = `mobil-${suffix}`
const VOICE_CHANNEL = `Mobil Talk ${suffix}`
const MIN_TARGET = 44

// Sealed calls do not work on Playwright's Linux WebKit (CI); they pass on macOS
// WebKit and on Chromium. Same limit as the CI WebKit job, which skips the call specs.
const isLinuxWebkit = (browserName: string) => browserName === 'webkit' && process.platform === 'linux'

// The login allows 60 attempts per 15 minutes from one address and the whole
// suite shares them. The first mobile project signs in through the dialog and
// leaves its session here; the next one reuses it (and signs in itself if the
// session is no longer valid) instead of spending another attempt.
const SESSION_FILE = 'test-results/.mobile-session.json'

async function savedSession(): Promise<Cookie[] | null> {
  try {
    return JSON.parse(await readFile(SESSION_FILE, 'utf8')) as Cookie[]
  } catch {
    return null
  }
}

async function signIn(page: Page) {
  const saved = await savedSession()
  if (saved) await page.context().addCookies(saved)
  await page.goto('/')
  const dialog = page.getByRole('dialog')
  await expect(dialog.or(page.getByTestId('open-nav'))).toBeVisible()
  if (!(await dialog.isVisible())) return
  await dialog.getByLabel('Benutzername').fill(ADMIN_USER)
  await dialog.getByLabel('Passwort').fill(ADMIN_PASSWORD)
  await dialog.getByRole('button', { name: 'Anmelden' }).click()
  await expect(dialog).toBeHidden()
  await mkdir('test-results', { recursive: true })
  await writeFile(SESSION_FILE, JSON.stringify(await page.context().cookies()))
}

async function expectNoSidewaysScroll(page: Page) {
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)
  expect(overflow).toBeLessThanOrEqual(0)
}

async function expectTouchSize(page: Page, name: string | RegExp) {
  const box = await bounds(page.getByRole('button', { name }).last())
  expect(box.width, `${String(name)} width`).toBeGreaterThanOrEqual(MIN_TARGET)
  expect(box.height, `${String(name)} height`).toBeGreaterThanOrEqual(MIN_TARGET)
}

async function openDrawer(page: Page) {
  await page.getByTestId('open-nav').tap()
  await expect(page.getByTestId('channel-drawer')).toBeInViewport({ ratio: 0.9 })
}

test('phone: sign in, drawer, message, thread, settings, Talk', async ({ page, browserName }) => {
  await signIn(page)
  const viewport = page.viewportSize()
  expect(viewport && viewport.width).toBeLessThan(768)

  // Channels to work with, made through the API like the other specs do.
  const text = await apiFetch(page, 'POST', '/api/admin/channels', { name: TEXT_CHANNEL, type: 'text' })
  expect(text.status).toBe(201)
  const voice = await apiFetch(page, 'POST', '/api/admin/channels', { name: VOICE_CHANNEL, type: 'voice' })
  expect(voice.status).toBe(201)
  await page.reload()

  // The channel list is closed and out of the way; the drawer opens it.
  await expect(page.getByTestId('open-nav')).toBeVisible()
  await expect(page.getByTestId('channel-drawer')).not.toBeInViewport()
  await expectTouchSize(page, 'Kanalliste öffnen')
  await openDrawer(page)
  const row = page.locator('[data-channel-type="text"]', { hasText: TEXT_CHANNEL })
  expect((await bounds(row)).height).toBeGreaterThanOrEqual(MIN_TARGET)
  await row.tap()
  await expect(page.getByTestId('channel-drawer')).not.toBeInViewport()
  await expect(page.getByTestId('chat-channel-name')).toHaveText(TEXT_CHANNEL)

  // Send a message with the composer; its send button is a proper touch target.
  const composer = page.getByRole('textbox', { name: `Nachricht an #${TEXT_CHANNEL}` })
  const message = `Hallo vom Telefon ${suffix}`
  await composer.fill(message)
  await expectTouchSize(page, 'Senden')
  await page.getByRole('button', { name: 'Senden' }).tap()
  const posted = page.locator('[data-msg-id]', { hasText: message })
  await expect(posted).toBeVisible()
  await expectNoSidewaysScroll(page)

  // A long unbroken word wraps instead of pushing the page sideways.
  const long = `https://example.com/${'lang/'.repeat(30)}`
  await composer.fill(long)
  await composer.press('Enter')
  await expect(page.locator('[data-msg-id]', { hasText: 'example.com' })).toBeVisible()
  await expectNoSidewaysScroll(page)

  // Thread: a reply makes the root open it; the thread fills the screen.
  const messageId = await posted.getAttribute('data-msg-id')
  const reply = await apiFetch(page, 'POST', `/api/channels/${responseString(text, 'id')}/messages`, { content: `Antwort ${suffix}`, parent_id: messageId })
  expect(reply.status).toBe(201)
  await page.getByRole('button', { name: /Thread/ }).first().tap()
  const thread = page.getByRole('complementary').filter({ hasText: `Antwort ${suffix}` })
  await expect(thread).toBeVisible()
  const threadBox = await bounds(thread)
  expect(threadBox.width).toBeGreaterThanOrEqual((viewport?.width ?? 0) - 1)
  await page.getByRole('button', { name: 'Thread schließen' }).tap()
  await expect(thread).toBeHidden()

  // Member list: a full-screen panel with a close button.
  await page.getByRole('button', { name: 'Mitgliederliste ein-/ausblenden' }).tap()
  await expect(page.getByTestId('members-close')).toBeVisible()
  await page.getByTestId('members-close').tap()
  await expect(page.getByTestId('members-close')).toBeHidden()

  // Settings: the account menu lives at the bottom of the drawer; the dialog fits the screen.
  await openDrawer(page)
  await page.getByTestId('account-menu-button').tap()
  await page.getByRole('menuitem').first().tap()
  const dialog = page.getByRole('dialog')
  await expect(dialog).toBeVisible()
  const box = await bounds(dialog)
  expect(box.x).toBeGreaterThanOrEqual(0)
  expect(box.x + box.width).toBeLessThanOrEqual((viewport?.width ?? 0) + 1)
  expect(box.y + box.height).toBeLessThanOrEqual((viewport?.height ?? 0) + 1)
  await expectNoSidewaysScroll(page)
  await page.keyboard.press('Escape')
  await expect(dialog).toBeHidden()

  // Talk: join from the drawer, the control bar stays on screen, leave.
  await openDrawer(page)
  await page.locator('[data-channel-type="voice"]', { hasText: VOICE_CHANNEL }).tap()
  if (isLinuxWebkit(browserName)) {
    // Playwright's Linux WebKit cannot seal call media (no encoded-frame
    // transform or Ed25519), so the client refuses to join there, on a phone
    // as on a desktop; see doc/product-status.md. The refusal is what that
    // engine must show: the message on screen, no call, no leave button.
    await expect(page.getByRole('alert')).toContainText('Dieser Browser kann die Medien eines Talks nicht schützen')
    await expect(page.getByRole('button', { name: 'Verlassen' })).toHaveCount(0)
    await expectNoSidewaysScroll(page)
    return
  }
  // The stage's own control bar (the drawer's status panel is off screen).
  const leave = page.getByRole('button', { name: 'Verlassen' }).last()
  await expect(leave).toBeInViewport({ ratio: 1 })
  await expectTouchSize(page, 'Verlassen')
  await expectNoSidewaysScroll(page)
  await leave.tap()
  await expect(page.getByRole('button', { name: 'Verlassen' })).toHaveCount(0)
})

// Popovers and the admin dialogs are anchored or wide on a desktop; on a phone
// they must stay inside the screen (mentions inbox, pins, emoji picker, the
// admin console).
test('phone: popovers, emoji picker and the admin console stay on screen', async ({ page }) => {
  await signIn(page)
  const width = page.viewportSize()?.width ?? 0
  const channel = `mobil-pop-${suffix}`
  const made = await apiFetch(page, 'POST', '/api/admin/channels', { name: channel, type: 'text' })
  expect(made.status).toBe(201)
  await page.reload()
  await openDrawer(page)
  await page.locator('[data-channel-type="text"]', { hasText: channel }).tap()

  const insideScreen = async (testId: string) => {
    const box = await bounds(page.getByTestId(testId))
    expect(box.x, `${testId} left edge`).toBeGreaterThanOrEqual(0)
    expect(box.x + box.width, `${testId} right edge`).toBeLessThanOrEqual(width + 1)
  }
  for (const [button, panel] of [['mentions-inbox-button', 'mentions-inbox'], ['pinned-button', 'pinned-panel']] as const) {
    await page.getByTestId(button).tap()
    await expect(page.getByTestId(panel)).toBeVisible()
    await insideScreen(panel)
    await page.getByTestId(button).tap()
    await expect(page.getByTestId(panel)).toBeHidden()
  }
  await page.getByTestId('emoji-button').tap()
  await expect(page.getByTestId('emoji-picker')).toBeVisible()
  await insideScreen('emoji-picker')
  await page.keyboard.press('Escape')

  // The admin console: every tab fits, the title names the tab, the tab bar
  // keeps its place and nothing pushes the page sideways.
  await page.goto('/admin/users')
  const dialog = page.getByRole('dialog')
  await expect(dialog).toBeVisible()
  const barTop = (await bounds(page.getByTestId('admin-tabs'))).y
  for (const tab of ['users', 'roles', 'channels', 'invites', 'onboarding', 'emoji', 'reports', 'bans', 'bots', 'media', 'audit', 'system']) {
    const button = page.getByTestId(`tab-${tab}`)
    await button.tap()
    await expect(button).toHaveAttribute('aria-pressed', 'true')
    await expect(button).toBeInViewport()
    await expect(dialog.getByRole('heading', { level: 2 })).toHaveText((await button.innerText()).trim())
    expect((await bounds(page.getByTestId('admin-tabs'))).y, `${tab} tab bar position`).toBe(barTop)
    await expectNoSidewaysScroll(page)
  }
})

// Direct messages have no screen of their own on a phone: they sit at the top of
// the channel drawer, with the same touch targets and no sideways scrolling.
test('phone: direct messages live in the drawer', async ({ page, browser }) => {
  await signIn(page)
  const viewport = page.viewportSize()
  const name = `mobdm_${suffix}`
  const memberContext = await browser.newContext()
  const member = await memberContext.newPage()
  try {
    // A member to talk to, registered from a context of their own.
    await member.goto('/')
    const invite = await apiFetch(page, 'POST', '/api/admin/invites', { max_uses: 1 })
    const registered = await apiFetch(member, 'POST', '/api/auth/register', { username: name, password: `pw-${suffix}-mobile-x1`, invite_code: responseString(invite, 'code') })
    expect(registered.status).toBe(201)
    const memberId = responseUserId(await apiFetch(member, 'GET', '/api/auth/me'))
    const started = await apiFetch(page, 'POST', '/api/dms', { user_ids: [memberId] })
    expect([200, 201]).toContain(started.status)
    const message = `Hallo aus der Direktnachricht ${suffix}`
    expect((await apiFetch(member, 'POST', `/api/channels/${responseString(started, 'id')}/messages`, { content: message })).status).toBe(201)
    await page.reload()

    // The section is in the drawer, not on the screen, with a badge and touch-sized rows.
    await expect(page.getByTestId('dm-section')).not.toBeInViewport()
    await openDrawer(page)
    await expect(page.getByTestId('dm-section')).toBeInViewport()
    const row = page.locator('[data-dm]', { hasText: name })
    await expect(row.getByTestId('dm-unread')).toHaveText('1')
    expect((await bounds(row)).height).toBeGreaterThanOrEqual(MIN_TARGET)
    await expectTouchSize(page, 'Neue Direktnachricht')
    await expectNoSidewaysScroll(page)

    // Opening one closes the drawer and shows the conversation.
    await row.tap()
    await expect(page.getByTestId('channel-drawer')).not.toBeInViewport()
    await expect(page.getByTestId('chat-channel-name')).toHaveText(name)
    await expect(page.locator('[data-msg-id]', { hasText: message })).toBeVisible()
    await expect(page.getByPlaceholder(`Nachricht an ${name}`)).toBeVisible()
    await expectTouchSize(page, 'Gesprächsinfo')
    await expectNoSidewaysScroll(page)

    // The info dialog and the picker fit the screen.
    await page.getByRole('button', { name: 'Gesprächsinfo' }).tap()
    const info = page.getByTestId('dm-info')
    await expect(info).toBeVisible()
    const box = await bounds(page.getByRole('dialog'))
    expect(box.x).toBeGreaterThanOrEqual(0)
    expect(box.x + box.width).toBeLessThanOrEqual((viewport?.width ?? 0) + 1)
    await page.keyboard.press('Escape')
    await openDrawer(page)
    await page.getByTestId('dm-new').tap()
    await expect(page.getByTestId('dm-create')).toBeVisible()
    const picker = await bounds(page.getByRole('dialog'))
    expect(picker.x + picker.width).toBeLessThanOrEqual((viewport?.width ?? 0) + 1)
    await expectNoSidewaysScroll(page)
  } finally {
    await memberContext.close()
  }
})
