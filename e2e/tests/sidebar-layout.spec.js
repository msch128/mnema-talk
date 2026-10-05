import { randomBytes } from 'node:crypto'
import { test, expect } from '@playwright/test'

const ADMIN_USER = process.env.E2E_ADMIN_USER || 'Herzog'
const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD
if (!ADMIN_PASSWORD) throw new Error('E2E_ADMIN_PASSWORD is required (run via e2e/run.sh)')

// Throwaway names for this run; other specs may have created channels too,
// so every check below only looks at the channels and categories made here.
const suffix = Date.now().toString(36)
const MEMBER = `member_${suffix}`
const MEMBER_PASSWORD = `pw-${suffix}-${randomBytes(8).toString('hex')}`

// The seeded layout: the uncategorized group (U) first, then three categories.
const CATEGORIES = { A: `Alpha ${suffix}`, B: `Beta ${suffix}`, G: `Gamma ${suffix}` }
const CHANNELS = {
  u1: { name: `frei-${suffix}`, type: 'text', cat: null },
  a1: { name: `a-eins-${suffix}`, type: 'text', cat: 'A' },
  a2: { name: `a-zwei-${suffix}`, type: 'text', cat: 'A' },
  a3: { name: `a-drei-${suffix}`, type: 'text', cat: 'A' },
  b1: { name: `b-eins-${suffix}`, type: 'text', cat: 'B' },
  b2: { name: `Beta Talk ${suffix}`, type: 'voice', cat: 'B' },
  g1: { name: `g-eins-${suffix}`, type: 'text', cat: 'G' },
}
const SEED = { cats: ['A', 'B', 'G'], U: ['u1'], A: ['a1', 'a2', 'a3'], B: ['b1', 'b2'], G: ['g1'] }

const ids = {} // key → server id (channels and categories)
const keyOf = (id) => Object.keys(ids).find((k) => ids[k] === id)

const isLayoutSave = (req) => req.method() === 'PUT' && new URL(req.url()).pathname === '/api/admin/layout'

// Same-origin fetch from inside the page, so cookies and the Origin header
// behave exactly like the real app (CSRF origin check included).
async function apiFetch(page, method, path, body) {
  return page.evaluate(
    async ({ method, path, body }) => {
      const res = await fetch(path, {
        method,
        credentials: 'same-origin',
        headers: body ? { 'Content-Type': 'application/json' } : {},
        body: body ? JSON.stringify(body) : undefined,
      })
      return { status: res.status, json: await res.json().catch(() => null) }
    },
    { method, path, body },
  )
}

async function signIn(page, username, password) {
  await page.goto('/')
  const dialog = page.getByRole('dialog')
  await dialog.getByLabel('Benutzername').fill(username)
  await dialog.getByLabel('Passwort').fill(password)
  await dialog.getByRole('button', { name: 'Anmelden' }).click()
  await expect(dialog).toBeHidden()
}

// ---- Reading the layout (only this run's items, as keys) ----

function emptyLayout() {
  return { cats: [], U: [], A: [], B: [], G: [] }
}

function addChannels(out, container, channelIds) {
  for (const id of channelIds) {
    const key = keyOf(id)
    if (key && CHANNELS[key]) out[container].push(key)
  }
}

/** The order on screen. */
async function domLayout(page) {
  const sections = await page.locator('nav [data-drop-section]').evaluateAll((els) =>
    els.map((el) => ({
      id: el.getAttribute('data-drop-section'),
      channels: [...el.querySelectorAll('[data-drop="channel"]')].map((r) => r.getAttribute('data-id')),
    })),
  )
  const out = emptyLayout()
  for (const s of sections) {
    const container = s.id === '__uncategorized' ? 'U' : keyOf(s.id)
    if (!container || !out[container]) continue
    if (container !== 'U') out.cats.push(container)
    addChannels(out, container, s.channels)
  }
  return out
}

