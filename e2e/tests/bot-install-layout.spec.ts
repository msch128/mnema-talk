import type { Page } from '@playwright/test'
import { test, expect } from './fixtures'
import { apiFetch, responseString } from './helpers'

// The confirm screen of a bot's manifest at the widths members use: nothing may
// run out of the admin pane (a long channel name, a long command name), the
// quoted name is never cut, the tablet gives the flow real width, and on a
// phone the add link lands with the confirm screen in view.
const ADMIN_USER = process.env.E2E_ADMIN_USER || 'cool_guy'
const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD ?? ''
if (!ADMIN_PASSWORD) throw new Error('E2E_ADMIN_PASSWORD is required (run via e2e/run.sh)')

const suffix = Date.now().toString(36)
// 64 characters, the longest a channel name may be.
const longChannel = (width: number) => `a-channel-with-a-really-long-name-that-will-not-fit-${suffix}-${width}`.padEnd(64, 'x').slice(0, 64)
const manifest = {
  manifest_version: 1, name: 'Mnemo helper for notes', description: 'Answers questions and keeps notes for the whole community, in every channel it is allowed into.',
  homepage: 'https://example.org/mnemo/a/rather/long/path/to/its/readme', intents: ['GUILDS'], permissions: ['view_channel', 'send_messages'],
  commands: [{ name: 'remindmeinaverylongcommandname', description: 'A command with a long name', options: [] }]
}
const link = `/admin/bots/add#manifest=${Buffer.from(JSON.stringify(manifest), 'utf8').toString('base64url')}`

/** Elements of the flow that end right of the pane (or are cut), as short descriptions. */
function overflowing(page: Page) {
  return page.evaluate(() => {
    const pane = document.querySelector('[data-testid="admin-bots"]')
    if (!pane) return ['no pane']
    const right = pane.getBoundingClientRect().right + 1
    const out: string[] = []
    for (const el of pane.querySelectorAll<HTMLElement>('[data-testid="manifest-install-flow"] *')) {
      const box = el.getBoundingClientRect()
      if (box.width > 0 && box.right > right) out.push(`${el.tagName.toLowerCase()} ${(el.textContent ?? '').trim().slice(0, 40)}`)
    }
    const scroller = pane.closest('.overflow-y-auto')
    if (scroller && scroller.scrollWidth > scroller.clientWidth + 1) out.push('the pane scrolls sideways')
    return out
  })
}

for (const [width, height] of [[390, 844], [820, 1180], [1132, 820]] as const) {
  test(`the confirm screen fits at ${width} px`, async ({ browser }) => {
    const page = await (await browser.newContext({ viewport: { width, height } })).newPage()
    await page.goto('/')
    expect((await apiFetch(page, 'POST', '/api/auth/login', { username: ADMIN_USER, password: ADMIN_PASSWORD })).status).toBe(200)
    const name = longChannel(width)
    const channel = await apiFetch(page, 'POST', '/api/admin/channels', { name, type: 'text' })
    expect(channel.status).toBe(201)
    await page.goto(link)
    const confirm = page.getByTestId('manifest-confirm')
    await expect(confirm).toBeVisible()
    await expect(page.getByTestId('manifest-channels')).toContainText(name)
    await page.waitForTimeout(600) // the smooth scroll has settled

    expect(await overflowing(page)).toEqual([])
    const nameCut = await page.getByTestId('manifest-name').evaluate(el => el.scrollWidth > el.clientWidth + 1)
    expect(nameCut, 'the quoted name is cut').toBe(false)
    const flowWidth = (await page.getByTestId('manifest-install-flow').boundingBox())?.width ?? 0
    // A column of a third of the pane (about 210 px on the tablet) is too narrow to read.
    if (width >= 820) expect(flowWidth).toBeGreaterThan(420)
    // The heading of the confirm screen is on screen, and focused.
    const heading = await page.locator('#manifest-title').boundingBox()
    expect(heading && heading.y >= 0 && heading.y < height).toBe(true)
    await expect(page.locator('#manifest-title')).toBeFocused()
    await apiFetch(page, 'DELETE', `/api/admin/channels/${responseString(channel, 'id')}`)
  })
}
