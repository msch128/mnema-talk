import type { Page } from '@playwright/test'
import { test, expect } from './fixtures'
import { apiFetch, bounds, responseString } from './helpers'
import { translate } from './clipping/i18n'

// A thread and the member list on windows too narrow for every column: the
// chat never shrinks below 360px, the thread opens over the chat with a way
// back, and the member button says whether the list is on screen and shows it
// (over the chat) when pressed.

const ADMIN_USER = process.env.E2E_ADMIN_USER || 'cool_guy'
const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD ?? ''
if (!ADMIN_PASSWORD) throw new Error('E2E_ADMIN_PASSWORD is required (run via e2e/run.sh)')

const CHAT_MIN = 360
const made: string[] = []
test.afterEach(async ({ page }) => {
  for (const id of made.splice(0)) await apiFetch(page, 'DELETE', `/api/admin/channels/${id}`)
})

/** Signs in, makes a channel with a thread, and returns the thread's address. */
async function channelWithThread(page: Page): Promise<string> {
  await page.goto('/')
  expect((await apiFetch(page, 'POST', '/api/auth/login', { username: ADMIN_USER, password: ADMIN_PASSWORD })).status).toBe(200)
  const channel = await apiFetch(page, 'POST', '/api/admin/channels', { name: `columns-${Date.now().toString(36)}`, type: 'text' })
  const id = responseString(channel, 'id')
  made.push(id)
  const root = responseString(await apiFetch(page, 'POST', `/api/channels/${id}/messages`, { content: 'A message with a thread' }), 'id')
  expect((await apiFetch(page, 'POST', `/api/channels/${id}/messages`, { content: 'A reply in the thread', parent_id: root })).status).toBe(201)
  return `/c/${id}/t/${root}`
}

const memberButton = (page: Page) => page.getByRole('button', { name: translate('de', 'members.toggle'), exact: true })

test('on a tablet the thread opens over the chat, with a way back to a chat of full width', async ({ page }) => {
  await page.setViewportSize({ width: 820, height: 1180 })
  const thread = await channelWithThread(page)
  await page.goto(thread)
  await expect(page.getByTestId('thread-back')).toBeVisible()
  const drawer = await bounds(page.getByTestId('channel-drawer'))
  const panel = await bounds(page.locator('aside').filter({ has: page.getByTestId('thread-close') }))
  expect(panel.x, 'the thread covers the chat beside the channel list').toBeLessThanOrEqual(drawer.x + drawer.width + 1)
  await page.getByTestId('thread-back').click()
  await expect(page.getByTestId('thread-close')).toHaveCount(0)
  expect((await bounds(page.locator('#main-content'))).width).toBeGreaterThanOrEqual(CHAT_MIN)
})

test('the member button tells whether the list is shown and shows it over the chat beside a thread', async ({ page }) => {
  await page.setViewportSize({ width: 1000, height: 800 })
  const thread = await channelWithThread(page)
  await page.goto(thread)
  await expect(page.getByTestId('thread-close')).toBeVisible()
  await expect(page.getByTestId('thread-back')).toHaveCount(0)
  expect((await bounds(page.locator('#main-content'))).width, 'the chat beside the thread').toBeGreaterThanOrEqual(CHAT_MIN)
  // The list stepped aside for the thread: the button is not pressed, and pressing it shows the list.
  if ((await memberButton(page).getAttribute('aria-pressed')) === 'true') {
    await expect(page.locator('[data-member]').first()).toBeVisible()
    await memberButton(page).click()
  }
  await expect(memberButton(page)).toHaveAttribute('aria-pressed', 'false')
  await memberButton(page).click()
  await expect(memberButton(page)).toHaveAttribute('aria-pressed', 'true')
  await expect(page.locator('[data-member]').first()).toBeVisible()
  // The list over the chat closes with its own button; the thread is still there.
  await page.getByTestId('members-close').click()
  await expect(page.locator('[data-member]')).toHaveCount(0)
  await expect(memberButton(page)).toHaveAttribute('aria-pressed', 'false')
  await expect(page.getByTestId('thread-close')).toBeVisible()
})

// Two lines of messages with the list's padding (pt-2, pb-6, 22px lines).
const TWO_LINES = 76

test('on a short window the notification hint keeps to one line and the messages never show as a sliver', async ({ page }) => {
  // The hint shows to an account past its welcome until it is answered; this
  // account may be anywhere in that, depending on the specs before.
  await page.route('**/api/onboarding', async (route) => {
    const response = await route.fetch()
    const json = (await response.json()) as { dismissed_tips?: string[] }
    const dismissed = (json.dismissed_tips ?? []).filter(tip => tip !== 'notif-hint')
    await route.fulfill({ response, json: { ...json, welcome_done: true, dismissed_tips: dismissed } })
  })
  // ...and only while the browser has not been asked yet (headless browsers answer "denied").
  await page.addInitScript(() => Object.defineProperty(Notification, 'permission', { configurable: true, get: () => 'default' }))
  // The desktop client's minimum window (572x480) zoomed to 125 %.
  await page.setViewportSize({ width: 458, height: 384 })
  const thread = await channelWithThread(page)
  await page.goto(thread.replace(/\/t\/.*$/, ''))
  const hint = page.getByRole('region', { name: translate('de', 'notifications.hintTitle') })
  await expect(hint).toBeVisible()
  expect((await bounds(hint)).height, 'the hint on one line').toBeLessThanOrEqual(52)

  const list = page.locator('.msg-list')
  await expect(list.locator('[data-msg-id]').first()).toBeAttached()
  for (const height of [384, 340, 300, 260, 230, 200]) {
    await page.setViewportSize({ width: 458, height })
    const drawn = await list.evaluate(el => getComputedStyle(el).visibility !== 'hidden')
    const tall = (await bounds(list)).height
    expect(!drawn || tall >= TWO_LINES, `at ${height}px the list is drawn ${Math.round(tall)}px tall`).toBe(true)
  }
})