/** The order the server has (GET /api/channels). */
async function serverLayout(page) {
  const { json } = await apiFetch(page, 'GET', '/api/channels')
  const out = emptyLayout()
  addChannels(out, 'U', (json.uncategorized || []).map((c) => c.id))
  for (const cat of json.categories || []) {
    const key = keyOf(cat.id)
    if (!key || !out[key] || key === 'U') continue
    out.cats.push(key)
    addChannels(out, key, (cat.channels || []).map((c) => c.id))
  }
  return out
}

async function expectLayout(page, expected, { server = true } = {}) {
  const want = { ...emptyLayout(), ...expected }
  await expect.poll(() => domLayout(page), { message: 'sidebar order' }).toEqual(want)
  if (server) await expect.poll(() => serverLayout(page), { message: 'server order' }).toEqual(want)
}

/** Puts this run's items back into the seeded order (directly via the API). */
async function resetLayout(page) {
  const res = await apiFetch(page, 'PUT', '/api/admin/layout', {
    categories: SEED.cats.map((k, i) => ({ id: ids[k], sort_order: 100 + i })),
    channels: ['U', ...SEED.cats].flatMap((c) =>
      SEED[c].map((k, i) => ({ id: ids[k], category_id: c === 'U' ? null : ids[c], sort_order: 100 + i })),
    ),
  })
  expect(res.status).toBe(204)
}

// ---- Locators ----

const nav = (page) => page.getByRole('navigation', { name: 'Kanäle' })
const row = (page, key) => page.locator(`[data-drop="channel"][data-id="${ids[key]}"]`)
const header = (page, key) => page.locator(`[data-drop="header"][data-id="${ids[key]}"]`)
const toggle = (page, key) => page.locator(`[data-category-toggle="${ids[key]}"]`)
const ghost = (page) => page.getByTestId('drag-ghost')
const indicator = (page) => page.locator('[data-drop-indicator]')

// ---- Real mouse drags ----

/**
 * Presses on `source`, moves past the drag threshold and on to `ratio` of the
 * target's height (0 = top edge, 1 = bottom edge), in small steps like a hand.
 * Leaves the button pressed; finish with page.mouse.up() or Escape.
 */
async function dragTo(page, source, target, ratio) {
  // Both ends on screen first: the list auto-scrolls near its edges.
  await target.scrollIntoViewIfNeeded()
  await expect(source).toBeInViewport()
  const s = await source.boundingBox()
  const x = s.x + Math.min(40, s.width / 2)
  const y = s.y + s.height / 2
  await page.mouse.move(x, y)
  await page.mouse.down()
  await page.mouse.move(x + 2, y + 6, { steps: 3 })
  await expect(ghost(page)).toBeVisible()
  const t = await target.boundingBox()
  await page.mouse.move(t.x + 40, t.y + t.height * ratio, { steps: 12 })
}

/** A full drag and drop that saves; resolves once the server answered. */
async function dropOn(page, source, target, ratio) {
  await dragTo(page, source, target, ratio)
  await expect(indicator(page)).toBeVisible()
  const saved = page.waitForResponse((res) => isLayoutSave(res.request()))
  await page.mouse.up()
  expect((await saved).status()).toBe(204)
  await expect(ghost(page)).toBeHidden()
}

async function reloadSignedIn(page) {
  await page.reload()
  await expect(row(page, 'u1')).toBeVisible()
}

// ---- Shared sessions ----

let adminCtx
let memberCtx
let admin
let member
let layoutSaves = 0
const pageErrors = []

test.describe.configure({ mode: 'serial' })

