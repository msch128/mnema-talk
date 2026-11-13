import type { Page } from '@playwright/test'
import { test, expect } from './fixtures'
import { apiFetch, bounds, responseString } from './helpers'
import { translate } from './clipping/i18n'

// A thread and the member list on windows too narrow for every column: the
// chat never shrinks below 360px, the thread opens over the chat with a way
// back, and the member button says whether the list is on screen and shows it
// (over the chat) when pressed. Also parts that must not lie over each other:
// the header's name, topic and chips, and a reply's avatar and reply line.

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
  // The member list is shown first, so the thread has to make room beside both.
  await page.goto(thread.replace(/\/t\/.*$/, ''))
  if ((await memberButton(page).getAttribute('aria-pressed')) !== 'true') await memberButton(page).click()
  await expect(page.locator('[data-member]').first()).toBeVisible()
  await page.goto(thread)
  await expect(page.getByTestId('thread-back')).toBeVisible()
  const drawer = await bounds(page.getByTestId('channel-drawer'))
  const panel = await bounds(page.locator('aside').filter({ has: page.getByTestId('thread-close') }))
  expect(panel.x, 'the thread covers the chat beside the channel list').toBeLessThanOrEqual(drawer.x + drawer.width + 1)
  await page.getByTestId('thread-back').click()
  await expect(page.getByTestId('thread-close')).toHaveCount(0)
  expect((await bounds(page.locator('#main-content'))).width).toBeGreaterThanOrEqual(CHAT_MIN)
  // Beside the member list the composer's field keeps room to type in.
  await expect(page.locator('[data-member]').first()).toBeVisible()
  expect((await bounds(page.locator('#main-content').getByRole('textbox', { name: /^Nachricht an/ }))).width, 'the message field').toBeGreaterThanOrEqual(96)
})

/** Whether two boxes share more than half a pixel in both directions. */
function overlap(a: { x: number; y: number; width: number; height: number }, b: { x: number; y: number; width: number; height: number }): boolean {
  const x = Math.min(a.x + a.width, b.x + b.width) - Math.max(a.x, b.x)
  const y = Math.min(a.y + a.height, b.y + b.height) - Math.max(a.y, b.y)
  return x > 0.5 && y > 0.5
}

test('the channel name, its topic and the auto-delete chip never lie over each other in the header', async ({ page }) => {
  await page.goto('/')
  expect((await apiFetch(page, 'POST', '/api/auth/login', { username: ADMIN_USER, password: ADMIN_PASSWORD })).status).toBe(200)
  const channel = await apiFetch(page, 'POST', '/api/admin/channels', { name: `a-header-with-a-long-channel-name-${Date.now().toString(36)}`, type: 'text', topic: `A topic for the header ${'that keeps going '.repeat(4)}` })
  const id = responseString(channel, 'id')
  made.push(id)
  expect((await apiFetch(page, 'PUT', `/api/admin/channels/${id}/auto-delete`, { days: 7 })).status).toBe(200)
  for (const [width, height] of [[390, 844], [820, 1180], [1132, 820]] as const) {
    await page.setViewportSize({ width, height })
    await page.goto(`/c/${id}`)
    const chip = page.getByTestId('auto-delete-chip')
    await expect(chip).toBeVisible()
    const header = page.locator('#main-content header').first()
    // The topic is a button too: the header's other buttons are its icons.
    const icons = header.getByRole('button').and(page.locator(':not([data-testid="channel-topic"])'))
    const parts = [page.getByTestId('chat-channel-name'), page.getByTestId('channel-topic'), chip, ...await icons.all()]
    const boxes: { name: string; box: { x: number; y: number; width: number; height: number } }[] = []
    for (const part of parts) {
      if (!(await part.isVisible())) continue
      boxes.push({ name: (await part.getAttribute('data-testid')) ?? (await part.getAttribute('aria-label')) ?? (await part.innerText()), box: await bounds(part) })
    }
    for (const [i, a] of boxes.entries()) {
      for (const b of boxes.slice(i + 1)) expect(overlap(a.box, b.box), `at ${width}px "${a.name}" lies over "${b.name}"`).toBe(false)
    }
  }
})

test('the avatar of a reply sits below the line that says what it replies to', async ({ page }) => {
  await page.goto('/')
  expect((await apiFetch(page, 'POST', '/api/auth/login', { username: ADMIN_USER, password: ADMIN_PASSWORD })).status).toBe(200)
  const id = responseString(await apiFetch(page, 'POST', '/api/admin/channels', { name: `reply-${Date.now().toString(36)}`, type: 'text' }), 'id')
  made.push(id)
  const first = responseString(await apiFetch(page, 'POST', `/api/channels/${id}/messages`, { content: 'The message replied to' }), 'id')
  expect((await apiFetch(page, 'POST', `/api/channels/${id}/messages`, { content: 'The reply', reply_to_id: first })).status).toBe(201)
  for (const [width, height] of [[390, 844], [1132, 820]] as const) {
    await page.setViewportSize({ width, height })
    await page.goto(`/c/${id}`)
    const row = page.locator('[data-msg-id]').filter({ has: page.locator('[data-reply-preview]') })
    const preview = await bounds(row.locator('[data-reply-preview]'))
    const avatar = await bounds(row.getByTestId('msg-avatar'))
    expect(overlap(preview, avatar), `at ${width}px the avatar lies over the reply line`).toBe(false)
    expect(avatar.y, `at ${width}px the avatar starts below the reply line`).toBeGreaterThanOrEqual(preview.y + preview.height - 0.5)
  }
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