test.beforeAll(async ({ browser }) => {
  // Tall enough that the whole channel list (other specs add channels too) fits.
  adminCtx = await browser.newContext({ viewport: { width: 1280, height: 1100 } })
  memberCtx = await browser.newContext({ viewport: { width: 1280, height: 1100 } })
  admin = await adminCtx.newPage()
  member = await memberCtx.newPage()
  admin.on('request', (req) => { if (isLayoutSave(req)) layoutSaves++ })
  for (const page of [admin, member]) page.on('pageerror', (err) => pageErrors.push(err.message))

  await signIn(admin, ADMIN_USER, ADMIN_PASSWORD)
  for (const [key, name] of Object.entries(CATEGORIES)) {
    const res = await apiFetch(admin, 'POST', '/api/admin/categories', { name, sort_order: 100 })
    expect(res.status).toBe(201)
    ids[key] = res.json.id
  }
  for (const [key, ch] of Object.entries(CHANNELS)) {
    const res = await apiFetch(admin, 'POST', '/api/admin/channels', {
      name: ch.name, type: ch.type, category_id: ch.cat ? ids[ch.cat] : null, sort_order: 0,
    })
    expect(res.status).toBe(201)
    ids[key] = res.json.id
  }
  await resetLayout(admin)

  const invite = await apiFetch(admin, 'POST', '/api/admin/invites', { max_uses: 1 })
  expect(invite.status).toBe(201)
  await member.goto('/')
  const reg = await apiFetch(member, 'POST', '/api/auth/register', {
    username: MEMBER, password: MEMBER_PASSWORD, invite_code: invite.json.code,
  })
  expect(reg.status).toBe(201)
})

test.afterAll(async () => {
  // Leave the instance as it was for the specs that run after this one.
  if (admin && !admin.isClosed()) {
    for (const key of Object.keys(CHANNELS)) {
      if (ids[key]) await apiFetch(admin, 'DELETE', `/api/admin/channels/${ids[key]}`)
    }
    for (const key of Object.keys(CATEGORIES)) {
      if (ids[key]) await apiFetch(admin, 'DELETE', `/api/admin/categories/${ids[key]}`)
    }
  }
  await adminCtx?.close()
  await memberCtx?.close()
})

test.afterEach(() => {
  expect(pageErrors, 'uncaught errors in the app').toEqual([])
})

test.beforeEach(async () => {
  await resetLayout(admin)
  await reloadSignedIn(admin)
  await expectLayout(admin, SEED)
})

test('admin reorders with the mouse; saved, persisted and live for others', async () => {
  await reloadSignedIn(member)
  await expectLayout(member, SEED, { server: false })

  // 1. Within a category: a1 below a2.
  await dropOn(admin, row(admin, 'a1'), row(admin, 'a2'), 0.75)
  const step1 = { ...SEED, A: ['a2', 'a1', 'a3'] }
  await expectLayout(admin, step1)
  // The member sees it without reloading (channels_changed).
  await expectLayout(member, step1, { server: false })
  await reloadSignedIn(admin)
  await expectLayout(admin, step1)

  // 2. Into another category: a3 above b2 (the voice channel).
  await dropOn(admin, row(admin, 'a3'), row(admin, 'b2'), 0.25)
  const step2 = { ...step1, A: ['a2', 'a1'], B: ['b1', 'a3', 'b2'] }
  await expectLayout(admin, step2)
  await expectLayout(member, step2, { server: false })
  await reloadSignedIn(admin)
  await expectLayout(admin, step2)

  // 3. Out to the uncategorized group: b1 below u1.
  await dropOn(admin, row(admin, 'b1'), row(admin, 'u1'), 0.75)
  const step3 = { ...step2, U: ['u1', 'b1'], B: ['a3', 'b2'] }
  await expectLayout(admin, step3)
  await reloadSignedIn(admin)
  await expectLayout(admin, step3)

  // 4. Categories: Gamma (dragged by its header) above Alpha.
  await dropOn(admin, header(admin, 'G'), header(admin, 'A'), 0.2)
  const step4 = { ...step3, cats: ['G', 'A', 'B'] }
  await expectLayout(admin, step4)
  await expectLayout(member, step4, { server: false })
  // The click after the drag didn't reach the header: Gamma is still open.
  await expect(toggle(admin, 'G')).toHaveAttribute('aria-expanded', 'true')
  await reloadSignedIn(admin)
  await expectLayout(admin, step4)
})

test('drag feedback: indicator, Escape cancels, a click still opens', async () => {
  const savesBefore = layoutSaves

  // While dragging: the ghost follows the pointer, the indicator marks the slot.
  await dragTo(admin, row(admin, 'a1'), row(admin, 'a3'), 0.8)
  await expect(indicator(admin)).toHaveCount(1)
  // The line sits at the bottom edge of a3's row.
  const line = await indicator(admin).boundingBox()
  const a3 = await row(admin, 'a3').boundingBox()
  expect(Math.abs(line.y + line.height / 2 - (a3.y + a3.height))).toBeLessThanOrEqual(4)
  await expect(row(admin, 'a1').locator('..')).toHaveClass(/opacity-40/)

  // Escape cancels: nothing moves, nothing is saved, releasing does nothing.
  await admin.keyboard.press('Escape')
  await expect(ghost(admin)).toBeHidden()
  await expect(indicator(admin)).toHaveCount(0)
  await admin.mouse.up()
  await expectLayout(admin, SEED)

  // Dropping outside the channel list cancels as well.
  await dragTo(admin, row(admin, 'a2'), row(admin, 'g1'), 0.5)
  const box = await nav(admin).boundingBox()
  await admin.mouse.move(box.x + box.width + 200, box.y + 100, { steps: 5 })
  await expect(indicator(admin)).toHaveCount(0)
  await admin.mouse.up()
  await expect(ghost(admin)).toBeHidden()
  await expectLayout(admin, SEED)

  // A plain click (even with a small wobble below the threshold) navigates.
  const g1 = await row(admin, 'g1').boundingBox()
  await admin.mouse.move(g1.x + 30, g1.y + g1.height / 2)
  await admin.mouse.down()
  await admin.mouse.move(g1.x + 32, g1.y + g1.height / 2 + 1)
  await admin.mouse.up()
  await expect(admin).toHaveURL(new RegExp(`/c/${ids.g1}$`))
  await expect(admin.getByRole('textbox', { name: `Nachricht an #${CHANNELS.g1.name}` })).toBeVisible()
  await expect(ghost(admin)).toBeHidden()

  expect(layoutSaves).toBe(savesBefore)
})

test('undo from the toast restores the previous order', async () => {
  await dropOn(admin, row(admin, 'g1'), row(admin, 'a1'), 0.25)
  const moved = { ...SEED, A: ['g1', 'a1', 'a2', 'a3'], G: [] }
  await expectLayout(admin, moved)

  const toast = admin.getByText('Kanal verschoben')
  await expect(toast).toBeVisible()
  const saved = admin.waitForResponse((res) => isLayoutSave(res.request()))
  await admin.getByRole('button', { name: 'Rückgängig' }).click()
  expect((await saved).status()).toBe(204)
  await expect(admin.getByText('Reihenfolge wiederhergestellt')).toBeVisible()
  await expectLayout(admin, SEED)
  await reloadSignedIn(admin)
  await expectLayout(admin, SEED)
})

test('context menus: create, collapse and duplicate', async () => {
  const menu = admin.getByRole('menu')

  // Empty part of the list → "Kategorie erstellen" → dialog → new category at
  // the end. With a long list that is the padding below the last channel.
  await nav(admin).evaluate((el) => { el.scrollTop = el.scrollHeight })
  const box = await nav(admin).boundingBox()
  await admin.mouse.click(box.x + box.width / 2, box.y + box.height - 6, { button: 'right' })
  await expect(menu.getByRole('menuitem', { name: 'Kanal erstellen' })).toBeVisible()
  await menu.getByRole('menuitem', { name: 'Kategorie erstellen' }).click()
  const dialog = admin.getByRole('dialog')
  const newCat = `Delta ${suffix}`
  await dialog.getByRole('textbox').fill(newCat)
  await dialog.getByRole('button', { name: 'Erstellen' }).click()
  await expect(dialog).toBeHidden()
  const created = admin.locator('nav [data-category-id]').last()
  await expect(created).toContainText(newCat)
  const deltaId = await created.getAttribute('data-category-id')

  // Category menu → "Kategorie erstellen" puts the new one right below it.
  await header(admin, 'A').click({ button: 'right' })
  await menu.getByRole('menuitem', { name: 'Kategorie erstellen' }).click()
  const below = `Epsilon ${suffix}`
  await dialog.getByRole('textbox').fill(below)
  await dialog.getByRole('button', { name: 'Erstellen' }).click()
  await expect(dialog).toBeHidden()
  const afterAlpha = admin.locator(`nav section[data-category-id="${ids.A}"] + section`)
  await expect(afterAlpha).toContainText(below)
  const epsilonId = await afterAlpha.getAttribute('data-category-id')
  await expect.poll(async () => {
    const { json } = await apiFetch(admin, 'GET', '/api/channels')
    const order = json.categories.map((c) => c.id)
    return order.indexOf(epsilonId) - order.indexOf(ids.A)
  }).toBe(1)

  // Category menu → "Kanal erstellen" opens the modal with that category chosen.
  await header(admin, 'B').click({ button: 'right' })
  await menu.getByRole('menuitem', { name: 'Kanal erstellen' }).click()
  await expect(admin.getByLabel('Kategorie', { exact: true })).toHaveValue(ids.B)
  await admin.getByRole('button', { name: 'Abbrechen' }).click()
  await expect(dialog).toBeHidden()

  // Collapse all / expand all.
  await header(admin, 'G').click({ button: 'right' })
  await menu.getByRole('menuitem', { name: 'Alle einklappen' }).click()
  for (const k of SEED.cats) await expect(toggle(admin, k)).toHaveAttribute('aria-expanded', 'false')
  await expect(row(admin, 'a2')).toBeHidden()
  await header(admin, 'G').click({ button: 'right' })
  await expect(menu.getByRole('menuitem', { name: 'Alle einklappen' })).toBeDisabled()
  await menu.getByRole('menuitem', { name: 'Alle ausklappen' }).click()
  for (const k of SEED.cats) await expect(toggle(admin, k)).toHaveAttribute('aria-expanded', 'true')
  await expect(row(admin, 'a2')).toBeVisible()

  // Channel menu → "Kanal duplizieren": the copy sits right below, nothing navigates.
  await row(admin, 'b1').click()
  await expect(admin).toHaveURL(new RegExp(`/c/${ids.b1}$`))
  await row(admin, 'a2').click({ button: 'right' })
  const dup = admin.waitForResponse((res) => res.request().method() === 'POST' && res.url().endsWith(`/api/admin/channels/${ids.a2}/duplicate`))
  await menu.getByRole('menuitem', { name: 'Kanal duplizieren' }).click()
  const copy = (await (await dup).json())
  expect(copy.id).not.toBe(ids.a2)
  expect(copy.name).toBe(CHANNELS.a2.name)
  const alphaRows = admin.locator(`nav section[data-category-id="${ids.A}"] [data-drop="channel"]`)
  await expect.poll(() => alphaRows.evaluateAll((els) => els.map((e) => e.getAttribute('data-id'))))
    .toEqual([ids.a1, ids.a2, copy.id, ids.a3])
  await expect.poll(async () => {
    const { json } = await apiFetch(admin, 'GET', '/api/channels')
    return json.categories.find((c) => c.id === ids.A).channels.map((c) => c.id)
  }).toEqual([ids.a1, ids.a2, copy.id, ids.a3])
  await expect(admin).toHaveURL(new RegExp(`/c/${ids.b1}$`))
  await expect(admin.getByRole('textbox', { name: `Nachricht an #${CHANNELS.b1.name}` })).toBeVisible()

  // Clean up what this test created.
  expect((await apiFetch(admin, 'DELETE', `/api/admin/channels/${copy.id}`)).status).toBe(204)
  for (const id of [deltaId, epsilonId]) {
    expect((await apiFetch(admin, 'DELETE', `/api/admin/categories/${id}`)).status).toBe(204)
  }
})

test('keyboard: Alt+Arrow moves the focused channel and announces it', async () => {
  const announcer = admin.getByTestId('sidebar-announcer')

  await row(admin, 'a1').focus()
  await admin.keyboard.press('Alt+ArrowDown')
  await expectLayout(admin, { ...SEED, A: ['a2', 'a1', 'a3'] })
  await expect(announcer).toHaveText(`${CHANNELS.a1.name} verschoben: Position 2 von 3 in ${CATEGORIES.A}`)
  await expect(row(admin, 'a1')).toBeFocused()

  // Past the end of a category it goes to the start of the next one.
  await admin.keyboard.press('Alt+ArrowDown')
  await admin.keyboard.press('Alt+ArrowDown')
  await expectLayout(admin, { ...SEED, A: ['a2', 'a3'], B: ['a1', 'b1', 'b2'] })
  await expect(announcer).toHaveText(`${CHANNELS.a1.name} verschoben: Position 1 von 3 in ${CATEGORIES.B}`)
  await expect(row(admin, 'a1')).toBeFocused()

  await admin.keyboard.press('Alt+ArrowUp')
  await expectLayout(admin, { ...SEED, A: ['a2', 'a3', 'a1'] })
  await expect(row(admin, 'a1')).toBeFocused()

  // Categories move from their header.
  await toggle(admin, 'G').focus()
  await admin.keyboard.press('Alt+ArrowUp')
  const final = { ...SEED, cats: ['A', 'G', 'B'], A: ['a2', 'a3', 'a1'] }
  await expectLayout(admin, final)
  // Other specs' categories count too, so only check that it went up by one.
  const { json } = await apiFetch(admin, 'GET', '/api/channels')
  const position = json.categories.findIndex((c) => c.id === ids.G) + 1
  await expect(announcer).toHaveText(`${CATEGORIES.G} verschoben: Position ${position} von ${json.categories.length}`)
  expect(json.categories[position]?.id).toBe(ids.B)

  await reloadSignedIn(admin)
  await expectLayout(admin, final)
})

test('touch: long press drags, a quick swipe scrolls', async ({ browser }) => {
  // A small touch screen: the channel list is taller than its viewport.
  const ctx = await browser.newContext({ hasTouch: true, isMobile: true, viewport: { width: 900, height: 480 } })
  const page = await ctx.newPage()
  page.on('pageerror', (err) => pageErrors.push(err.message))
  let saves = 0
  page.on('request', (req) => { if (isLayoutSave(req)) saves++ })
  await signIn(page, ADMIN_USER, ADMIN_PASSWORD)
  await expectLayout(page, SEED, { server: false })
  const list = nav(page)

  // Real touch input through the browser's input pipeline (pointer, touch
  // and gesture events all come from it, as on a phone).
  const cdp = await ctx.newCDPSession(page)
  const touch = (type, x, y) =>
    cdp.send('Input.dispatchTouchEvent', { type, touchPoints: type === 'touchEnd' ? [] : [{ x, y }] })
  const point = async (locator, ratio = 0.5) => {
    const b = await locator.boundingBox()
    return { x: b.x + 40, y: b.y + b.height * ratio }
  }

  // Long press picks the channel up, then it follows the finger. Alpha sits
  // mid-list, away from the edges where the list would auto-scroll.
  await row(page, 'a2').evaluate((el) => el.scrollIntoView({ block: 'center' }))
  const from = await point(row(page, 'a1'))
  const to = await point(row(page, 'a3'), 0.8)
  const scrollBefore = await list.evaluate((el) => el.scrollTop)
  await touch('touchStart', from.x, from.y)
  await expect(ghost(page)).toBeVisible()
  for (let i = 1; i <= 10; i++) {
    await touch('touchMove', from.x, from.y + ((to.y - from.y) * i) / 10)
  }
  await expect(indicator(page)).toBeVisible()
  // The list did not scroll under the finger while dragging.
  expect(await list.evaluate((el) => el.scrollTop)).toBe(scrollBefore)
  const saved = page.waitForResponse((res) => isLayoutSave(res.request()))
  await touch('touchEnd')
  expect((await saved).status()).toBe(204)
  await expect(ghost(page)).toBeHidden()
  await expectLayout(page, { ...SEED, A: ['a2', 'a3', 'a1'] })
  // Neither the long press nor the drop opened a menu or a channel.
  await expect(page.getByRole('menu')).toHaveCount(0)
  await expect(page).not.toHaveURL(new RegExp(ids.a1))

  // Holding without moving and letting go opens the channel's menu instead.
  await row(page, 'a2').scrollIntoViewIfNeeded()
  const hold = await point(row(page, 'a2'))
  await touch('touchStart', hold.x, hold.y)
  await expect(ghost(page)).toBeVisible()
  await touch('touchEnd')
  await expect(ghost(page)).toBeHidden()
  await expect(page.getByRole('menu').getByRole('menuitem', { name: 'Kanal duplizieren' })).toBeVisible()
  await page.keyboard.press('Escape')
  await expect(page.getByRole('menu')).toHaveCount(0)
  expect(saves).toBe(1)

  // A quick swipe scrolls the list and picks nothing up.
  await list.evaluate((el) => { el.scrollTop = 0 })
  const area = await list.boundingBox()
  const start = { x: area.x + 40, y: area.y + area.height * 0.8 } // on a row near the bottom
  await touch('touchStart', start.x, start.y)
  for (let i = 1; i <= 6; i++) await touch('touchMove', start.x, start.y - i * 25)
  await touch('touchEnd')
  await expect.poll(() => list.evaluate((el) => el.scrollTop)).toBeGreaterThan(30)
  await expect(ghost(page)).toHaveCount(0)
  await expect(page.getByRole('menu')).toHaveCount(0)
  expect(saves).toBe(1)

  await ctx.close()
})

test('members cannot move anything', async () => {
  await reloadSignedIn(member)
  await expectLayout(member, SEED, { server: false })
  let saves = 0
  const count = (req) => { if (isLayoutSave(req)) saves++ }
  member.on('request', count)

  // Dragging does nothing: no ghost, no indicator, no save.
  const s = await row(member, 'a1').boundingBox()
  const t = await row(member, 'a3').boundingBox()
  await member.mouse.move(s.x + 30, s.y + s.height / 2)
  await member.mouse.down()
  await member.mouse.move(t.x + 30, t.y + t.height * 0.8, { steps: 12 })
  await expect(ghost(member)).toHaveCount(0)
  await expect(indicator(member)).toHaveCount(0)
  await member.mouse.up()

  // Nor does the keyboard.
  await row(member, 'a2').focus()
  await member.keyboard.press('Alt+ArrowDown')
  await expect(row(member, 'a2')).not.toHaveAttribute('aria-keyshortcuts', /.+/)

  // No admin items in the menus; the empty list area keeps the browser's menu.
  const menu = member.getByRole('menu')
  await row(member, 'a2').click({ button: 'right' })
  await expect(menu.getByRole('menuitem', { name: 'Link kopieren' })).toBeVisible()
  await expect(menu.getByRole('menuitem', { name: 'Kanal duplizieren' })).toHaveCount(0)
  await expect(menu.getByRole('menuitem', { name: 'Kanal bearbeiten' })).toHaveCount(0)
  await member.keyboard.press('Escape')
  await expect(menu).toHaveCount(0)
  await header(member, 'A').click({ button: 'right' })
  await expect(menu.getByRole('menuitem', { name: 'Alle einklappen' })).toBeVisible()
  await expect(menu.getByRole('menuitem', { name: 'Kanal erstellen' })).toHaveCount(0)
  await member.keyboard.press('Escape')
  await nav(member).evaluate((el) => { el.scrollTop = el.scrollHeight })
  const box = await nav(member).boundingBox()
  await member.mouse.click(box.x + box.width / 2, box.y + box.height - 6, { button: 'right' })
  await expect(menu).toHaveCount(0)

  await expectLayout(member, SEED)
  member.off('request', count)
  expect(saves).toBe(0)
})
